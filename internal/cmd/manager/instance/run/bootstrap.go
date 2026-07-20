/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package run

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/kballard/go-shellquote"
	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/bootstrap"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
	instancecertificate "github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/instance/certificate"
)

// bootstrapOptions collects the run flags that describe how to initialize an
// uninitialized PGDATA in-process before starting PostgreSQL. The flag names
// are the same the bootstrap Job builders used to emit, so the operator can
// hand them over verbatim. They are inert unless --bootstrap-mode is set.
type bootstrapOptions struct {
	mode string

	pgWal                            string
	parentNode                       string
	appDBName                        string
	appUser                          string
	initDBFlags                      string
	postInitSQL                      string
	postInitApplicationSQL           string
	postInitTemplateSQL              string
	postInitSQLRefsFolder            string
	postInitApplicationSQLRefsFolder string
	postInitTemplateSQLRefsFolder    string
	backupLabel                      string
	tablespaceMap                    string
	immediate                        bool
}

func (o *bootstrapOptions) registerFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.mode, "bootstrap-mode", "",
		"The bootstrap method to initialize an uninitialized data directory before starting PostgreSQL")
	cmd.Flags().StringVar(&o.pgWal, "pg-wal", "", "The PGWAL to be created during bootstrap")
	cmd.Flags().StringVar(&o.parentNode, "parent-node", "", "The origin node")
	cmd.Flags().StringVar(&o.appDBName, "app-db-name", "",
		"The name of the application containing the database")
	cmd.Flags().StringVar(&o.appUser, "app-user", "", "The name of the application user")
	cmd.Flags().StringVar(&o.initDBFlags, "initdb-flags", "",
		"The list of flags to be passed to initdb while creating the initial database")
	cmd.Flags().StringVar(&o.postInitSQL, "post-init-sql", "",
		"The list of SQL queries to be executed to configure the new instance")
	cmd.Flags().StringVar(&o.postInitApplicationSQL, "post-init-application-sql", "",
		"The list of SQL queries to be executed inside application database right after the database is created")
	cmd.Flags().StringVar(&o.postInitTemplateSQL, "post-init-template-sql", "",
		"The list of SQL queries to be executed inside template1 database to configure the new instance")
	cmd.Flags().StringVar(&o.postInitSQLRefsFolder, "post-init-sql-refs-folder", "",
		"The folder contains a set of SQL files to be executed in alphabetical order")
	cmd.Flags().StringVar(&o.postInitApplicationSQLRefsFolder, "post-init-application-sql-refs-folder", "",
		"The folder contains a set of SQL files to be executed in alphabetical order "+
			"against the application database immediately after its creation")
	cmd.Flags().StringVar(&o.postInitTemplateSQLRefsFolder, "post-init-template-sql-refs-folder", "",
		"The folder contains a set of SQL files to be executed in alphabetical order")
	cmd.Flags().StringVar(&o.backupLabel, "backuplabel", "", "The restore backup_label file content")
	cmd.Flags().StringVar(&o.tablespaceMap, "tablespacemap", "", "The restore tablespace_map file content")
	cmd.Flags().BoolVar(&o.immediate, "immediate", false,
		"Do not start PostgreSQL but just recover the snapshot")
}

// instruction returns the bootstrap instruction described by the flags and a
// flag telling whether a bootstrap was requested at all.
func (o *bootstrapOptions) instruction() (bootstrap.Instruction, bool) {
	if o.mode == "" {
		return bootstrap.Instruction{}, false
	}

	return bootstrap.Instruction{
		Mode:      bootstrap.Mode(o.mode),
		Immediate: o.immediate,
	}, true
}

// initInfo builds the InitInfo consumed by the bootstrap library from the flags
// and the instance identity carried by run's own flags. It mirrors, verbatim,
// the parsing the bootstrap subcommands do (shellquote for the SQL lists,
// base64 for the backup label and tablespace map).
func (o *bootstrapOptions) initInfo(
	pgData, podName, clusterName, namespace string,
) (postgres.InitInfo, error) {
	initDBFlags, err := shellquote.Split(o.initDBFlags)
	if err != nil {
		return postgres.InitInfo{}, fmt.Errorf("while parsing initdb flags: %w", err)
	}

	postInitSQL, err := shellquote.Split(o.postInitSQL)
	if err != nil {
		return postgres.InitInfo{}, fmt.Errorf("while parsing post init SQL queries: %w", err)
	}

	postInitApplicationSQL, err := shellquote.Split(o.postInitApplicationSQL)
	if err != nil {
		return postgres.InitInfo{}, fmt.Errorf("while parsing post init application SQL queries: %w", err)
	}

	postInitTemplateSQL, err := shellquote.Split(o.postInitTemplateSQL)
	if err != nil {
		return postgres.InitInfo{}, fmt.Errorf("while parsing post init template SQL queries: %w", err)
	}

	var backupLabelFile []byte
	if o.backupLabel != "" {
		if backupLabelFile, err = base64.StdEncoding.DecodeString(o.backupLabel); err != nil {
			return postgres.InitInfo{}, fmt.Errorf("while decoding the backup label: %w", err)
		}
	}

	var tablespaceMapFile []byte
	if o.tablespaceMap != "" {
		if tablespaceMapFile, err = base64.StdEncoding.DecodeString(o.tablespaceMap); err != nil {
			return postgres.InitInfo{}, fmt.Errorf("while decoding the tablespace map: %w", err)
		}
	}

	return postgres.InitInfo{
		PgData:                           pgData,
		PgWal:                            o.pgWal,
		PodName:                          podName,
		ClusterName:                      clusterName,
		Namespace:                        namespace,
		ParentNode:                       o.parentNode,
		ApplicationDatabase:              o.appDBName,
		ApplicationUser:                  o.appUser,
		InitDBOptions:                    initDBFlags,
		PostInitSQL:                      postInitSQL,
		PostInitApplicationSQL:           postInitApplicationSQL,
		PostInitTemplateSQL:              postInitTemplateSQL,
		PostInitSQLRefsFolder:            o.postInitSQLRefsFolder,
		PostInitApplicationSQLRefsFolder: o.postInitApplicationSQLRefsFolder,
		PostInitTemplateSQLRefsFolder:    o.postInitTemplateSQLRefsFolder,
		BackupLabelFile:                  backupLabelFile,
		TablespaceMapFile:                tablespaceMapFile,
	}, nil
}

// runBootstrap initializes an uninitialized PGDATA in-process before the
// controller-runtime manager is started (phase 0). It is a no-op if a previous
// attempt already wrote the completion marker, so a container restart after a
// successful bootstrap falls straight through to the normal run.
//
// While it works it flips the instance bootstrap flag and serves a minimal
// status web server plus the local web server (needed by wal-restore during
// PITR): the kubelet startup probe is skipped, readiness stays red and the
// status endpoint replies 503. Both servers are shut down before returning so
// the manager's own web servers can bind the same ports.
func runBootstrap(
	ctx context.Context,
	instance *postgres.Instance,
	info postgres.InitInfo,
	instruction bootstrap.Instruction,
) error {
	contextLogger := log.FromContext(ctx)

	cli, err := management.NewControllerRuntimeClient()
	if err != nil {
		return fmt.Errorf("while creating the Kubernetes client for bootstrap: %w", err)
	}

	// Load the cluster before checking the completion marker: the marker is
	// scoped to the instance identity, and the cluster UID is part of that
	// identity. The cluster is already known to exist here (run's PreRunE waits
	// for it), and the fetch is needed anyway by the restore recovery-CA step
	// below.
	var cluster apiv1.Cluster
	if err := cli.Get(ctx, client.ObjectKey{
		Namespace: info.Namespace,
		Name:      info.ClusterName,
	}, &cluster); err != nil {
		return fmt.Errorf("while loading the cluster for the bootstrap: %w", err)
	}

	// The marker certifies that THIS instance bootstrapped this data directory.
	// Because the marker lives inside PGDATA it travels inside volume snapshots,
	// so a PVC cloned from a snapshot already carries the source instance's
	// marker; scoping the check to the identity is what makes such a clone run
	// phase-0 instead of wrongly skipping it.
	identity := bootstrap.MarkerIdentity{
		Namespace:   info.Namespace,
		ClusterName: info.ClusterName,
		ClusterUID:  string(cluster.UID),
		PodName:     info.PodName,
	}

	completed, err := bootstrap.IsCompleted(info.PgData, identity)
	if err != nil {
		return fmt.Errorf("while checking the bootstrap completion marker: %w", err)
	}
	if completed {
		contextLogger.Info("Data directory already bootstrapped, skipping", "mode", instruction.Mode)
		return nil
	}

	contextLogger.Info("Starting in-process bootstrap", "mode", instruction.Mode)

	// A restore-based bootstrap may reach a TLS-protected barman object store (the
	// base backup download and, during WAL replay, wal-restore) before the
	// instance manager and its certificate reconciler run, so write the
	// recovery-source endpoint CA to disk first. The file lives on an emptyDir
	// shared with the normal run, so it survives the handover to instance-run.
	if err := writeRecoveryBarmanEndpointCA(ctx, cli, &cluster, instruction.Mode); err != nil {
		return err
	}

	// When the status port is served over TLS, the kubelet probes handshake
	// against it, so we must load the server certificate before serving. On a
	// plain-HTTP status port this is skipped entirely.
	if instance.StatusPortTLS {
		if err := loadServerCertificate(ctx, cli, instance); err != nil {
			return err
		}
	}

	instance.StartBootstrap(string(instruction.Mode))

	recorder, err := management.NewEventRecorder()
	if err != nil {
		return fmt.Errorf("while creating the event recorder for bootstrap: %w", err)
	}

	statusServer := webserver.NewBootstrapWebServer(instance)
	localServer, err := webserver.NewLocalWebServer(instance, cli, recorder)
	if err != nil {
		return fmt.Errorf("while creating the local web server for bootstrap: %w", err)
	}

	serversCtx, stopServers := context.WithCancel(ctx)
	var wg sync.WaitGroup
	startServer := func(name string, srv *webserver.Webserver) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if serveErr := srv.Start(serversCtx); serveErr != nil {
				contextLogger.Error(serveErr, "phase-0 web server stopped with an error", "server", name)
			}
		}()
	}
	startServer("status", statusServer)
	startServer("local", localServer)

	bootErr := bootstrap.Execute(ctx, cli, instance, info, instruction)

	// Release the status and local ports so the manager web servers can bind
	// them once we fall through to the normal run.
	stopServers()
	wg.Wait()

	if bootErr != nil {
		return bootErr
	}

	if err := bootstrap.WriteCompletedMarker(info.PgData, instruction.Mode, identity); err != nil {
		return err
	}

	instance.CompleteBootstrap()
	contextLogger.Info("In-process bootstrap completed", "mode", instruction.Mode)

	return nil
}

// writeRecoveryBarmanEndpointCA writes the recovery-source barman endpoint CA to
// disk for the restore and restoresnapshot bootstraps, so barman-cloud can reach
// a TLS-protected object store before the instance manager's certificate
// reconciler runs. It is a no-op for the other modes and when the recovery source
// carries no endpoint CA.
func writeRecoveryBarmanEndpointCA(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	mode bootstrap.Mode,
) error {
	if mode != bootstrap.ModeRestore && mode != bootstrap.ModeRestoreSnapshot {
		return nil
	}

	backup, err := loadRecoveryBackup(ctx, cli, cluster)
	if err != nil {
		return fmt.Errorf("while loading the recovery backup for the barman endpoint CA: %w", err)
	}

	if _, err := instancecertificate.RefreshRecoveryBarmanEndpointCA(ctx, cli, cluster, backup); err != nil {
		return fmt.Errorf("while writing the recovery barman endpoint CA: %w", err)
	}

	return nil
}

// loadRecoveryBackup loads the Backup object referenced by the recovery stanza,
// when recovering from a Backup reference. It returns nil when recovering from an
// external cluster or from volume snapshots, mirroring the operator's origin
// backup lookup.
func loadRecoveryBackup(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
) (*apiv1.Backup, error) {
	if cluster.Spec.Bootstrap == nil ||
		cluster.Spec.Bootstrap.Recovery == nil ||
		cluster.Spec.Bootstrap.Recovery.Backup == nil {
		return nil, nil
	}

	var backup apiv1.Backup
	if err := cli.Get(ctx, client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Spec.Bootstrap.Recovery.Backup.Name,
	}, &backup); err != nil {
		return nil, err
	}
	return &backup, nil
}

// loadServerCertificate reads the instance server certificate into memory so
// the phase-0 status server can serve the TLS probes.
func loadServerCertificate(ctx context.Context, cli client.Client, instance *postgres.Instance) error {
	var cluster apiv1.Cluster
	if err := cli.Get(ctx, client.ObjectKey{
		Namespace: instance.GetNamespaceName(),
		Name:      instance.GetClusterName(),
	}, &cluster); err != nil {
		return fmt.Errorf("while loading the cluster for the bootstrap status server: %w", err)
	}

	if err := instancecertificate.NewReconciler(cli, instance).
		EnsureServerCertificateLoaded(ctx, &cluster); err != nil {
		return fmt.Errorf("while loading the server certificate for the bootstrap status server: %w", err)
	}

	return nil
}
