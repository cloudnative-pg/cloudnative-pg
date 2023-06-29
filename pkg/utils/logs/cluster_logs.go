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

package logs

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// ClusterStreamingRequest represents a request to stream a cluster's pod logs
type ClusterStreamingRequest struct {
	Cluster  apiv1.Cluster
	Options  *v1.PodLogOptions
	Previous bool `json:"previous,omitempty"`
	// NOTE: the client argument may be omitted, but it is good practice to pass it
	// Importantly, it makes the logging functions testable
	client kubernetes.Interface
}

func (csr *ClusterStreamingRequest) getClusterName() string {
	return csr.Cluster.Name
}

func (csr *ClusterStreamingRequest) getClusterNamespace() string {
	return csr.Cluster.Namespace
}

func (csr *ClusterStreamingRequest) getLogOptions() *v1.PodLogOptions {
	if csr.Options == nil {
		csr.Options = &v1.PodLogOptions{}
	}
	csr.Options.Previous = csr.Previous
	return csr.Options
}

func (csr *ClusterStreamingRequest) getKubernetesClient() kubernetes.Interface {
	if csr.client != nil {
		return csr.client
	}
	conf := ctrl.GetConfigOrDie()

	csr.client = kubernetes.NewForConfigOrDie(conf)

	return csr.client
}

// safeWriter is an io.Writer that is safe for concurrent use. It guarantees
// that only one goroutine gets to write to the underlying writer at any given
// time
type safeWriter struct {
	m      sync.Mutex
	Writer io.Writer
}

func (w *safeWriter) Write(b []byte) (n int, err error) {
	w.m.Lock()
	defer w.m.Unlock()
	return w.Writer.Write(b)
}

func safeWriterFrom(w io.Writer) *safeWriter {
	return &safeWriter{
		Writer: w,
	}
}

// activeStreams is goroutine-safe counter of active streams. It is similar
// in idea to a WaitGroup, but does not block when we check for zero
type activeStreams struct {
	m     sync.Mutex
	count int
}

func (ww *activeStreams) Increment() {
	ww.m.Lock()
	defer ww.m.Unlock()
	ww.count++
}

func (ww *activeStreams) Decrement() {
	ww.m.Lock()
	defer ww.m.Unlock()
	ww.count--
}

func (ww *activeStreams) IsZero() bool {
	return ww.count == 0
}

// Stream streams the cluster's pod logs and shunts them to the `writer`.
func (csr *ClusterStreamingRequest) Stream(ctx context.Context, writer io.Writer) (err error) {
	contextLogger := log.FromContext(ctx)
	client := csr.getKubernetesClient()
	podBeingLogged := make(map[string]bool)
	var streamSet activeStreams
	var errChan chan error // so the goroutines streaming can communicate errors
	defer func() {
		fmt.Fprintf(writer, "\nClosing cluster log streaming. %d pods streming\n", streamSet.count)
	}()

	for {
		podList, err := client.CoreV1().Pods(csr.getClusterNamespace()).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(podList.Items) == 0 && streamSet.IsZero() {
			contextLogger.Warning("no pods to log in namespace", "namespace", csr.getClusterNamespace())
			return nil
		}

		select {
		case routineErr := <-errChan:
			contextLogger.Error(routineErr, "while streaming cluster pod logs",
				"cluster", csr.getClusterName(),
				"namespace", csr.getClusterNamespace())
		default:
		}

		for _, pod := range podList.Items {
			if pod.Labels[utils.ClusterLabelName] != csr.getClusterName() ||
				pod.Labels[utils.PodRoleLabelName] != "instance" {
				continue
			}
			if podBeingLogged[pod.Name] {
				continue
			}
			podBeingLogged[pod.Name] = true
			streamSet.Increment()
			go func(ctx context.Context, podName string, w io.Writer, errch chan error) {
				defer func() {
					streamSet.Decrement()
				}()
				pods := client.CoreV1().Pods(csr.getClusterNamespace())
				logsRequest := pods.GetLogs(
					podName,
					csr.getLogOptions())
				logStream, err := logsRequest.Stream(ctx)
				if err != nil {
					errch <- err
					return
				}
				defer func() {
					innerErr := logStream.Close()
					if innerErr != nil {
						errch <- innerErr
					}
				}()

				_, err = io.Copy(w, logStream)
				if err != nil {
					errch <- err
					return
				}
			}(ctx, pod.Name, safeWriterFrom(writer), errChan)
		}
		if streamSet.IsZero() {
			writer.Write([]byte("\n<- EOF ->\n"))
			return nil
		}
		// sleep a bit so we're not in a busy waiting cycle
		time.Sleep(5 * time.Second)
	}
}

// TailClusterLogs streams the cluster pod logs starting from the current time, and keeps
// waiting for any new pods, and any new logs, until the  context is cancelled
// by the calling process
// If `parseTimestamps` is true, the log line will have the timestamp in
// human-readable prepended. NOTE: this will make log-lines NON-JSON
func TailClusterLogs(
	ctx context.Context,
	client kubernetes.Interface,
	cluster apiv1.Cluster,
	writer io.Writer,
	parseTimestamps bool,
) (err error) {
	now := metav1.Now()
	streamClusterLogs := ClusterStreamingRequest{
		Cluster: cluster,
		Options: &v1.PodLogOptions{
			Timestamps: parseTimestamps,
			Follow:     true,
			SinceTime:  &now,
		},
		client: client,
	}
	return streamClusterLogs.Stream(ctx, writer)
}
