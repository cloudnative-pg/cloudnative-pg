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
package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StreamingRequest represents a request to stream a pod's logs
type StreamingRequest struct {
	Pod            v1.Pod
	DefaultOptions v1.PodLogOptions
	Client         kubernetes.Interface
}

func (spl *StreamingRequest) getPodName() string {
	return spl.Pod.Name
}

func (spl *StreamingRequest) getPodNamespace() string {
	return spl.Pod.Namespace
}

func (spl *StreamingRequest) getKubernetesClient() kubernetes.Interface {
	if spl.Client != nil {
		return spl.Client
	}
	conf := ctrl.GetConfigOrDie()

	spl.Client = kubernetes.NewForConfigOrDie(conf)

	return spl.Client
}

// getContainerLogsRequestWithDefaultOptions opens a REST request to the pod
func (spl *StreamingRequest) getContainerLogsRequestWithDefaultOptions() *rest.Request {
	return spl.getContainerLogsRequestWithOptions(&spl.DefaultOptions)
}

// getContainerLogsRequestWithOptions returns the stream to the pod with overridden container and `previous` values
func (spl *StreamingRequest) getContainerLogsRequestWithOptions(options *v1.PodLogOptions) *rest.Request {
	client := spl.getKubernetesClient()
	pods := client.CoreV1().Pods(spl.getPodNamespace())
	return pods.GetLogs(spl.getPodName(), options)
}

// Stream streams the pod logs and shunts them to the `writer`.
// If there are multiple containers, it will concatenate all the container streams into the writer
func (spl *StreamingRequest) Stream(ctx context.Context, writer io.Writer) (err error) {
	if spl.DefaultOptions.Container != "" {
		return sendLogsToWriter(ctx, spl.getContainerLogsRequestWithDefaultOptions(), writer)
	}

	for _, container := range spl.Pod.Spec.Containers {
		opts := spl.DefaultOptions.DeepCopy()
		opts.Container = container.Name
		request := spl.getContainerLogsRequestWithOptions(opts)
		if err := sendLogsToWriter(ctx, request, writer); err != nil {
			return err
		}
	}
	return nil
}

// writerConstructor is the interface representing an object that can spawn writers
type writerConstructor interface {
	Create(name string) (io.Writer, error)
}

// StreamMultiple streams the pod logs, sending each container's stream to a separate writer
func (spl *StreamingRequest) StreamMultiple(
	ctx context.Context,
	writerConstructor writerConstructor,
	filePathGenerator func(string) string,
) error {
	logContainer := func(containerName string) error {
		logFilePath := filePathGenerator(containerName)
		writer, createErr := writerConstructor.Create(logFilePath)
		if createErr != nil {
			return createErr
		}
		if spl.DefaultOptions.Previous {
			jsWriter := json.NewEncoder(writer)
			if err := jsWriter.Encode("====== Beginning of Previous Log ====="); err != nil {
				return err
			}
			opts := spl.DefaultOptions.DeepCopy()
			opts.Container = containerName
			opts.Previous = true
			// getting the Previous logs can fail (as with `kubectl logs -p`). Don't error out
			if err := sendLogsToWriter(ctx, spl.getContainerLogsRequestWithOptions(opts), writer); err != nil {
				// we try to print the json-safe error message. We don't exit on error
				_ = json.NewEncoder(writer).Encode("Error fetching previous logs: " + err.Error())
			}
			if err := jsWriter.Encode("====== End of Previous Log ====="); err != nil {
				return err
			}
		}
		// get the current logs
		opts := spl.DefaultOptions.DeepCopy()
		opts.Container = containerName
		opts.Previous = false
		return sendLogsToWriter(ctx, spl.getContainerLogsRequestWithOptions(opts), writer)
	}

	if spl.DefaultOptions.Container != "" {
		return logContainer(spl.DefaultOptions.Container)
	}
	for _, container := range spl.Pod.Spec.Containers {
		if err := logContainer(container.Name); err != nil {
			return err
		}
	}
	return nil
}

func sendLogsToWriter(ctx context.Context, podStream *rest.Request, writer io.Writer) error {
	logStream, err := podStream.Stream(ctx)
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
