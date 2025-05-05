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

package probes

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	cnpgUrl "github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	postgresSpec "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// livenessPingerCfg if the configuration of the instance
// reachability checker
type livenessPingerCfg struct {
	Enabled           bool          `json:"enabled,omitempty"`
	RequestTimeout    time.Duration `json:"requestTimeout,omitempty"`
	ConnectionTimeout time.Duration `json:"connectionTimeout,omitempty"`
}

// newLivenessPingerConfigFromCluster creates a new pinger configuration from the annotations
// in the passed cluster definition
func newLivenessPingerConfigFromCluster(ctx context.Context, cluster *apiv1.Cluster) (*livenessPingerCfg, error) {
	const (
		// defaultRequestTimeout is the default value of the request timeout
		defaultRequestTimeout = 500 * time.Millisecond

		// defaultConnectionTimeout is the default value of the connection timeout
		defaultConnectionTimeout = 1000 * time.Millisecond
	)

	contextLogger := log.FromContext(ctx)

	v, ok := cluster.Annotations[utils.LivenessPingerAnnotationName]
	if !ok {
		contextLogger.Debug("pinger config not found in the cluster annotations")
		return nil, nil
	}

	var cfg livenessPingerCfg
	if err := json.Unmarshal([]byte(v), &cfg); err != nil {
		contextLogger.Error(err, "failed to unmarshal pinger config")
		return nil, fmt.Errorf("while unmarshalling pinger config: %w", err)
	}

	if cfg.RequestTimeout == 0 {
		cfg.RequestTimeout = defaultRequestTimeout
	}
	if cfg.ConnectionTimeout == 0 {
		cfg.ConnectionTimeout = defaultConnectionTimeout
	}

	return &cfg, nil
}

// pinger can check if a certain instance is reachable by using
// the failsafe REST endpoint
type pinger struct {
	dialer *net.Dialer
	client *http.Client

	config livenessPingerCfg
}

// buildInstanceReachabilityChecker creates a new instance reachability checker by loading
// the server CA certificate from the same location that will be used by PostgreSQL.
// In this case, we avoid using the API Server as it may be unreliable.
func buildInstanceReachabilityChecker(cfg livenessPingerCfg) (*pinger, error) {
	certificateLocation := postgresSpec.ServerCACertificateLocation
	caCertificate, err := os.ReadFile(certificateLocation) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("while reading server CA certificate [%s]: %w", certificateLocation, err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertificate)

	tlsConfig := certs.NewTLSConfigFromCertPool(caCertPool)

	dialer := &net.Dialer{Timeout: cfg.ConnectionTimeout}

	client := http.Client{
		Transport: &http.Transport{
			DialContext:     dialer.DialContext,
			TLSClientConfig: tlsConfig,
		},
		Timeout: cfg.RequestTimeout,
	}

	return &pinger{
		dialer: dialer,
		client: &client,
		config: cfg,
	}, nil
}

// ping checks if the instance with the passed coordinates is reachable
// by calling the failsafe endpoint.
func (e *pinger) ping(host, ip string) error {
	failsafeURL := url.URL{
		Scheme: "https",
		Host:   fmt.Sprintf("%s:%d", ip, cnpgUrl.StatusPort),
		Path:   cnpgUrl.PathFailSafe,
	}

	var res *http.Response
	var err error
	if res, err = e.client.Get(failsafeURL.String()); err != nil {
		return &pingError{
			host:   host,
			err:    err,
			config: e.config,
		}
	}

	_ = res.Body.Close()

	return nil
}

func (e pinger) ensureInstancesAreReachable(cluster *apiv1.Cluster) error {
	for name, state := range cluster.Status.InstancesReportedState {
		host := string(name)
		ip := state.IP
		if err := e.ping(host, ip); err != nil {
			return err
		}
	}

	return nil
}

// pingError is raised when the instance connectivity test failed.
type pingError struct {
	host string
	ip   string

	config livenessPingerCfg

	err error
}

// Error implements the error interface
func (e *pingError) Error() string {
	return fmt.Sprintf(
		"instance connectivity error for instance [%s] with ip [%s] (requestTimeout:%v connectionTimeout:%v): %s",
		e.host,
		e.ip,
		e.config.RequestTimeout,
		e.config.ConnectionTimeout,
		e.err.Error())
}

// Unwrap implements the error interface
func (e *pingError) Unwrap() error {
	return e.err
}
