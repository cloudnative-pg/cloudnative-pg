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
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"go.uber.org/multierr"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/probes"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/upgrade"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const errCodeAnotherRequestInProgress = "ANOTHER_REQUEST_IN_PROGRESS"

// IsRetryableError checks if the error is retryable
func IsRetryableError(err *Error) bool {
	if err == nil {
		return false
	}
	return err.Code == errCodeAnotherRequestInProgress
}

type remoteWebserverEndpoints struct {
	typedClient          client.Client
	instance             *postgres.Instance
	currentBackup        *backupConnection
	ongoingBackupRequest sync.Mutex
	// Stateful probes with persistent caches for API server resilience
	livenessChecker  probes.Checker
	readinessChecker probes.Checker
	startupChecker   probes.Checker
}

// StartBackupRequest the required data to execute the pg_start_backup
type StartBackupRequest struct {
	ImmediateCheckpoint bool   `json:"immediateCheckpoint"`
	WaitForArchive      bool   `json:"waitForArchive"`
	BackupName          string `json:"backupName"`
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

	// Create a shared cache for all probe types to reduce memory usage and ensure consistency
	sharedCache := probes.NewClusterCache(
		typedClient,
		client.ObjectKey{Namespace: instance.GetNamespaceName(), Name: instance.GetClusterName()},
	)

	endpoints := remoteWebserverEndpoints{
		typedClient:      typedClient,
		instance:         instance,
		livenessChecker:  probes.NewLivenessChecker(instance, sharedCache),
		readinessChecker: probes.NewReadinessChecker(instance, sharedCache),
		startupChecker:   probes.NewStartupChecker(instance, sharedCache),
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc(url.PathFailSafe, endpoints.failSafe)
	serveMux.HandleFunc(url.PathPgModeBackup, endpoints.backup)
	serveMux.HandleFunc(url.PathHealth, endpoints.isServerHealthy)
	serveMux.HandleFunc(url.PathReady, endpoints.isServerReady)
	serveMux.HandleFunc(url.PathStartup, endpoints.isServerStartedUp)
	serveMux.HandleFunc(url.PathPgStatus, endpoints.pgStatus)
	serveMux.HandleFunc(url.PathPgArchivePartial, endpoints.pgArchivePartial)
	serveMux.HandleFunc(url.PathPGControlData, endpoints.pgControlData)
	serveMux.HandleFunc(url.PathUpdate, endpoints.updateInstanceManager(cancelFunc, exitedConditions))

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
				return instance.GetServerCertificate(), nil
			},
		}
	}

	srv := NewWebServer(server)

	srv.routines = append(srv.routines, endpoints.cleanupStaleCollections)

	return srv, nil
}

func (ws *remoteWebserverEndpoints) cleanupStaleCollections(ctx context.Context) {
	closeBackupConnection := func(bc *backupConnection) {
		log := log.WithValues(
			"backupName", bc.data.BackupName,
			"phase", bc.data.Phase,
		)
		log.Warning("Closing stale PostgreSQL backup connection")

		if err := bc.conn.Close(); err != nil {
			bc.err = multierr.Append(bc.err, err)
			log.Error(err, "Error while closing stale PostgreSQL backup connection")
		}
		bc.data.Phase = Completed
	}

	innerRoutine := func() {
		if ws == nil {
			return
		}
		bc := ws.currentBackup
		if bc == nil || bc.conn == nil {
			return
		}

		ws.ongoingBackupRequest.Lock()
		defer ws.ongoingBackupRequest.Unlock()

		if bc.data.Phase == Completed || bc.data.BackupName == "" {
			return
		}

		if bc.err != nil {
			closeBackupConnection(bc)
			return
		}

		if err := bc.conn.PingContext(ctx); err != nil {
			bc.err = fmt.Errorf("error while pinging: %w", err)
			closeBackupConnection(bc)
			return
		}

		var backup apiv1.Backup

		err := ws.typedClient.Get(ctx, client.ObjectKey{
			Namespace: ws.instance.GetNamespaceName(),
			Name:      bc.data.BackupName,
		}, &backup)
		if apierrs.IsNotFound(err) {
			bc.err = fmt.Errorf("backup %s not found", bc.data.BackupName)
			closeBackupConnection(bc)
			return
		}
		if err != nil {
			return
		}

		if backup.Status.IsDone() {
			bc.err = fmt.Errorf("backup %s is done", bc.data.BackupName)
			closeBackupConnection(bc)
			return
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Minute):
			innerRoutine()
		}
	}
}

// isServerStartedUp evaluates the startup probe
func (ws *remoteWebserverEndpoints) isServerStartedUp(w http.ResponseWriter, req *http.Request) {
	// If `pg_rewind` is running, it means that the Pod is starting up.
	// We need to report it healthy to avoid being killed by the kubelet.
	if ws.instance.PgRewindIsRunning || ws.instance.MightBeUnavailable() {
		log.Trace("Startup probe skipped")
		_, _ = fmt.Fprint(w, "Skipped")
		return
	}

	ws.startupChecker.IsHealthy(req.Context(), w)
}

// This is the failsafe entrypoint
func (ws *remoteWebserverEndpoints) failSafe(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprint(w, "OK")
}

// This is the liveness probe
func (ws *remoteWebserverEndpoints) isServerHealthy(w http.ResponseWriter, req *http.Request) {
	ws.livenessChecker.IsHealthy(req.Context(), w)
}

// This is the readiness probe
func (ws *remoteWebserverEndpoints) isServerReady(w http.ResponseWriter, req *http.Request) {
	if !ws.instance.CanCheckReadiness() {
		http.Error(w, "instance is not ready yet", http.StatusInternalServerError)
		return
	}

	ws.readinessChecker.IsHealthy(req.Context(), w)
}

// This probe is for the instance status, including replication
func (ws *remoteWebserverEndpoints) pgStatus(w http.ResponseWriter, r *http.Request) {
	// Extract the status of the current instance
	status, err := ws.instance.GetStatus(r.Context())
	if err != nil {
		log.Debug(
			"Instance status probe failing",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Marshal the status back to the operator
	log.Trace("Instance status probe succeeding")
	js, err := json.Marshal(status)
	if err != nil {
		log.Warning(
			"Internal error marshalling instance status",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(js)
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

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(res)
}

// updateInstanceManager replace the instance with one in the
// new binary
func (ws *remoteWebserverEndpoints) updateInstanceManager(
	cancelFunc context.CancelFunc,
	exitedCondition concurrency.MultipleExecuted,
) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		// No need to handle this request if it is not a put
		if r.Method != http.MethodPut {
			http.Error(w, "wrong method used", http.StatusMethodNotAllowed)
			return
		}

		// No need to do anything if we are already upgrading
		if !ws.instance.InstanceManagerIsUpgrading.CompareAndSwap(false, true) {
			http.Error(w, "instance manager is already upgrading", http.StatusTeapot)
			return
		}
		// If we get here, the InstanceManagerIsUpgrading flag was set and
		// we will perform the upgrade. Ensure we unset the flag in the end
		defer ws.instance.InstanceManagerIsUpgrading.Store(false)

		err := upgrade.FromReader(cancelFunc, exitedCondition, ws.typedClient, ws.instance, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Unfortunately this point, if everything is right, will not be reached.
		// At this stage we are running the new version of the instance manager
		// and not the old one.
		_, _ = fmt.Fprint(w, "OK")
	}
}

// nolint: gocognit
func (ws *remoteWebserverEndpoints) backup(w http.ResponseWriter, req *http.Request) {
	log.Trace("request method", "method", req.Method)
	if !ws.ongoingBackupRequest.TryLock() {
		sendUnprocessableEntityJSONResponse(w, errCodeAnotherRequestInProgress, "")
		return
	}
	defer ws.ongoingBackupRequest.Unlock()

	switch req.Method {
	case http.MethodGet:
		if ws.currentBackup == nil {
			sendJSONResponseWithData(w, 200, struct{}{})
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

		sendJSONResponse(w, 200, res)
		return

	case http.MethodPost:
		var p StartBackupRequest
		err := json.NewDecoder(req.Body).Decode(&p)
		if err != nil {
			sendBadRequestJSONResponse(w, "FAILED_TO_PARSE_REQUEST", "Failed to parse request body")
			return
		}
		defer func() {
			if err := req.Body.Close(); err != nil {
				log.Error(err, "while closing the body")
			}
		}()
		if ws.currentBackup != nil {
			log.Debug("trying to close the current backup connection",
				"backupName", ws.currentBackup.data.BackupName,
			)
			if err := ws.currentBackup.conn.Close(); err != nil {
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
			sendUnprocessableEntityJSONResponse(w, "CANNOT_INITIALIZE_CONNECTION", err.Error())
			return
		}
		go ws.currentBackup.startBackup(context.Background(), &ws.ongoingBackupRequest)

		res := Response[BackupResultData]{
			Data: &ws.currentBackup.data,
		}
		sendJSONResponseWithData(w, 200, res)
		return

	case http.MethodPut:
		var p StopBackupRequest
		err := json.NewDecoder(req.Body).Decode(&p)
		if err != nil {
			sendBadRequestJSONResponse(w, "FAILED_TO_PARSE_REQUEST", "Failed to parse request body")
			return
		}
		defer func() {
			if err := req.Body.Close(); err != nil {
				log.Error(err, "while closing the body")
			}
		}()
		if ws.currentBackup == nil {
			sendBadRequestJSONResponse(w, "NO_ONGOING_BACKUP", "")
			return
		}

		if ws.currentBackup.data.BackupName != p.BackupName {
			sendUnprocessableEntityJSONResponse(w, "NOT_CURRENT_RUNNING_BACKUP",
				fmt.Sprintf("Phase is: %s", ws.currentBackup.data.Phase))
			return
		}

		if ws.currentBackup.err != nil {
			if err := ws.currentBackup.conn.Close(); err != nil {
				if !errors.Is(err, sql.ErrConnDone) {
					log.Error(err, "Error while closing backup connection (stop)")
				}
			}

			sendUnprocessableEntityJSONResponse(w, "BACKUP_FAILED", ws.currentBackup.err.Error())
			return
		}

		res := Response[BackupResultData]{
			Data: &ws.currentBackup.data,
		}

		if ws.currentBackup.data.Phase == Closing {
			sendJSONResponseWithData(w, 200, res)
			return
		}

		if ws.currentBackup.data.Phase != Started {
			sendUnprocessableEntityJSONResponse(w, "CANNOT_CLOSE_NOT_STARTED",
				fmt.Sprintf("Phase is: %s", ws.currentBackup.data.Phase))
			return
		}

		ws.currentBackup.data.Phase = Closing

		go ws.currentBackup.stopBackup(context.Background(), &ws.ongoingBackupRequest)
		sendJSONResponseWithData(w, 200, res)
		return
	}
}

func (ws *remoteWebserverEndpoints) pgArchivePartial(w http.ResponseWriter, req *http.Request) {
	if !ws.instance.IsFenced() {
		sendBadRequestJSONResponse(w, "NOT_FENCED", "")
		return
	}

	var cluster apiv1.Cluster
	if err := ws.typedClient.Get(req.Context(),
		client.ObjectKey{
			Namespace: ws.instance.GetNamespaceName(),
			Name:      ws.instance.GetClusterName(),
		},
		&cluster); err != nil {
		sendBadRequestJSONResponse(w, "NO_CLUSTER_FOUND", err.Error())
		return
	}

	if cluster.Status.TargetPrimary != ws.instance.GetPodName() ||
		cluster.Status.CurrentPrimary != ws.instance.GetPodName() {
		sendBadRequestJSONResponse(w, "NOT_EXPECTED_PRIMARY", "")
		return
	}

	out, err := ws.instance.GetPgControldata()
	if err != nil {
		log.Debug("Instance pg_controldata endpoint failing", "err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := utils.ParsePgControldataOutput(out)
	walFile := data.GetREDOWALFile()
	if walFile == "" {
		sendBadRequestJSONResponse(w, "COULD_NOT_PARSE_REDOWAL_FILE", "")
		return
	}

	pgData := os.Getenv("PGDATA")
	walRelativePath := path.Join("pg_wal", walFile)
	partialWalFileRelativePath := fmt.Sprintf("%s.partial", walRelativePath)
	walFileAbsolutePath := path.Join(pgData, walRelativePath)
	partialWalFileAbsolutePath := path.Join(pgData, partialWalFileRelativePath)

	if err := os.Link(walFileAbsolutePath, partialWalFileAbsolutePath); err != nil {
		log.Error(err, "failed to get pg_controldata")
		sendBadRequestJSONResponse(w, "ERROR_WHILE_CREATING_SYMLINK", err.Error())
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
		sendBadRequestJSONResponse(w, "ERROR_WHILE_EXECUTING_WAL_ARCHIVE", err.Error())
		return
	}

	sendJSONResponseWithData(w, 200, walFile)
}
