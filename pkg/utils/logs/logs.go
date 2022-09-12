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
// returning the log lines in a slice.
// If `getPrevious` was activated, it will get the previous logs
func GetPodLogs(ctx context.Context, pod corev1.Pod, getPrevious bool, writer io.Writer) (lines []string, err error) {
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

	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, wrapErr(err)
	}

	return lines, nil
}
