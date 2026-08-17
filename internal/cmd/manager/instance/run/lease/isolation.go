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

package lease

import (
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/failsafe"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	postgresSpec "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// pinger can check if a certain instance is reachable by using
// the failsafe REST endpoint
type pinger struct {
	dialer *net.Dialer
	client *http.Client

	config *apiv1.IsolationCheckConfiguration
}

// buildInstanceReachabilityChecker creates a new instance reachability checker by loading
// the server CA certificate from the same location that will be used by PostgreSQL.
// In this case, we avoid using the API Server as it may be unreliable.
func buildInstanceReachabilityChecker(cfg *apiv1.IsolationCheckConfiguration) (*pinger, error) {
	if cfg == nil {
		return nil, errors.New("isolation check configuration is nil")
	}

	certificateLocation := postgresSpec.ServerCACertificateLocation
	caCertificate, err := os.ReadFile(filepath.Clean(certificateLocation))
	if err != nil {
		return nil, fmt.Errorf("while reading server CA certificate [%s]: %w", certificateLocation, err)
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCertificate)

	tlsConfig := certs.NewTLSConfigFromCertPool(caCertPool)

	dialer := &net.Dialer{Timeout: time.Duration(cfg.ConnectionTimeout) * time.Millisecond}

	client := http.Client{
		Transport: &http.Transport{
			DialContext:     dialer.DialContext,
			TLSClientConfig: tlsConfig,
		},
		Timeout: time.Duration(cfg.RequestTimeout) * time.Millisecond,
	}

	return &pinger{
		dialer: dialer,
		client: &client,
		config: cfg,
	}, nil
}

// ping checks if the instance with the passed coordinates is reachable by
// calling the failsafe endpoint, and returns the target primary it reports
// (empty if unknown, e.g. a pre-upgrade peer's plain "OK" response).
func (e *pinger) ping(host, ip string) (targetPrimary string, err error) {
	failsafeURL := url.Build("https", ip, url.PathFailSafe, url.StatusPort)

	res, err := e.client.Get(failsafeURL)
	if err != nil {
		return "", &PingError{
			Host:              host,
			IP:                ip,
			RequestTimeout:    e.config.RequestTimeout,
			ConnectionTimeout: e.config.ConnectionTimeout,
			Err:               err,
		}
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", nil
	}
	return failsafe.Parse(body).TargetPrimary, nil
}

func (e pinger) ensureInstancesAreReachable(cluster *apiv1.Cluster, ourIdentity string) error {
	for name, state := range cluster.Status.InstancesReportedState {
		host := string(name)
		ip := state.IP
		target, err := e.ping(host, ip)
		if err != nil {
			return err
		}
		if target != "" && target != ourIdentity {
			return &SupersededError{Host: host, TargetPrimary: target, OurIdentity: ourIdentity}
		}
	}

	return nil
}

// PingError is raised when the instance connectivity test failed. It is
// exported with public, JSON-tagged fields so it can be logged as a
// structured value.
type PingError struct {
	Host              string `json:"host"`
	IP                string `json:"ip"`
	RequestTimeout    int    `json:"requestTimeout"`
	ConnectionTimeout int    `json:"connectionTimeout"`
	Err               error  `json:"error"`
}

// Error implements the error interface
func (e *PingError) Error() string {
	return fmt.Sprintf(
		"instance connectivity error for instance [%s] with ip [%s] (requestTimeout:%v connectionTimeout:%v): %s",
		e.Host,
		e.IP,
		e.RequestTimeout,
		e.ConnectionTimeout,
		e.Err.Error())
}

// Unwrap implements the error interface
func (e *PingError) Unwrap() error {
	return e.Err
}

// SupersededError is raised when a reachable peer reports a target primary
// other than ours: we have already been superseded, regardless of whether
// we can still reach every peer. It is exported with public, JSON-tagged
// fields so it can be logged as a structured value.
type SupersededError struct {
	Host          string `json:"host"`
	TargetPrimary string `json:"targetPrimary"`
	OurIdentity   string `json:"ourIdentity"`
}

// Error implements the error interface
func (e *SupersededError) Error() string {
	return fmt.Sprintf("instance [%s] reports [%s] as the target primary, we are [%s]",
		e.Host, e.TargetPrimary, e.OurIdentity)
}

// shouldStepDown reports whether this primary should stop believing it is
// still the legitimate primary: either because it cannot reach some peer
// instance, or because a reachable peer reports a different target primary
// (see supersededError). It returns (false, nil) without checking anything
// if the isolation check is disabled or the cluster has a single instance.
// On any step-down verdict or checker build failure, reason carries the
// specific cause for the caller to log.
func shouldStepDown(cluster *apiv1.Cluster, ourIdentity string) (stepDown bool, reason error) {
	var cfg *apiv1.IsolationCheckConfiguration
	if cluster.Spec.Probes != nil && cluster.Spec.Probes.Liveness != nil {
		cfg = cluster.Spec.Probes.Liveness.IsolationCheck
	}
	if cfg == nil || cfg.Enabled == nil || !*cfg.Enabled {
		return false, nil
	}
	if cluster.Spec.Instances == 1 {
		return false, nil
	}

	checker, err := buildInstanceReachabilityChecker(cfg)
	if err != nil {
		return false, fmt.Errorf("failed to build instance reachability checker: %w", err)
	}

	if err := checker.ensureInstancesAreReachable(cluster, ourIdentity); err != nil {
		return true, err
	}

	return false, nil
}
