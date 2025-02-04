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
	"io"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StreamingRequest represents a request to stream a pod's logs
type StreamingRequest struct {
	Pod      v1.Pod
	Options  *v1.PodLogOptions
	Previous bool `json:"previous,omitempty"`
	// NOTE: the Client argument may be omitted, but it is good practice to pass it
	// Importantly, it makes the logging functions testable
	Client kubernetes.Interface
}

func (spl *StreamingRequest) getPodName() string {
	return spl.Pod.Name
}

func (spl *StreamingRequest) getPodNamespace() string {
	return spl.Pod.Namespace
}

func (spl *StreamingRequest) getLogOptions() *v1.PodLogOptions {
	if spl.Options == nil {
		spl.Options = &v1.PodLogOptions{}
	}
	spl.Options.Previous = spl.Previous
	return spl.Options
}

func (spl *StreamingRequest) getKubernetesClient() kubernetes.Interface {
	if spl.Client != nil {
		return spl.Client
	}
	conf := ctrl.GetConfigOrDie()

	spl.Client = kubernetes.NewForConfigOrDie(conf)

	return spl.Client
}

// getPodStreams opens REST request to the pod containers
// If the pod has only one container, it is used by default. Otherwise
// it returns a list of streams to each of the running containers
func (spl *StreamingRequest) getPodStream() *rest.Request {
	client := spl.getKubernetesClient()
	pods := client.CoreV1().Pods(spl.getPodNamespace())

	return pods.GetLogs(
		spl.getPodName(),
		spl.getLogOptions())
}

func (spl *StreamingRequest) getQualifiedPodStream(container string) *rest.Request {
	client := spl.getKubernetesClient()
	pods := client.CoreV1().Pods(spl.getPodNamespace())
	options := spl.getLogOptions()
	options.Container = container

	return pods.GetLogs(
		spl.getPodName(),
		options)
}

// Stream streams the pod logs and shunts them to the `writer`.
func (spl *StreamingRequest) Stream(ctx context.Context, writer io.Writer) (err error) {
	options := spl.getLogOptions()
	if options.Container != "" {
		return writeLogs(ctx, spl.getPodStream(), writer)
	}

	for _, container := range spl.Pod.Spec.Containers {
		if err := writeLogs(ctx, spl.getQualifiedPodStream(container.Name), writer); err != nil {
			return err
		}
	}
	return nil
}

// writerCreator is the interface representing an object that can spawn writers
type writerCreator interface {
	Create(name string) (io.Writer, error)
}

// StreamMultiple streams the pod logs and shunts each container into a separate writer
func (spl *StreamingRequest) StreamMultiple(
	ctx context.Context,
	writerGen writerCreator,
	namer func(string) string,
) (err error) {
	options := spl.getLogOptions()
	if options.Container != "" {
		writer, err := writerGen.Create(namer(options.Container))
		if err != nil {
			return err
		}
		if err := writeLogs(ctx, spl.getPodStream(), writer); err != nil {
			return err
		}
	}

	for _, container := range spl.Pod.Spec.Containers {
		writer, err := writerGen.Create(namer(container.Name))
		if err != nil {
			return err
		}
		if err := writeLogs(ctx, spl.getQualifiedPodStream(container.Name), writer); err != nil {
			return err
		}
	}
	return nil
}

func writeLogs(ctx context.Context, podStream *rest.Request, writer io.Writer) error {
	logStream, err := podStream.Stream(ctx)
	if err != nil {
		return err
	}
	defer func() {
		innerErr := logStream.Close()
		if err == nil && innerErr != nil {
			err = innerErr
		}
	}()

	_, err = io.Copy(writer, logStream)
	if err != nil {
		return err
	}
	_, _ = writer.Write([]byte("\n"))
	return nil
}
