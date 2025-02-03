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
	"fmt"
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
func (spl *StreamingRequest) getPodStreams() []*rest.Request {
	client := spl.getKubernetesClient()
	pods := client.CoreV1().Pods(spl.getPodNamespace())

	options := spl.getLogOptions()
	if options.Container != "" {
		return []*rest.Request{
			pods.GetLogs(
				spl.getPodName(),
				options),
		}
	}

	if len(spl.Pod.Spec.Containers) == 1 {
		enrichedOptions := *options
		enrichedOptions.Container = spl.Pod.Spec.Containers[0].Name
		return []*rest.Request{
			pods.GetLogs(
				spl.getPodName(),
				&enrichedOptions),
		}
	}

	// we get all the containers
	var streams []*rest.Request
	for _, container := range spl.Pod.Status.ContainerStatuses {
		enrichedOptions := *options
		enrichedOptions.Container = container.Name
		if container.State.Running != nil {
			streams = append(streams,
				pods.GetLogs(
					spl.getPodName(),
					&enrichedOptions))
		}
	}

	return streams
}

// Stream streams the pod logs and shunts them to the `writer`.
func (spl *StreamingRequest) Stream(ctx context.Context, writer io.Writer) (err error) {
	wrapErr := func(err error) error { return fmt.Errorf("in Stream: %w", err) }

	for _, podStream := range spl.getPodStreams() {
		logStream, err := podStream.Stream(ctx)
		if err != nil {
			return wrapErr(err)
		}
		defer func() {
			innerErr := logStream.Close()
			if err == nil && innerErr != nil {
				err = innerErr
			}
		}()

		_, err = io.Copy(writer, logStream)
		if err != nil {
			err = wrapErr(err)
			return err
		}
		_, _ = writer.Write([]byte("\n"))
	}
	return nil
}
