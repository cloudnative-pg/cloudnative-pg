// Package logs contains code to fetch logs from Kubernetes pods
package logs

import (
	"context"
	"fmt"
	"io"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StreamPodLogs gets the pod logs and shunts them to the `writer`. If `getPrevious`
// was activated, it will get the previous logs
//
// TODO: we should use this function in the report plugin
func StreamPodLogs(ctx context.Context, pod corev1.Pod, getPrevious bool, writer io.Writer) (err error) {
	conf := ctrl.GetConfigOrDie()
	pods := kubernetes.NewForConfigOrDie(conf).CoreV1().Pods(pod.Namespace)
	logsRequest := pods.GetLogs(pod.Name, &corev1.PodLogOptions{
		Previous: getPrevious,
	})
	logStream, err := logsRequest.Stream(ctx)
	if err != nil {
		return fmt.Errorf("could not stream the logs: %w", err)
	}
	defer func() {
		innerErr := logStream.Close()
		if err == nil && innerErr != nil {
			err = innerErr
		}
	}()

	_, err = io.Copy(writer, logStream)
	if err != nil {
		err = fmt.Errorf("could not send logs to writer: %w", err)
	}
	return err
}
