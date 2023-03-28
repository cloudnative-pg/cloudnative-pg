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
	"bufio"
	"context"
	"fmt"
	"io"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StreamingRequest represents a request to stream a pod's logs
type StreamingRequest struct {
	Pod      *v1.Pod
	Options  *v1.PodLogOptions
	Previous bool `json:"previous,omitempty"`
	// NOTE: the client argument may be omitted, but it is good practice to pass it
	// Importantly, it makes the logging functions testable
	client kubernetes.Interface
}

func (spl *StreamingRequest) getPodName() string {
	if spl.Pod != nil {
		return spl.Pod.Name
	}
	return ""
}

func (spl *StreamingRequest) getPodNamespace() string {
	if spl.Pod != nil {
		return spl.Pod.Namespace
	}
	return ""
}

func (spl *StreamingRequest) getLogOptions() *v1.PodLogOptions {
	if spl.Options == nil {
		spl.Options = &v1.PodLogOptions{}
	}
	spl.Options.Previous = spl.Previous
	return spl.Options
}

func (spl *StreamingRequest) getKubernetesClient() kubernetes.Interface {
	if spl.client != nil {
		return spl.client
	}
	conf := ctrl.GetConfigOrDie()

	spl.client = kubernetes.NewForConfigOrDie(conf)

	return spl.client
}

// getStreamToPod opens the REST request to the pod
func (spl *StreamingRequest) getStreamToPod() *rest.Request {
	client := spl.getKubernetesClient()
	pods := client.CoreV1().Pods(spl.getPodNamespace())

	return pods.GetLogs(
		spl.getPodName(),
		spl.getLogOptions())
}

// Stream streams the pod logs and shunts them to the `writer`.
func (spl *StreamingRequest) Stream(ctx context.Context, writer io.Writer) (err error) {
	wrapErr := func(err error) error { return fmt.Errorf("in Stream: %w", err) }

	logsRequest := spl.getStreamToPod()
	logStream, err := logsRequest.Stream(ctx)
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
	}
	return err
}

// TailPodLogs streams the pod logs starting from the current time, and keeps
// waiting for any new logs, until the  context is cancelled by the calling process
// If `parseTimestamps` is true, the log line will have the timestamp in
// human-readable prepended. NOTE: this will make log-lines NON-JSON
func TailPodLogs(
	ctx context.Context,
	client kubernetes.Interface,
	pod v1.Pod,
	writer io.Writer,
	parseTimestamps bool,
) (err error) {
	now := metav1.Now()
	streamPodLog := StreamingRequest{
		Pod: &pod,
		Options: &v1.PodLogOptions{
			Timestamps: parseTimestamps,
			Follow:     true,
			SinceTime:  &now,
		},
		client: client,
	}
	return streamPodLog.Stream(ctx, writer)
}

// GetPodLogs streams the pod logs and shunts them to the `writer`, as well as
// returning the last `requestedLineLength` of lines of logs in a slice.
// If `getPrevious` was activated, it will get the previous logs
//
// TODO: this function is a bit hacky. The K8s PodLogOptions have a field
// called `TailLines` that seems to be just what we would like.
// HOWEVER: we want the full logs too, so we can write them to a file, in addition to
// the `TailLines` we want to pass along for display
func GetPodLogs(
	ctx context.Context,
	client kubernetes.Interface,
	pod v1.Pod,
	getPrevious bool,
	writer io.Writer,
	requestedLineLength int,
) (
	[]string, error,
) {
	wrapErr := func(err error) error { return fmt.Errorf("in GetPodLogs: %w", err) }

	streamPodLog := StreamingRequest{
		Pod:      &pod,
		Previous: getPrevious,
		Options:  &v1.PodLogOptions{},
		client:   client,
	}
	logsRequest := streamPodLog.getStreamToPod()

	logStream, err := logsRequest.Stream(ctx)
	if err != nil {
		return nil, wrapErr(err)
	}
	defer func() {
		innerErr := logStream.Close()
		if err == nil && innerErr != nil {
			err = innerErr
		}
	}()

	rd := bufio.NewReader(logStream)
	teedReader := io.TeeReader(rd, writer)
	scanner := bufio.NewScanner(teedReader)

	if requestedLineLength <= 0 {
		requestedLineLength = 10
	}

	// slice to hold the last `requestedLineLength` lines of log
	lines := make([]string, requestedLineLength)
	// index of the current line of the log (starting from zero)
	i := 0
	// index in the slice that holds the current line of log
	curIdx := 0

	for scanner.Scan() {
		lines[curIdx] = scanner.Text()
		i++
		// `curIdx` walks from `0` to `requestedLineLength-1` and then to `0` in a cycle
		curIdx = i % requestedLineLength
	}

	if err := scanner.Err(); err != nil {
		return nil, wrapErr(err)
	}
	// if `curIdx` walks to in the middle of 0 and `requestedLineLength-1`, assemble the last `requestedLineLength`
	// lines of logs
	if i > requestedLineLength && curIdx < (requestedLineLength-1) {
		return append(lines[curIdx+1:], lines[:curIdx+1]...), nil
	}

	return lines, nil
}
