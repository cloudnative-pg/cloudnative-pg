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
	"bufio"
	"context"
	"io"
	"log"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// DefaultFollowWaiting is the default time the cluster streaming should
// wait before searching again for new cluster pods
const DefaultFollowWaiting time.Duration = 1 * time.Second

// ClusterStreamingRequest represents a request to stream a cluster's pod logs
//
// If the Follow Option is set to true, streaming will sit in a loop looking
// for any new / regenerated pods, and will only exit when there are no pods
// streaming
type ClusterStreamingRequest struct {
	Cluster       *apiv1.Cluster
	Options       *v1.PodLogOptions
	Previous      bool `json:"previous,omitempty"`
	FollowWaiting time.Duration
	// NOTE: the Client argument may be omitted, but it is good practice to pass it
	// Importantly, it makes the logging functions testable
	Client kubernetes.Interface
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
	if csr.Client != nil {
		return csr.Client
	}
	conf := ctrl.GetConfigOrDie()

	csr.Client = kubernetes.NewForConfigOrDie(conf)

	return csr.Client
}

func (csr *ClusterStreamingRequest) getFollowWaitingTime() time.Duration {
	if csr.FollowWaiting > 0 {
		return csr.FollowWaiting
	}
	return DefaultFollowWaiting
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
func (as *activeSet) add(name string) {
	as.wg.Add(1)
	as.m.Lock()
	defer as.m.Unlock()
	as.set[name] = true
}

// has returns true if and only if name is active
func (as *activeSet) has(name string) bool {
	_, found := as.set[name]
	return found
}

// drop takes a name out of the active set
func (as *activeSet) drop(name string) {
	as.wg.Done()
	as.m.Lock()
	defer as.m.Unlock()
	delete(as.set, name)
}

// isZero checks if there are any active processes
func (as *activeSet) isZero() bool {
	return len(as.set) == 0
}

// wait blocks until there are no active processes
func (as *activeSet) wait() {
	as.wg.Wait()
}

// SingleStream streams the cluster's pod logs and shunts them to a single io.Writer
func (csr *ClusterStreamingRequest) SingleStream(ctx context.Context, writer io.Writer) error {
	client := csr.getKubernetesClient()
	streamSet := newActiveSet()
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
			podList, err = client.CoreV1().Pods(csr.getClusterNamespace()).List(ctx, metav1.ListOptions{
				LabelSelector: utils.ClusterLabelName + "=" + csr.getClusterName(),
			})
			if err != nil {
				return err
			}
			isFirstScan = false
		} else {
			streamSet.wait()
			return nil
		}
		if len(podList.Items) == 0 && streamSet.isZero() {
			log.Printf("no pods to log in namespace %s", csr.getClusterNamespace())
			return nil
		}

		for _, pod := range podList.Items {
			if streamSet.has(pod.Name) {
				continue
			}

			streamSet.add(pod.Name)
			go csr.streamInGoroutine(ctx, pod.Name, client, streamSet,
				safeWriterFrom(writer))
		}
		if streamSet.isZero() {
			return nil
		}
		// wait before looking for new pods to log
		time.Sleep(csr.getFollowWaitingTime())
	}
}

// streamInGoroutine streams a pod's logs to a writer. It is designed
// to be called as a goroutine.
//
// IMPORTANT: the output writer should be goroutine-safe
// NOTE: the default Go `log` package is used for logging because it's goroutine-safe
func (csr *ClusterStreamingRequest) streamInGoroutine(
	ctx context.Context,
	podName string,
	client kubernetes.Interface,
	streamSet *activeSet,
	output io.Writer,
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
		log.Printf("error on streaming request, pod %s: %v", podName, err)
		return
	} else if apierrs.IsBadRequest(err) {
		return
	}
	defer func() {
		err := logStream.Close()
		if err != nil {
			log.Printf("error closing streaming request, pod %s: %v", podName, err)
		}
	}()

	scanner := bufio.NewScanner(logStream)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	bufferedOutput := bufio.NewWriter(output)

readLoop:
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			break readLoop
		default:
			data := scanner.Text()
			if _, err := bufferedOutput.Write([]byte(data)); err != nil {
				log.Printf("error writing log line to output: %v", err)
			}
			if err := bufferedOutput.WriteByte('\n'); err != nil {
				log.Printf("error writing newline to output: %v", err)
			}
			if err := bufferedOutput.Flush(); err != nil {
				log.Printf("error flushing output: %v", err)
			}
		}
	}
}
