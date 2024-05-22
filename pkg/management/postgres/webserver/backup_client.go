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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
)

// backupClient a client to interact with the instance backup endpoints
type backupClient struct {
	cli *http.Client
}

// BackupClient is a struct capable of interacting with the instance backup endpoints
type BackupClient interface {
	StatusWithErrors(ctx context.Context, pod *corev1.Pod) (*Response[BackupResultData], error)
	Start(ctx context.Context, pod *corev1.Pod, sbq StartBackupRequest) error
	Stop(ctx context.Context, pod *corev1.Pod, sbq StopBackupRequest) error
}

// NewBackupClient creates a client capable of interacting with the instance backup endpoints
func NewBackupClient() BackupClient {
	const connectionTimeout = 2 * time.Second
	const requestTimeout = 30 * time.Second

	// We want a connection timeout to prevent waiting for the default
	// TCP connection timeout (30 seconds) on lost SYN packets
	dialer := &net.Dialer{
		Timeout: connectionTimeout,
	}
	timeoutClient := &http.Client{
		Transport: &http.Transport{
			DialContext: dialer.DialContext,
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				tlsConfig, err := certs.GetTLSConfigFromContext(ctx)
				if err != nil {
					return nil, err
				}
				tlsDialer := tls.Dialer{
					NetDialer: dialer,
					Config:    tlsConfig,
				}
				return tlsDialer.DialContext(ctx, network, addr)
			},
		},
		Timeout: requestTimeout,
	}
	return &backupClient{cli: timeoutClient}
}

// StatusWithErrors retrieves the current status of the backup.
// Returns the response body in case there is an error in the request
func (c *backupClient) StatusWithErrors(ctx context.Context, pod *corev1.Pod) (*Response[BackupResultData], error) {
	scheme := instance.GetStatusSchemeFromPod(pod)
	httpURL := url.Build(scheme.ToString(), pod.Status.PodIP, url.PathPgModeBackup, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, "GET", httpURL, nil)
	if err != nil {
		return nil, err
	}

	return executeRequestWithError[BackupResultData](ctx, c.cli, req, true)
}

// Start runs the pg_start_backup
func (c *backupClient) Start(ctx context.Context, pod *corev1.Pod, sbq StartBackupRequest) error {
	scheme := instance.GetStatusSchemeFromPod(pod)
	httpURL := url.Build(scheme.ToString(), pod.Status.PodIP, url.PathPgModeBackup, url.StatusPort)

	// Marshalling the payload to JSON
	jsonBody, err := json.Marshal(sbq)
	if err != nil {
		return fmt.Errorf("failed to marshal start payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", httpURL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	_, err = executeRequestWithError[struct{}](ctx, c.cli, req, false)
	return err
}

// Stop runs the command pg_stop_backup
func (c *backupClient) Stop(ctx context.Context, pod *corev1.Pod, sbq StopBackupRequest) error {
	scheme := instance.GetStatusSchemeFromPod(pod)
	httpURL := url.Build(scheme.ToString(), pod.Status.PodIP, url.PathPgModeBackup, url.StatusPort)
	// Marshalling the payload to JSON
	jsonBody, err := json.Marshal(sbq)
	if err != nil {
		return fmt.Errorf("failed to marshal stop payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PUT", httpURL, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	_, err = executeRequestWithError[BackupResultData](ctx, c.cli, req, false)
	return err
}

func executeRequestWithError[T any](
	ctx context.Context,
	cli *http.Client,
	req *http.Request,
	ignoreBodyErrors bool,
) (*Response[T], error) {
	contextLogger := log.FromContext(ctx)

	resp, err := cli.Do(req)
	if err != nil {
		return nil, fmt.Errorf("while executing http request: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			contextLogger.Error(err, "while closing response body")
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("while reading the response body: %w", err)
	}

	if resp.StatusCode == http.StatusInternalServerError {
		return nil, fmt.Errorf("encountered an internal server error status code 500 with body: %s", string(body))
	}

	var result Response[T]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("while unmarshalling the body, body: %s err: %w", string(body), err)
	}
	if result.Error != nil && !ignoreBodyErrors {
		return nil, fmt.Errorf("body contained an error code: %s and message: %s",
			result.Error.Code, result.Error.Message)
	}

	return &result, nil
}
