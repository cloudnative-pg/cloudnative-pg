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
	"fmt"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/concurrency"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/upgrade"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

type remoteWebserverEndpoints struct {
	typedClient client.Client
	instance    *postgres.Instance
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
		typedClient: typedClient,
		instance:    instance,
	}
	serveMux := http.NewServeMux()
	serveMux.HandleFunc(url.PathHealth, endpoints.isServerHealthy)
	serveMux.HandleFunc(url.PathReady, endpoints.isServerReady)
	serveMux.HandleFunc(url.PathPgStatus, endpoints.pgStatus)
	serveMux.HandleFunc(url.PathUpdate,
		endpoints.updateInstanceManager(cancelFunc, exitedConditions))

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", url.StatusPort),
		Handler:           serveMux,
		ReadTimeout:       DefaultReadTimeout,
		ReadHeaderTimeout: DefaultReadHeaderTimeout,
	}

	return NewWebServer(instance, server), nil
}

func (ws *remoteWebserverEndpoints) isServerHealthy(w http.ResponseWriter, r *http.Request) {
	// If `pg_rewind` is running the Pod is starting up.
	// We need to report it healthy to avoid being killed by the kubelet.
	// Same goes for instances with fencing on.
	if ws.instance.PgRewindIsRunning || ws.instance.MightBeUnavailable() {
		log.Trace("Liveness probe skipped")
		_, _ = fmt.Fprint(w, "Skipped")
	}

	err := ws.instance.IsServerHealthy()
	if err != nil {
		log.Info("Liveness probe failing", "err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Trace("Liveness probe succeeding")
	_, _ = fmt.Fprint(w, "OK")
}

// This is the readiness probe
func (ws *remoteWebserverEndpoints) isServerReady(w http.ResponseWriter, r *http.Request) {
	if err := ws.instance.IsServerReady(); err != nil {
		log.Info("Readiness probe failing", "err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Trace("Readiness probe succeeding")
	_, _ = fmt.Fprint(w, "OK")
}

// This probe is for the instance status, including replication
func (ws *remoteWebserverEndpoints) pgStatus(w http.ResponseWriter, r *http.Request) {
	// Extract the status of the current instance
	status, err := ws.instance.GetStatus()
	if err != nil {
		log.Info(
			"Instance status probe failing",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Marshal the status back to the operator
	log.Trace("Instance status probe succeeding")
	js, err := json.Marshal(status)
	if err != nil {
		log.Info(
			"Internal error marshalling instance status",
			"err", err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(js)
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
