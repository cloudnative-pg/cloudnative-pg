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

package local

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	corev1 "k8s.io/api/core/v1"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/remote"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/url"
)

// BackupClient is the interface to interact with the backup endpoints
type BackupClient interface {
	StatusWithErrors(ctx context.Context, pod *corev1.Pod) (*webserver.Response[webserver.BackupResultData], error)
	Start(ctx context.Context, pod *corev1.Pod, sbq webserver.StartBackupRequest) error
	Stop(ctx context.Context, pod *corev1.Pod, sbq webserver.StopBackupRequest) error
}

// backupClientImpl a client to interact with the instance backup endpoints
type backupClientImpl struct {
	cli *http.Client
}

// StatusWithErrors retrieves the current status of the backup.
// Returns the response body in case there is an error in the request
func (c *backupClientImpl) StatusWithErrors(
	ctx context.Context,
	pod *corev1.Pod,
) (*webserver.Response[webserver.BackupResultData], error) {
	scheme := remote.GetStatusSchemeFromPod(pod)
	httpURL := url.Build(scheme.ToString(), pod.Status.PodIP, url.PathPgModeBackup, url.StatusPort)
	req, err := http.NewRequestWithContext(ctx, "GET", httpURL, nil)
	if err != nil {
		return nil, err
	}

	return executeRequestWithError[webserver.BackupResultData](ctx, c.cli, req, true)
}

// Start runs the pg_start_backup
func (c *backupClientImpl) Start(ctx context.Context, pod *corev1.Pod, sbq webserver.StartBackupRequest) error {
	scheme := remote.GetStatusSchemeFromPod(pod)
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
func (c *backupClientImpl) Stop(ctx context.Context, pod *corev1.Pod, sbq webserver.StopBackupRequest) error {
	scheme := remote.GetStatusSchemeFromPod(pod)
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
	_, err = executeRequestWithError[webserver.BackupResultData](ctx, c.cli, req, false)
	return err
}
