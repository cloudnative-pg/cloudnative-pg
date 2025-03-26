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
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// ErrorContainerNotFound is raised when an Exec call is invoked against
// a non existing container
var ErrorContainerNotFound = fmt.Errorf("container not found")

// ExecCommand executes a command inside the pod, and returns its result
func ExecCommand(
	ctx context.Context,
	client kubernetes.Interface,
	config *rest.Config,
	pod corev1.Pod,
	containerName string,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	// iterate through all containers looking for the one running PostgreSQL.
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

	// Unfortunately RESTClient doesn't still work with contexts but when it
	// will, we'll use the context there.
	//
	// A similar consideration can be applied for the `container` parameter:
	// in this moment we need to specify that parameter in the "Post" request
	// and in the VersionedParams section too. This will hopefully be unified
	// in a next client-go release.
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
		timedCtx, cancelFunc := context.WithTimeout(ctx, *timeout)
		defer cancelFunc()
		ctx = timedCtx
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
