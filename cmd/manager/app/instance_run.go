/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package app

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/lib/pq"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/management/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/postgres/webserver"
)

var (
	postgresCommand *exec.Cmd
	reconciler      *controller.InstanceReconciler

	// RetryUntilServerStarted if the default retry configuration that is used
	// to wait for a server to start
	RetryUntilServerStarted = wait.Backoff{
		Duration: 1 * time.Second,
		// Steps is declared as an "int", so we are capping
		// to int32 to support ARM-based 32 bit architectures
		Steps: math.MaxInt32,
	}
)

func runSubCommand() {
	var err error

	reconciler, err = controller.NewInstanceReconciler(&instance)
	if err != nil {
		log.Log.Error(err, "Error while starting reconciler")
		os.Exit(1)
	}

	err = instance.RefreshConfigurationFiles(reconciler.GetClient())
	if err != nil {
		log.Log.Error(err, "Error while writing the bootstrap configuration")
		os.Exit(1)
	}

	err = reconciler.RefreshServerCertificateFiles()
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS server certificates")
		os.Exit(1)
	}

	err = reconciler.RefreshPostgresUserCertificate()
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS server certificates")
		os.Exit(1)
	}

	err = reconciler.RefreshCA()
	if err != nil {
		log.Log.Error(err, "Error while writing the TLS CA certificates")
		os.Exit(1)
	}

	err = reconciler.VerifyPgDataCoherence(context.Background())
	if err != nil {
		log.Log.Error(err, "Error while checking Kubernetes cluster status")
		os.Exit(1)
	}

	startWebServer()
	startReconciler()
	registerSignalHandler()

	// Print the content of PostgreSQL control data, for debugging and tracing
	pgControlData := exec.Command("pg_controldata")
	pgControlData.Env = os.Environ()
	pgControlData.Env = append(pgControlData.Env, "PGDATA="+instance.PgData)
	pgControlData.Stdout = os.Stdout
	pgControlData.Stderr = os.Stderr
	err = pgControlData.Run()

	if err != nil {
		log.Log.Error(err, "Error printing the control information of this PostgreSQL instance")
		os.Exit(1)
	}

	postgresCommand, err = instance.Run()
	if err != nil {
		log.Log.Error(err, "Unable to start PostgreSQL up")
		os.Exit(1)
	}

	isPrimary, err := instance.IsPrimary()
	if err != nil {
		log.Log.Error(err, "Checking if primary status")
		os.Exit(1)
	}
	if isPrimary {
		db, err := instance.GetSuperUserDB()
		if err != nil {
			log.Log.Error(err, "Cannot open connection to primary node")
			os.Exit(1)
		}

		err = retry.OnError(RetryUntilServerStarted, func(err error) bool {
			log.Log.Info("waiting for server to start", "err", err)
			return true
		}, db.Ping)
		if err != nil {
			log.Log.Error(err, "server did not start in time")
			os.Exit(1)
		}

		err = configureInstance(db)
		if err != nil {
			log.Log.Error(err, "Cannot configure primary node")
			os.Exit(1)
		}
	}

	if err = postgresCommand.Wait(); err != nil {
		log.Log.Error(err, "PostgreSQL exited with errors")
		os.Exit(1)
	}
}

// startWebServer start the web server for handling probes given
// a certain PostgreSQL instance
func startWebServer() {
	go func() {
		err := webserver.ListenAndServe(&instance)
		if err != nil {
			log.Log.Error(err, "Error while starting the web server")
		}
	}()
}

// startReconciler start the reconciliation loop
func startReconciler() {
	go reconciler.Run()
}

// registerSignalHandler handles signals from k8s, notifying postgres as
// needed
func registerSignalHandler() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		log.Log.Info("Received termination signal", "signal", sig)

		log.Log.Info("Shutting down web server")
		err := webserver.Shutdown()
		if err != nil {
			log.Log.Error(err, "Error while shutting down the web server")
		} else {
			log.Log.Info("Web server shut down")
		}

		log.Log.Info("Shutting down controller")
		reconciler.Stop()

		if postgresCommand != nil {
			log.Log.Info("Shutting down PostgreSQL instance")
			err := postgresCommand.Process.Signal(syscall.SIGTERM)
			if err != nil {
				log.Log.Error(err, "Unable to send SIGTERM to PostgreSQL instance")
			}
		}
	}()
}

// configureInstance creates the expected users and databases in a new
// PostgreSQL instance
func configureInstance(db *sql.DB) error {
	var err error

	log.Log.Info("Configuring primary instance")

	var hasLoginRight, hasReplicationRight bool
	row := db.QueryRow("SELECT rolcanlogin, rolreplication FROM pg_roles WHERE rolname = $1",
		v1alpha1.StreamingReplicationUser)
	err = row.Scan(&hasLoginRight, &hasReplicationRight)
	if err != nil {
		if err == sql.ErrNoRows {
			_, err = db.Exec(fmt.Sprintf(
				"CREATE USER %v REPLICATION",
				pq.QuoteIdentifier(v1alpha1.StreamingReplicationUser)))
			if err != nil {
				return fmt.Errorf("CREATE USER %v error: %w", v1alpha1.StreamingReplicationUser, err)
			}
		} else {
			return fmt.Errorf("while creating streaming replication user: %w", err)
		}
	}

	if !hasLoginRight || !hasReplicationRight {
		_, err = db.Exec(fmt.Sprintf(
			"ALTER USER %v LOGIN REPLICATION",
			pq.QuoteIdentifier(v1alpha1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("ALTER USER %v error: %w", v1alpha1.StreamingReplicationUser, err)
		}
	}

	// Ensure the user has rights to execute the functions needed for pg_rewind
	var hasPgRewindPrivileges bool
	row = db.QueryRow(
		`
			SELECT has_function_privilege($1, 'pg_ls_dir(text, boolean, boolean)', 'execute') AND
			       has_function_privilege($2, 'pg_stat_file(text, boolean)', 'execute') AND
			       has_function_privilege($3, 'pg_read_binary_file(text)', 'execute') AND
			       has_function_privilege($4, 'pg_read_binary_file(text, bigint, bigint, boolean)', 'execute')`,
		v1alpha1.StreamingReplicationUser,
		v1alpha1.StreamingReplicationUser,
		v1alpha1.StreamingReplicationUser,
		v1alpha1.StreamingReplicationUser)
	err = row.Scan(&hasPgRewindPrivileges)
	if err != nil {
		return fmt.Errorf("while getting streaming replication user privileges: %w", err)
	}
	if !hasPgRewindPrivileges {
		_, err = db.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_ls_dir(text, boolean, boolean) TO %v",
			pq.QuoteIdentifier(v1alpha1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = db.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_stat_file(text, boolean) TO %v",
			pq.QuoteIdentifier(v1alpha1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = db.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_read_binary_file(text) TO %v",
			pq.QuoteIdentifier(v1alpha1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}

		_, err = db.Exec(fmt.Sprintf(
			"GRANT EXECUTE ON function pg_catalog.pg_read_binary_file(text, bigint, bigint, boolean) TO %v",
			pq.QuoteIdentifier(v1alpha1.StreamingReplicationUser)))
		if err != nil {
			return fmt.Errorf("while granting pgrewind privileges: %w", err)
		}
	}

	return nil
}
