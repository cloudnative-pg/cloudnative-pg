/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webserver

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"golang.org/x/sync/singleflight"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/readiness"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/upgrade"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	pg "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

type remoteWebserverEndpoints struct {
	typedClient      client.Client
	instance         *postgres.Instance
	currentBackup    *backupConnection
	readinessChecker *readiness.Data

	sg singleflight.Group
}

// StartBackupRequest the required data to execute the pg_start_backup
type StartBackupRequest struct {
	ImmediateCheckpoint bool   `json:"immediateCheckpoint"`
	WaitForArchive      bool   `json:"waitForArchive"`
	BackupName          string `json:"backupName"`
	Force               bool   `json:"force,omitempty"`
}

// StopBackupRequest the required data to execute the pg_stop_backup
type StopBackupRequest struct {
	BackupName string `json:"backupName"`
}

// NewStopBackupRequest constructor
func NewStopBackupRequest(backupName string) *StopBackupRequest {
	return &StopBackupRequest{BackupName: backupName}
}

// NewRemoteWebServer returns a webserver that allows connection from external clients
func NewRemoteWebServer(
	instance *postgres.Instance,
	cancelFunc context.CancelFunc,
	exitedConditions concurrency.MultipleExecuted,
) (*Webserver, error) {
	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		return nil, fmt.Errorf("creating controller-runtine client: %v", err)
	}

	endpoints := remoteWebserverEndpoints{
		typedClient:      typedClient,
		instance:         instance,
		readinessChecker: readiness.ForInstance(instance),
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc(url.PathPgModeBackup, endpoints.backup)
	serveMux.HandleFunc(url.PathHealth, endpoints.isServerHealthy)
	serveMux.HandleFunc(url.PathReady, endpoints.isServerReady)
	serveMux.HandleFunc(url.PathPgStatus, endpoints.pgStatus)
	serveMux.HandleFunc(url.PathPgArchivePartial, endpoints.pgArchivePartial)
	serveMux.HandleFunc(url.PathPGControlData, endpoints.pgControlData)
	serveMux.HandleFunc(url.PathUpdate, endpoints.updateInstanceManager(cancelFunc, exitedConditions))
	serveMux.HandleFunc(url.PathRestart, endpoints.restartInstanceManager(cancelFunc, exitedConditions))

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", url.StatusPort),
		Handler:           serveMux,
		ReadTimeout:       DefaultReadTimeout,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
	}

	if instance.StatusPortTLS {
		server.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS13,
			GetCertificate: func(_ *tls.ClientHelloInfo) (*tls.Certificate, error) {
				return instance.ServerCertificate, nil
			},
		}
	}

	return NewWebServer(server), nil
}

func (ws *remoteWebserverEndpoints) isServerHealthy(w http.ResponseWriter, _ *http.Request) {
	// Debugging the hanging endpoints:
	// --------------------------------
	// If the number of :deferred exit logs don't match the number of :entry logs, something is wrong
	// This function exits-early; so no :exit log is expected in some cases

	log.Trace("isServerHealthy: entry")

	defer log.Trace("isServerHealthy: deferred exit")

	// If `pg_rewind` is running the Pod is starting up.
	// We need to report it healthy to avoid being killed by the kubelet.
	// Same goes for instances with fencing on.
	if ws.instance.PgRewindIsRunning || ws.instance.MightBeUnavailable() {
		log.Trace("Liveness probe skipped")
		_, _ = fmt.Fprint(w, "Skipped")
		return
	}

	err := ws.instance.IsServerHealthy()
	if err != nil {
		log.Debug("isServerHealthy: Liveness probe failing", "err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Trace("isServerHealthy: Liveness probe succeeding")
	_, _ = fmt.Fprint(w, "OK")
	log.Trace("isServerHealthy: exit")
}

// This is the readiness probe
func (ws *remoteWebserverEndpoints) isServerReady(w http.ResponseWriter, r *http.Request) {
	// Debugging the hanging endpoints:
	// --------------------------------
	// If the number of :deferred exit logs don't match the number of :entry logs, something is wrong
	// This function exits-early; so no :exit log is expected in some cases

	log.Trace("isServerReady: entry")

	defer log.Trace("isServerReady: deferred exit")

	if err := ws.readinessChecker.IsServerReady(r.Context()); err != nil {
		log.Debug("isServerReady: Readiness probe failing", "err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Trace("isServerReady: Readiness probe succeeding")

	err := writeTextResponse(w, "isServerReady", "OK")
	if err != nil {
		log.Warning("isServerReady: failed to write response", "err", err.Error())
	}

	log.Trace("isServerReady: exit")
}

// This probe is for the instance status, including replication
func (ws *remoteWebserverEndpoints) pgStatus(w http.ResponseWriter, r *http.Request) {
	// Debugging the hanging endpoints:
	// --------------------------------
	// If the number of :exit logs don't match the number of :entry logs, something is wrong
	// If the number of :exit logs don't match the number of :deferred exit logs, something is wrong

	defer log.Trace("pgStatus: deferred exit")

	log.Trace("pgStatus: entry")

	// Extract the status of the current instance. Dedup concurrent calls to make this cheap to
	// call concurrently.
	res, err, shared := ws.sg.Do("pgStatus", func() (any, error) {
		// Use a single context here that's shared among the goroutines calling this to decouple
		// the lifetimes.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Extract the status of the current instance
		status, err := ws.instance.GetStatus(ctx)
		if err != nil {
			return nil, err
		}

		return status, nil
	})
	if err != nil {
		log.Debug(
			"pgStatus: Instance status probe failing",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	status, ok := res.(*pg.PostgresqlStatus)
	if !ok {
		log.Debug("got something that's not a postgres status", "got", fmt.Sprintf("%T", res))
		http.Error(w, "unexpected status type", http.StatusInternalServerError)
		return
	}

	// Marshal the status back to the operator
	log.Trace("pgStatus: marshalling status", "shared", shared)
	js, err := json.Marshal(status)
	if err != nil {
		log.Warning(
			"pgStatus: Internal error marshalling instance status",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Trace("pgStatus: marshalling complete")

	if err := writeJSONResponse(w, "pgStatus", js); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	log.Trace("pgStatus: exit")
}

func (ws *remoteWebserverEndpoints) pgControlData(w http.ResponseWriter, _ *http.Request) {
	type Response struct {
		Data string `json:"data,omitempty"`
	}

	out, err := ws.instance.GetPgControldata()
	if err != nil {
		log.Debug(
			"Instance pg_controldata endpoint failing",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	res, err := json.Marshal(Response{Data: out})
	if err != nil {
		log.Warning(
			"Internal error marshalling pg_controldata response",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := writeJSONResponse(w, "pgControlData", res); err != nil {
		log.Warning("pgControlData: failed to write response", "err", err.Error())
	}
}

// updateInstanceManager replace the instance with one in the
// new binary
func (ws *remoteWebserverEndpoints) updateInstanceManager(
	cancelFunc context.CancelFunc,
	exitedCondition concurrency.MultipleExecuted,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		defer log.Trace("updateInstanceManager: deferred exit")
		log.Trace("updateInstanceManager: entry")

		// No need to handle this request if it is not a put
		if r.Method != http.MethodPut {
			http.Error(w, "wrong method used", http.StatusMethodNotAllowed)
			return
		}

		// No need to do anything if we are already upgrading
		if !ws.instance.InstanceManagerIsUpgrading.CompareAndSwap(false, true) {
			log.Trace("updateInstanceManager: instance manager is already upgrading")
			http.Error(w, "instance manager is already upgrading", http.StatusTeapot)
			return
		}
		// If we get here, the InstanceManagerIsUpgrading flag was set and
		// we will perform the upgrade. Ensure we unset the flag in the end
		defer func() {
			log.Trace("updateInstanceManager: resetting instance manager upgrade flag")
			ws.instance.InstanceManagerIsUpgrading.Store(false)
			log.Trace("updateInstanceManager: instance manager upgrade flag reset")
		}()

		log.Trace("updateInstanceManager: upgrading instance manager")
		err := upgrade.FromReader(cancelFunc, exitedCondition, ws.typedClient, ws.instance, r.Body)
		if err != nil {
			log.Trace("updateInstanceManager: error while upgrading instance manager", "err", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Unfortunately this point, if everything is right, will not be reached.
		// At this stage we are running the new version of the instance manager
		// and not the old one.
		if err := writeTextResponse(w, "updateInstanceManager", "Unreachable OK"); err != nil {
			log.Trace("updateInstanceManager: failed to write response in unreachable code block", "err", err)
		}

		log.Trace("updateInstanceManager: exit (unreachable)")
	}
}

// restartInstanceManager replace the instance with a copy of itself
func (ws *remoteWebserverEndpoints) restartInstanceManager(
	cancelFunc context.CancelFunc,
	exitedCondition concurrency.MultipleExecuted,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		defer log.Trace("restartInstanceManager: deferred exit")

		log.Trace("restartInstanceManager: entry")
		// No need to handle this request if it is not a put
		if r.Method != http.MethodPut {
			http.Error(w, "wrong method used", http.StatusMethodNotAllowed)
			return
		}

		// No need to do anything if we are already upgrading
		if !ws.instance.InstanceManagerIsUpgrading.CompareAndSwap(false, true) {
			log.Trace("restartInstanceManager: instance manager is already upgrading")
			http.Error(w, "instance manager is already upgrading", http.StatusTeapot)
			return
		}
		// If we get here, the InstanceManagerIsUpgrading flag was set and
		// we will perform the upgrade. Ensure we unset the flag in the end
		defer func() {
			log.Trace("restartInstanceManager: resetting instance manager upgrade flag")
			ws.instance.InstanceManagerIsUpgrading.Store(false)
			log.Trace("restartInstanceManager: instance manager upgrade flag reset")
		}()

		log.Trace("restartInstanceManager: upgrading instance manager")
		err := upgrade.FromLocalBinary(cancelFunc, exitedCondition, ws.typedClient, ws.instance, upgrade.InstanceManagerPath)
		if err != nil {
			// The defer above is known not to execute in the upgradeInstanceManager function
			// so we'll call it explicitly here
			ws.instance.InstanceManagerIsUpgrading.Store(false)
			log.Trace("restartInstanceManager: error while upgrading instance manager", "err", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Unfortunately this point, if everything is right, will not be reached.
		// At this stage we are running the new version of the instance manager
		// and not the old one.
		if err := writeTextResponse(w, "restartInstanceManager", "Unreachable OK"); err != nil {
			log.Trace("restartInstanceManger: failed to write response in unreachable code block", "err", err)
		}

		log.Trace("restartInstanceManager: exit (unreachable)")
	}
}

// nolint: gocognit
func (ws *remoteWebserverEndpoints) backup(w http.ResponseWriter, req *http.Request) {
	log.Trace("request method", "method", req.Method)

	switch req.Method {
	case http.MethodGet:
		if ws.currentBackup == nil {
			sendJSONResponseWithData(w, 200, struct{}{}, "backup")
			return
		}

		res := Response[BackupResultData]{
			Data: &ws.currentBackup.data,
		}
		if ws.currentBackup.err != nil {
			res.Error = &Error{
				Code:    "BACKUP_STATUS_CONTAINS_ERROR",
				Message: ws.currentBackup.err.Error(),
			}
		}

		sendJSONResponse(w, 200, res, "backup")
		return

	case http.MethodPost:
		var p StartBackupRequest
		err := json.NewDecoder(req.Body).Decode(&p)
		if err != nil {
			sendBadRequestJSONResponse(w, "FAILED_TO_PARSE_REQUEST", "Failed to parse request body", "backup")
			return
		}
		defer func() {
			if err := req.Body.Close(); err != nil {
				log.Error(err, "while closing the body")
			}
		}()

		// TODO: There is a race condition here: ws.currentBackup is both being read and written to in this handler, without
		// any locking. That ain't good.

		if ws.currentBackup != nil {
			if !p.Force {
				sendUnprocessableEntityJSONResponse(w, "PROCESS_ALREADY_RUNNING", "", "backup")
				return
			}
			if err := ws.currentBackup.closeConnection(p.BackupName); err != nil {
				if !errors.Is(err, sql.ErrConnDone) {
					log.Error(err, "Error while closing backup connection (start)")
				}
			}
		}
		ws.currentBackup, err = newBackupConnection(
			req.Context(),
			ws.instance,
			p.BackupName,
			p.ImmediateCheckpoint,
			p.WaitForArchive,
		)
		if err != nil {
			sendUnprocessableEntityJSONResponse(w, "CANNOT_INITIALIZE_CONNECTION", err.Error(), "backup")
			return
		}
		go ws.currentBackup.startBackup(context.Background(), p.BackupName)
		sendJSONResponseWithData(w, 200, struct{}{}, "backup")
		return

	case http.MethodPut:
		var p StopBackupRequest
		err := json.NewDecoder(req.Body).Decode(&p)
		if err != nil {
			sendBadRequestJSONResponse(w, "FAILED_TO_PARSE_REQUEST", "Failed to parse request body", "backup")
			return
		}
		defer func() {
			if err := req.Body.Close(); err != nil {
				log.Error(err, "while closing the body")
			}
		}()
		if ws.currentBackup == nil {
			sendBadRequestJSONResponse(w, "NO_ONGOING_BACKUP", "", "backup")
			return
		}

		if ws.currentBackup.data.BackupName != p.BackupName {
			sendUnprocessableEntityJSONResponse(w, "NOT_CURRENT_RUNNING_BACKUP",
				fmt.Sprintf("Phase is: %s", ws.currentBackup.data.Phase), "backup")
			return
		}

		if ws.currentBackup.data.Phase == Closing {
			sendJSONResponseWithData(w, 200, struct{}{}, "backup")
			return
		}

		if ws.currentBackup.data.Phase != Started {
			sendUnprocessableEntityJSONResponse(w, "CANNOT_CLOSE_NOT_STARTED",
				fmt.Sprintf("Phase is: %s", ws.currentBackup.data.Phase), "backup")
			return
		}

		if ws.currentBackup.err != nil {
			if err := ws.currentBackup.closeConnection(p.BackupName); err != nil {
				if !errors.Is(err, sql.ErrConnDone) {
					log.Error(err, "Error while closing backup connection (stop)")
				}
			}

			sendJSONResponseWithData(w, 200, struct{}{}, "backup")
			return
		}
		ws.currentBackup.setPhase(Closing, p.BackupName)
		go ws.currentBackup.stopBackup(context.Background(), p.BackupName)
		sendJSONResponseWithData(w, 200, struct{}{}, "backup")
		return
	}
}

func (ws *remoteWebserverEndpoints) pgArchivePartial(w http.ResponseWriter, req *http.Request) {
	if !ws.instance.IsFenced() {
		sendBadRequestJSONResponse(w, "NOT_FENCED", "", "pgArchivePartial")
		return
	}

	var cluster apiv1.Cluster
	if err := ws.typedClient.Get(req.Context(),
		client.ObjectKey{
			Namespace: ws.instance.GetNamespaceName(),
			Name:      ws.instance.GetClusterName(),
		},
		&cluster); err != nil {
		sendBadRequestJSONResponse(w, "NO_CLUSTER_FOUND", err.Error(), "pgArchivePartial")
		return
	}

	if cluster.Status.TargetPrimary != ws.instance.GetPodName() ||
		cluster.Status.CurrentPrimary != ws.instance.GetPodName() {
		sendBadRequestJSONResponse(w, "NOT_EXPECTED_PRIMARY", "", "pgArchivePartial")
		return
	}

	out, err := ws.instance.GetPgControldata()
	if err != nil {
		log.Debug("Instance pg_controldata endpoint failing", "err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := utils.ParsePgControldataOutput(out)
	walFile := data[utils.PgControlDataKeyREDOWALFile]
	if walFile == "" {
		sendBadRequestJSONResponse(w, "COULD_NOT_PARSE_REDOWAL_FILE", "", "pgArchivePartial")
		return
	}

	pgData := os.Getenv("PGDATA")
	walRelativePath := path.Join("pg_wal", walFile)
	partialWalFileRelativePath := fmt.Sprintf("%s.partial", walRelativePath)
	walFileAbsolutePath := path.Join(pgData, walRelativePath)
	partialWalFileAbsolutePath := path.Join(pgData, partialWalFileRelativePath)

	if err := os.Link(walFileAbsolutePath, partialWalFileAbsolutePath); err != nil {
		log.Error(err, "failed to get pg_controldata")
		sendBadRequestJSONResponse(w, "ERROR_WHILE_CREATING_SYMLINK", err.Error(), "pgArchivePartial")
		return
	}

	defer func() {
		if err := fileutils.RemoveFile(partialWalFileAbsolutePath); err != nil {
			log.Error(err, "while deleting the partial wal file symlink")
		}
	}()

	options := []string{constants.WalArchiveCommand, partialWalFileRelativePath}
	walArchiveCmd := exec.Command("/controller/manager", options...) // nolint: gosec
	walArchiveCmd.Dir = pgData
	if err := execlog.RunBuffering(walArchiveCmd, "wal-archive-partial"); err != nil {
		sendBadRequestJSONResponse(w, "ERROR_WHILE_EXECUTING_WAL_ARCHIVE", err.Error(), "pgArchivePartial")
		return
	}

	sendJSONResponseWithData(w, 200, walFile, "pgArchivePartial")
}
