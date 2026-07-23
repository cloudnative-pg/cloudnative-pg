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
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

// NewBootstrapWebServer builds the status web server used during the in-process
// bootstrap phase, before the controller-runtime manager and the full remote
// web server exist. It serves only the probe and status endpoints on the status
// port: the bootstrap semantics (startup skipped, status 503, readiness red)
// come from the instance bootstrap flag, which is set for the whole lifetime of
// this server. The full NewRemoteWebServer is deliberately not reused: it needs
// the online-upgrade cancel function and exit conditions that only exist after
// the manager is wired, and it exposes backup/update surface that is undefined
// mid-bootstrap.
func NewBootstrapWebServer(instance *postgres.Instance) *Webserver {
	endpoints := remoteWebserverEndpoints{instance: instance}

	serveMux := http.NewServeMux()
	// While bootstrapping, pgStatus replies 503 and isServerStartedUp replies
	// "Skipped" because the bootstrap flag is set; isServerReady replies not
	// ready because the postmaster has not run yet (CanCheckReadiness is false).
	serveMux.HandleFunc(url.PathPgStatus, endpoints.pgStatus)
	serveMux.HandleFunc(url.PathReady, endpoints.isServerReady)
	serveMux.HandleFunc(url.PathStartup, endpoints.isServerStartedUp)
	// Liveness must pass while the container is initializing the data directory,
	// otherwise the kubelet would kill a legitimately long bootstrap.
	serveMux.HandleFunc(url.PathHealth, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "OK")
	})

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
			// RequestClientCert mirrors the full remote web server: no endpoint
			// here needs client authentication, so the certificate is optional.
			ClientAuth: tls.RequestClientCert,
		}
	}

	return NewWebServer(server)
}
