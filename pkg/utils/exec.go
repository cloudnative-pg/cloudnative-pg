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

// Package utils contains otherwise uncategorized kubernetes
// relative functions
package utils

// Look here: https://github.com/kubernetes/kubernetes/blob/release-1.17/test/e2e/framework/exec_util.go
// also here: https://github.com/kubernetes-client/python/blob/master/examples/pod_exec.py //wokeignore:rule=master

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

const (
	execRetryAttempts = 3
	execRetryDelay    = 2 * time.Second
)

// ErrorContainerNotFound is raised when an Exec call is invoked against
// a non existing container
var ErrorContainerNotFound = fmt.Errorf("container not found")

// isRetryableExecError returns true for transient infrastructure errors
func isRetryableExecError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Kubernetes API proxy errors (common in AKS)
	if strings.Contains(errStr, "proxy error") ||
		strings.Contains(errStr, "error dialing backend") {
		return true
	}

	// HTTP 500 errors from API server
	if strings.Contains(errStr, "500 Internal Server Error") ||
		strings.Contains(errStr, "Internal error occurred") {
		return true
	}

	// Network connectivity issues
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "i/o timeout") ||
		strings.Contains(errStr, "TLS handshake timeout") ||
		strings.Contains(errStr, "dial tcp") {
		return true
	}

	// Kubernetes API errors that are typically transient
	if apierrors.IsInternalError(err) ||
		apierrors.IsServerTimeout(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsServiceUnavailable(err) ||
		apierrors.IsTooManyRequests(err) {
		return true
	}

	return false
}

// ExecCommand executes a command inside the pod, automatically retrying
// transient errors like proxy failures or network issues.
func ExecCommand(
	ctx context.Context,
	client kubernetes.Interface,
	config *rest.Config,
	pod corev1.Pod,
	containerName string,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	contextLogger := log.FromContext(ctx).WithValues(
		"pod", pod.Name,
		"namespace", pod.Namespace,
		"container", containerName,
	)

	targetContainer := -1
	for i, cr := range pod.Spec.Containers {
		if cr.Name == containerName {
			targetContainer = i
			break
		}
	}

	if targetContainer < 0 {
		return "", "", ErrorContainerNotFound
	}

	execCtx := ctx
	var cancelFunc context.CancelFunc
	if timeout != nil {
		execCtx, cancelFunc = context.WithTimeout(ctx, *timeout)
		defer cancelFunc()
	}

	var stdout, stderr string
	var execErr error

	err := retry.New(
		retry.Attempts(execRetryAttempts),
		retry.Delay(execRetryDelay),
		retry.DelayType(retry.BackOffDelay),
		retry.Context(execCtx),
		retry.LastErrorOnly(true),
		retry.OnRetry(func(n uint, err error) {
			contextLogger.Info("Retrying kubectl exec",
				"attempt", n+1,
				"error", err.Error(),
			)
		}),
	).Do(
		func() error {
			stdout, stderr, execErr = execCommandOnce(
				execCtx, client, config, pod,
				targetContainer, timeout, command...,
			)

			// Don't retry if context was cancelled or timed out
			if execCtx.Err() != nil {
				return retry.Unrecoverable(execErr)
			}

			if execErr != nil && isRetryableExecError(execErr) {
				return execErr
			}

			// Either success or non-retryable error
			if execErr != nil {
				return retry.Unrecoverable(execErr)
			}

			return nil
		},
	)
	// Return the last attempt's result
	if err != nil {
		return stdout, stderr, execErr
	}

	return stdout, stderr, nil
}

// execCommandOnce performs a single kubectl exec operation without retries
func execCommandOnce(
	ctx context.Context,
	client kubernetes.Interface,
	config *rest.Config,
	pod corev1.Pod,
	targetContainer int,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	req := client.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		Param("container", pod.Spec.Containers[targetContainer].Name)

	newConfig := *config // local copy avoids modifying the passed config arg
	if timeout != nil {
		req.Timeout(*timeout)
		newConfig.Timeout = *timeout
	}

	req.VersionedParams(&corev1.PodExecOptions{
		Container: pod.Spec.Containers[targetContainer].Name,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(&newConfig, "POST", req.URL())
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		retErr := fmt.Errorf("cmd: %s\nerror: %w\nstdErr: %v", command, err, stderr.String())
		return stdout.String(), stderr.String(), retErr
	}

	return stdout.String(), stderr.String(), nil
}
