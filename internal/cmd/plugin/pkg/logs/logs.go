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
)

// StreamingRequest represents a request to stream a pod's logs
type StreamingRequest struct {
	Pod    v1.Pod
	Client kubernetes.Interface
}

// NewStreamingRequest initializes the struct
func NewStreamingRequest(pod v1.Pod, cli kubernetes.Interface) *StreamingRequest {
	return &StreamingRequest{Pod: pod, Client: cli}
}

// getContainerLogsRequestWithOptions returns the stream to the pod with overridden container and `previous` values
func (spl *StreamingRequest) getContainerLogsRequestWithOptions(options *v1.PodLogOptions) *rest.Request {
	pods := spl.Client.CoreV1().Pods(spl.Pod.Namespace)
	return pods.GetLogs(spl.Pod.Name, options)
}

// SingleStream streams the pod logs and shunts them to the `writer`.
// If there are multiple containers, it will concatenate all the container streams into the writer
func (spl *StreamingRequest) SingleStream(ctx context.Context, writer io.Writer, opts *v1.PodLogOptions) (err error) {
	if opts.Container != "" {
		return sendLogsToWriter(ctx, spl.getContainerLogsRequestWithOptions(opts), writer)
	}

	for _, container := range spl.Pod.Spec.Containers {
		opts := opts.DeepCopy()
		opts.Container = container.Name
		if err := sendLogsToWriter(ctx, spl.getContainerLogsRequestWithOptions(opts), writer); err != nil {
			return err
		}
	}
	return nil
}

// writerConstructor is the interface representing an object that can spawn writers
type writerConstructor interface {
	Create(name string) (io.Writer, error)
}

func (spl *StreamingRequest) sendPreviousContainerLogsToWriter(
	ctx context.Context,
	writer io.Writer,
	options *v1.PodLogOptions,
) error {
	if !options.Previous {
		return fmt.Errorf("invoked previous log writer but previous option is false")
	}

	jsWriter := json.NewEncoder(writer)
	if err := jsWriter.Encode("====== Beginning of Previous Log ====="); err != nil {
		return err
	}
	// getting the Previous logs can fail (as with `kubectl logs -p`). Don't error out
	if err := sendLogsToWriter(ctx, spl.getContainerLogsRequestWithOptions(options), writer); err != nil {
		// we try to print the json-safe error message. We don't exit on error
		_ = json.NewEncoder(writer).Encode("Error fetching previous logs: " + err.Error())
	}

	return jsWriter.Encode("====== End of Previous Log =====")
}

// MultipleStreams streams the pod logs, sending each container's stream to a separate writer
func (spl *StreamingRequest) MultipleStreams(
	ctx context.Context,
	opts *v1.PodLogOptions,
	writerConstructor writerConstructor,
	filePathGenerator func(string) string,
) error {
	if opts.Container != "" {
		logFilePath := filePathGenerator(opts.Container)
		writer, err := writerConstructor.Create(logFilePath)
		if err != nil {
			return err
		}
		if opts.Previous {
			return spl.sendPreviousContainerLogsToWriter(ctx, writer, opts)
		}
		return sendLogsToWriter(ctx, spl.getContainerLogsRequestWithOptions(opts), writer)
	}

	for _, container := range spl.Pod.Spec.Containers {
		logFilePath := filePathGenerator(container.Name)
		writer, err := writerConstructor.Create(logFilePath)
		if err != nil {
			return err
		}
		optsNew := opts.DeepCopy()
		optsNew.Container = container.Name

		if opts.Previous {
			if err := spl.sendPreviousContainerLogsToWriter(ctx, writer, opts); err != nil {
				return err
			}
			continue
		}

		if err := sendLogsToWriter(ctx, spl.getContainerLogsRequestWithOptions(opts), writer); err != nil {
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
