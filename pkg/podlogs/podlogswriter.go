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

// Package logs contains code to fetch logs from Kubernetes pods
package podlogs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// PodLogsWriter represents a request to stream a pod's logs
type PodLogsWriter struct {
	Pod    v1.Pod
	Client kubernetes.Interface
}

// NewPodLogsWriter initializes the struct
func NewPodLogsWriter(pod v1.Pod, cli kubernetes.Interface) *PodLogsWriter {
	return &PodLogsWriter{Pod: pod, Client: cli}
}

// Single streams the pod logs and shunts them to the `writer`.
// If there are multiple containers, it will concatenate all the container streams into the writer
func (spl *PodLogsWriter) Single(ctx context.Context, writer io.Writer, opts *v1.PodLogOptions) (err error) {
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

func (spl *PodLogsWriter) sendLogsToWriter(
	ctx context.Context,
	writer io.Writer,
	options *v1.PodLogOptions,
) error {
	request := spl.Client.CoreV1().Pods(spl.Pod.Namespace).GetLogs(spl.Pod.Name, options)

	if options.Previous {
		jsWriter := json.NewEncoder(writer)
		if err := jsWriter.Encode("====== Beginning of Previous Log ====="); err != nil {
			return err
		}
		// getting the Previous logs can fail (as with `kubectl logs -p`). Don't error out
		if err := executeGetLogRequest(ctx, request, writer); err != nil {
			// we try to print the json-safe error message. We don't exit on error
			_ = json.NewEncoder(writer).Encode("Error fetching previous logs: " + err.Error())
		}
		if err := jsWriter.Encode("====== End of Previous Log ====="); err != nil {
			return err
		}
	}
	return executeGetLogRequest(ctx, request, writer)
}

// Multiple streams the pod logs, sending each container's stream to a separate writer
func (spl *PodLogsWriter) Multiple(
	ctx context.Context,
	opts *v1.PodLogOptions,
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

		if err := spl.sendLogsToWriter(ctx, writer, opts); err != nil {
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
