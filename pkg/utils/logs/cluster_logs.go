package logs

import (
	"context"
	"io"
	"sync"

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

func (csr *ClusterStreamingRequest) getPodNamespace() string {
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

// Stream streams the cluster's pod logs and shunts them to the `writer`.
func (csr *ClusterStreamingRequest) Stream(ctx context.Context, writer io.Writer) (err error) {
	contextLogger := log.FromContext(ctx)
	client := csr.getKubernetesClient()
	podList, err := client.CoreV1().Pods(csr.getPodNamespace()).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(podList.Items) == 0 {
		contextLogger.Warning("no pods found in namespace", "namespace", csr.getPodNamespace())
		return nil
	}

	podBeingLogged := make(map[string]bool)
	var wg sync.WaitGroup
	for {
		for _, pod := range podList.Items {
			if pod.Labels[utils.ClusterLabelName] != csr.getClusterName() {
				continue
			}
			if podBeingLogged[pod.Name] {
				continue
			}
			podBeingLogged[pod.Name] = true
			wg.Add(1)
			go func(ctx context.Context, podName string, w io.Writer) {
				defer wg.Done()
				pods := client.CoreV1().Pods(csr.getPodNamespace())
				logsRequest := pods.GetLogs(
					podName,
					csr.getLogOptions())
				logStream, err := logsRequest.Stream(ctx)
				if err != nil {
					return
				}
				defer func() {
					innerErr := logStream.Close()
					if err == nil && innerErr != nil {
						err = innerErr
					}
				}()

				_, err = io.Copy(w, logStream)
				if err != nil {
					return
				}
			}(ctx, pod.Name, safeWriterFrom(writer))
		}
		wg.Wait()
	}
}
