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

// Package podlogs contains code to fetch logs from Kubernetes pods
package podlogs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Writer represents a request to stream a pod's logs and send them to an io.Writer
type Writer struct {
	Pod    corev1.Pod
	Client kubernetes.Interface
}

// NewPodLogsWriter initializes the struct
func NewPodLogsWriter(pod corev1.Pod, cli kubernetes.Interface) *Writer {
	return &Writer{Pod: pod, Client: cli}
}

// Single streams the pod logs and shunts them to the `writer`.
// If there are multiple containers, it will concatenate all the container streams into the writer
func (spl *Writer) Single(ctx context.Context, writer io.Writer, opts *corev1.PodLogOptions) (err error) {
	if opts.Container != "" {
		return spl.sendLogsToWriter(ctx, writer, opts)
	}

	for _, container := range spl.Pod.Spec.Containers {
		containerOpts := opts.DeepCopy()
		containerOpts.Container = container.Name
		if err := spl.sendLogsToWriter(ctx, writer, containerOpts); err != nil {
			return err
		}
	}
	return nil
}

// writerConstructor is the interface representing an object that can spawn writers
type writerConstructor interface {
	Create(name string) (io.Writer, error)
}

func (spl *Writer) sendLogsToWriter(
	ctx context.Context,
	writer io.Writer,
	options *corev1.PodLogOptions,
) error {
	if options.Previous {
		jsWriter := json.NewEncoder(writer)
		if err := jsWriter.Encode("====== Beginning of Previous Log ====="); err != nil {
			return err
		}
		// getting the Previous logs can fail (as with `kubectl logs -p`). Don't error out
		previousOpts := options.DeepCopy()
		previousRequest := spl.Client.CoreV1().Pods(spl.Pod.Namespace).GetLogs(spl.Pod.Name, previousOpts)
		if err := executeGetLogRequest(ctx, previousRequest, writer); err != nil {
			// we try to print the json-safe error message. We don't exit on error
			_ = json.NewEncoder(writer).Encode("Error fetching previous logs: " + err.Error())
		}
		if err := jsWriter.Encode("====== End of Previous Log ====="); err != nil {
			return err
		}
		// Now fetch current logs with Previous set to false
		options.Previous = false
	}

	request := spl.Client.CoreV1().Pods(spl.Pod.Namespace).GetLogs(spl.Pod.Name, options)
	return executeGetLogRequest(ctx, request, writer)
}

// Multiple streams the pod logs, sending each container's stream to a separate writer
func (spl *Writer) Multiple(
	ctx context.Context,
	opts *corev1.PodLogOptions,
	writerConstructor writerConstructor,
	filePathGenerator func(string) string,
) error {
	if opts.Container != "" {
		return fmt.Errorf("use Single method to handle a single container output")
	}

	for _, container := range spl.Pod.Spec.Containers {
		writer, err := writerConstructor.Create(filePathGenerator(container.Name))
		if err != nil {
			return err
		}
		containerOpts := opts.DeepCopy()
		containerOpts.Container = container.Name

		if err := spl.sendLogsToWriter(ctx, writer, containerOpts); err != nil {
			return err
		}
	}
	return nil
}

func executeGetLogRequest(ctx context.Context, logRequest *rest.Request, writer io.Writer) error {
	logStream, err := logRequest.Stream(ctx)
	if err != nil {
		return fmt.Errorf("when opening the log stream: %w", err)
	}
	defer func() {
		innerErr := logStream.Close()
		if err == nil && innerErr != nil {
			err = fmt.Errorf("when closing the log stream: %w", innerErr)
		}
	}()

	_, err = io.Copy(writer, logStream)
	if err != nil {
		return fmt.Errorf("when copying the log stream to the writer: %w", err)
	}
	_, _ = writer.Write([]byte("\n"))
	return nil
}
