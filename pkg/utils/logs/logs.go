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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StreamPodLogs streams the pod logs and shunts them to the `writer`. If `getPrevious`
// was activated, it will get the previous logs
func StreamPodLogs(ctx context.Context, pod corev1.Pod, getPrevious bool, writer io.Writer) (err error) {
	wrapErr := func(err error) error { return fmt.Errorf("in StreamPodLogs: %w", err) }
	conf := ctrl.GetConfigOrDie()
	pods := kubernetes.NewForConfigOrDie(conf).CoreV1().Pods(pod.Namespace)
	logsRequest := pods.GetLogs(pod.Name, &corev1.PodLogOptions{
		Previous: getPrevious,
	})
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

// GetPodLogs streams the pod logs and shunts them to the `writer`, as well as
// returning the last `requestedLineLength` of lines of logs in a slice.
// If `getPrevious` was activated, it will get the previous logs
func GetPodLogs(ctx context.Context, pod corev1.Pod, getPrevious bool, writer io.Writer, requestedLineLength int) (
	[]string, error,
) {
	wrapErr := func(err error) error { return fmt.Errorf("in GetPodLogs: %w", err) }
	conf := ctrl.GetConfigOrDie()
	pods := kubernetes.NewForConfigOrDie(conf).CoreV1().Pods(pod.Namespace)
	logsRequest := pods.GetLogs(pod.Name, &corev1.PodLogOptions{
		Previous: getPrevious,
	})
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
