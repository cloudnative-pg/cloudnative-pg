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
	"os"
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
//
// If the Follow Option is set to true, streaming will sit in a loop looking
// for any new / regenerated pods, and will only exit when there are no pods
// streaming
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

// activeSet is a goroutine-safe store of active processes. It is similar
// in idea to a WaitGroup, but does not block when we check for zero, and it
// also keeps a name for each active process to avoid duplication
type activeSet struct {
	m   sync.Mutex
	wg  sync.WaitGroup
	set map[string]bool
}

func newActiveSet() *activeSet {
	return &activeSet{
		set: make(map[string]bool),
	}
}

// add name as an active process
func (ww *activeSet) add(name string) {
	ww.wg.Add(1)
	ww.m.Lock()
	defer ww.m.Unlock()
	ww.set[name] = true
}

// has returns true if and only if name is active
func (ww *activeSet) has(name string) bool {
	_, found := ww.set[name]
	return found
}

// drop takes a name out of the active set
func (ww *activeSet) drop(name string) {
	ww.wg.Done()
	ww.m.Lock()
	defer ww.m.Unlock()
	delete(ww.set, name)
}

// isZero checks if there are any active processes
func (ww *activeSet) isZero() bool {
	return len(ww.set) == 0
}

// wait blocks until there are no active processes
func (ww *activeSet) wait() {
	ww.wg.Wait()
}

// SingleStream streams the cluster's pod logs and shunts them to a single io.Writer
func (csr *ClusterStreamingRequest) SingleStream(ctx context.Context, writer io.Writer) error {
	contextLogger := log.FromContext(ctx)
	client := csr.getKubernetesClient()
	streamSet := newActiveSet()
	var errChan chan error // so the goroutines streaming can communicate errors
	defer func() {
		// try to cancel the streaming goroutines
		ctx.Done()
	}()
	isFirstScan := true

	for {
		var (
			podList *v1.PodList
			err     error
		)
		if isFirstScan || csr.Options.Follow {
			podList, err = client.CoreV1().Pods(csr.getClusterNamespace()).List(ctx, metav1.ListOptions{})
			if err != nil {
				return err
			}
			isFirstScan = false
		} else {
			streamSet.wait()
			return nil
		}
		if len(podList.Items) == 0 && streamSet.isZero() {
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
			if pod.Labels[utils.ClusterLabelName] != csr.getClusterName() {
				continue
			}
			if streamSet.has(pod.Name) {
				continue
			}
			streamSet.add(pod.Name)
			go csr.streamInGoroutine(ctx, pod.Name, client, streamSet,
				safeWriterFrom(writer), safeWriterFrom(os.Stderr))
		}
		if streamSet.isZero() {
			return nil
		}
		// sleep a bit to avoid busy waiting cycle
		time.Sleep(2 * time.Second)
	}
}

// streamInGoroutine streams a pod's logs to a writer. It is designed
// to be called as a goroutine, so it uses an error channel to convey errors
// to the calling routine
//
// IMPORTANT: the writer should be goroutine-safe
func (csr *ClusterStreamingRequest) streamInGoroutine(
	ctx context.Context,
	podName string,
	client kubernetes.Interface,
	streamSet *activeSet,
	w io.Writer,
	safeStderr io.Writer,
) {
	defer func() {
		streamSet.drop(podName)
	}()

	pods := client.CoreV1().Pods(csr.getClusterNamespace())
	logsRequest := pods.GetLogs(
		podName,
		csr.getLogOptions())

	logStream, err := logsRequest.Stream(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(safeStderr, "error on streaming request, pod %s: %v", podName, err)
		return
	}
	defer func() {
		err := logStream.Close()
		if err != nil {
			_, _ = fmt.Fprintf(safeStderr, "error closing streaming request, pod %s: %v", podName, err)
		}
	}()

	_, err = io.Copy(w, logStream)
	if err != nil {
		_, _ = fmt.Fprintf(safeStderr, "error sending logs to writer, pod %s: %v", podName, err)
		return
	}
}

// TailClusterLogs streams the cluster pod logs to a single output io.Writer,
// starting from the current time, and watching for any new pods, and any new logs,
// until the  context is cancelled or there are no pods left.
//
// If `parseTimestamps` is true, the log line will have the timestamp in
// human-readable prepended. NOTE: this will make log-lines NON-JSON
func TailClusterLogs(
	ctx context.Context,
	client kubernetes.Interface,
	cluster apiv1.Cluster,
	writer io.Writer,
	parseTimestamps bool,
) error {
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
	return streamClusterLogs.SingleStream(ctx, writer)
}
