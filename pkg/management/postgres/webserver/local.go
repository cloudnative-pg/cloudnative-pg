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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

type localWebserverEndpoints struct {
	typedClient   client.Client
	instance      *postgres.Instance
	eventRecorder record.EventRecorder
}

// NewLocalWebServer returns a webserver that allows connection only from localhost
func NewLocalWebServer(instance *postgres.Instance) (*Webserver, error) {
	typedClient, err := management.NewControllerRuntimeClient()
	if err != nil {
		return nil, fmt.Errorf("creating controller-runtine client: %v", err)
	}
	eventRecorder, err := management.NewEventRecorder()
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes event recorder: %v", err)
	}

	endpoints := localWebserverEndpoints{
		typedClient:   typedClient,
		instance:      instance,
		eventRecorder: eventRecorder,
	}

	serveMux := http.NewServeMux()
	serveMux.HandleFunc(url.PathCache, endpoints.serveCache)
	serveMux.HandleFunc(url.PathPgBackup, endpoints.requestBackup)

	server := &http.Server{
		Addr:              fmt.Sprintf("localhost:%d", url.LocalPort),
		Handler:           serveMux,
		ReadHeaderTimeout: DefaultReadTimeout,
		ReadTimeout:       DefaultReadTimeout,
	}

	webserver := NewWebServer(instance, server)

	return webserver, nil
}

// This probe is for the instance status, including replication
func (ws *localWebserverEndpoints) serveCache(w http.ResponseWriter, r *http.Request) {
	requestedObject := strings.TrimPrefix(r.URL.Path, url.PathCache)

	log.Debug("Cached object request received")

	var js []byte
	switch requestedObject {
	case cache.ClusterKey:
		response, err := cache.LoadClusterUnsafe()
		if errors.Is(err, cache.ErrCacheMiss) {
			w.WriteHeader(http.StatusNotFound)
			return
		} else if err != nil {
			log.Error(err, "while loading cached cluster")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		js, err = json.Marshal(response)
		if err != nil {
			log.Error(err, "while unmarshalling cached cluster")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case cache.WALRestoreKey, cache.WALArchiveKey:
		response, err := cache.LoadEnv(requestedObject)
		if errors.Is(err, cache.ErrCacheMiss) {
			w.WriteHeader(http.StatusNotFound)
			return
		} else if err != nil {
			log.Error(err, "while loading cached env")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		js, err = json.Marshal(response)
		if err != nil {
			log.Error(err, "while unmarshalling cached env")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		log.Debug("Unsupported cached object type")
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(js)
}

// This function schedule a backup
func (ws *localWebserverEndpoints) requestBackup(w http.ResponseWriter, r *http.Request) {
	var cluster apiv1.Cluster
	var backup apiv1.Backup

	ctx := context.Background()

	backupName := r.URL.Query().Get("name")
	if len(backupName) == 0 {
		http.Error(w, "Missing backup name parameter", http.StatusBadRequest)
		return
	}

	if err := ws.typedClient.Get(ctx, client.ObjectKey{
		Namespace: ws.instance.Namespace,
		Name:      ws.instance.ClusterName,
	}, &cluster); err != nil {
		http.Error(
			w,
			fmt.Sprintf("error while getting cluster: %v", err.Error()),
			http.StatusInternalServerError)
		return
	}

	if err := ws.typedClient.Get(ctx, client.ObjectKey{
		Namespace: ws.instance.Namespace,
		Name:      backupName,
	}, &backup); err != nil {
		http.Error(
			w,
			fmt.Sprintf("error while getting backup: %v", err.Error()),
			http.StatusInternalServerError)
		return
	}

	switch backup.Spec.Method {
	case apiv1.BackupMethodBarmanObjectStore:
		if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
			http.Error(w, "Barman backup not configured in the cluster", http.StatusConflict)
			return
		}

		if err := ws.startBarmanBackup(ctx, &cluster, &backup); err != nil {
			http.Error(
				w,
				fmt.Sprintf("error while requesting backup: %v", err.Error()),
				http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, "OK")

	case apiv1.BackupMethodPlugin:
		if backup.Spec.PluginConfiguration.IsEmpty() {
			http.Error(w, "Plugin backup not configured in the cluster", http.StatusConflict)
			return
		}

		ws.startPluginBackup(ctx, &cluster, &backup)
		_, _ = fmt.Fprint(w, "OK")

	default:
		http.Error(
			w,
			fmt.Sprintf("Unknown backup method: %v", backup.Spec.Method),
			http.StatusBadRequest)
	}
}

func (ws *localWebserverEndpoints) startBarmanBackup(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) error {
	backupLog := log.WithValues(
		"backupName", backup.Name,
		"backupNamespace", backup.Name)

	backupCommand, err := postgres.NewBarmanBackupCommand(
		cluster,
		backup,
		ws.typedClient,
		ws.eventRecorder,
		ws.instance,
		backupLog,
	)
	if err != nil {
		return fmt.Errorf("while initializing backup: %w", err)
	}

	if err := backupCommand.Start(ctx); err != nil {
		return fmt.Errorf("while starting backup: %w", err)
	}

	return nil
}

func (ws *localWebserverEndpoints) startPluginBackup(
	ctx context.Context,
	cluster *apiv1.Cluster,
	backup *apiv1.Backup,
) {
	cmd := NewPluginBackupCommand(cluster, backup, ws.typedClient, ws.eventRecorder)
	cmd.Start(ctx)
	cmd.Close()
}
