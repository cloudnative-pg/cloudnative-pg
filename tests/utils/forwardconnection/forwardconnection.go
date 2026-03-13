/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package forwardconnection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/httpstream"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// PostgresPortMap is the default port map for the PostgreSQL Pod
const PostgresPortMap = "0:5432"

// expectedPortForwardErrors lists error substrings that are expected during
// port-forward teardown and should be suppressed. These originate from
// k8s.io/client-go/tools/portforward calling runtime.HandleError for:
//   - "error closing listener" — when listeners are closed during shutdown
//   - "an error occurred forwarding" — when kubelet reports connection reset
//     on the error stream during normal connection close
//
// This slice must not be modified after init.
var expectedPortForwardErrors = []string{
	"error closing listener",
	"an error occurred forwarding",
}

// isExpectedPortForwardError returns true if the error matches a known
// benign port-forward teardown error that should be suppressed.
func isExpectedPortForwardError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	for _, expected := range expectedPortForwardErrors {
		if strings.Contains(errMsg, expected) {
			return true
		}
	}
	return false
}

// init replaces utilruntime.ErrorHandlers with a single wrapper that
// suppresses known port-forward errors and delegates everything else
// to the handlers that were registered before this init ran.
//
// NOTE: because utilruntime.handleError iterates the ErrorHandlers
// slice directly, any handler appended after this init will be called
// for all errors regardless of the filter. This is fine as long as no
// other code in the test binary modifies ErrorHandlers.
func init() {
	originalHandlers := make([]utilruntime.ErrorHandler, len(utilruntime.ErrorHandlers))
	copy(originalHandlers, utilruntime.ErrorHandlers)

	utilruntime.ErrorHandlers = []utilruntime.ErrorHandler{
		func(ctx context.Context, err error, msg string, keysAndValues ...any) {
			if isExpectedPortForwardError(err) {
				return
			}
			for _, handler := range originalHandlers {
				handler(ctx, err, msg, keysAndValues...)
			}
		},
	}
}

// ForwardConnection holds the necessary information to manage a port-forward
// against a service of pod inside Kubernetes
type ForwardConnection struct {
	forwarder    *portforward.PortForwarder
	stopChannel  chan struct{}
	readyChannel chan struct{}
	closeOnce    sync.Once
	done         chan struct{}
	started      atomic.Bool
}

// NewDialerFromService returns a Dialer against the service specified
func NewDialerFromService(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	config *rest.Config,
	namespace,
	service string,
) (dialer httpstream.Dialer, portMaps []string, err error) {
	pod, portMap, err := getPodAndPortsFromService(ctx, kubeInterface, namespace, service)
	if err != nil {
		return nil, nil, err
	}

	dial, err := NewDialer(kubeInterface, config, namespace, pod)
	if err != nil {
		return nil, nil, err
	}

	return dial, portMap, nil
}

// NewForwardConnection returns a PortForwarder against the pod specified
func NewForwardConnection(
	dialer httpstream.Dialer,
	portMaps []string,
	outWriter,
	errWriter io.Writer,
) (*ForwardConnection, error) {
	fc := &ForwardConnection{
		stopChannel:  make(chan struct{}),
		readyChannel: make(chan struct{}, 1),
		done:         make(chan struct{}),
	}

	var err error
	fc.forwarder, err = portforward.New(
		dialer,
		portMaps,
		fc.stopChannel,
		fc.readyChannel,
		outWriter,
		errWriter,
	)
	if err != nil {
		return nil, err
	}

	return fc, nil
}

// NewDialer returns a Dialer to be used with a PortForwarder
func NewDialer(
	kubeInterface kubernetes.Interface,
	config *rest.Config,
	namespace string,
	pod string,
) (httpstream.Dialer, error) {
	req := kubeInterface.CoreV1().
		RESTClient().
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(pod).
		SubResource("portforward")

	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", req.URL())
	return dialer, nil
}

// StartAndWait begins the port-forwarding and waits until it's ready.
// It must be called at most once per ForwardConnection.
func (fc *ForwardConnection) StartAndWait(ctx context.Context) error {
	fc.started.Store(true)
	errChan := make(chan error, 1)
	go func() {
		defer close(fc.done)
		ginkgo.GinkgoWriter.Println("Starting port-forward")
		if err := fc.forwarder.ForwardPorts(); err != nil {
			ginkgo.GinkgoWriter.Printf("port-forward failed with error %s\n", err.Error())
			errChan <- err
		}
	}()

	select {
	case <-fc.readyChannel:
		ginkgo.GinkgoWriter.Println("port-forward ready")
		return nil
	case err := <-errChan:
		ginkgo.GinkgoWriter.Println("port-forward failed before becoming ready")
		return err
	case <-ctx.Done():
		fc.closeOnce.Do(func() { close(fc.stopChannel) })
		return ctx.Err()
	}
}

// Close stops the port-forward and waits for the forwarding goroutine to exit.
// It is safe to call multiple times and safe to call even if StartAndWait was
// never called.
func (fc *ForwardConnection) Close() {
	fc.closeOnce.Do(func() { close(fc.stopChannel) })
	if fc.started.Load() {
		<-fc.done
	}
}

// GetLocalPort will return the local port where the forward has started
func (fc *ForwardConnection) GetLocalPort() (string, error) {
	ports, err := fc.forwarder.GetPorts()
	if err != nil {
		return "", err
	}
	return strconv.Itoa(int(ports[0].Local)), nil
}

// getPortMap takes the first port between the list of ports exposed by the given service, and
// returns a map with 0 as the local port for auto-assignment
func getPortMap(serviceObj *corev1.Service) ([]string, error) {
	if len(serviceObj.Spec.Ports) == 0 {
		return []string{}, fmt.Errorf("service %s has no ports", serviceObj.Name)
	}
	port := serviceObj.Spec.Ports[0].Port
	return []string{fmt.Sprintf("0:%d", port)}, nil
}

func getPodAndPortsFromService(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	namespace,
	service string,
) (string, []string, error) {
	serviceObj, err := getServiceObject(ctx, kubeInterface, namespace, service)
	if err != nil {
		return "", nil, err
	}

	podObj, err := getPodFromService(ctx, kubeInterface, serviceObj)
	if err != nil {
		return "", nil, err
	}

	portMaps, err := getPortMap(serviceObj)
	if err != nil {
		return "", nil, err
	}

	return podObj.Name, portMaps, nil
}

func getServiceObject(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	namespace,
	service string,
) (*corev1.Service, error) {
	return kubeInterface.CoreV1().Services(namespace).Get(ctx, service, metav1.GetOptions{})
}

func getPodFromService(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	serviceObj *corev1.Service,
) (*corev1.Pod, error) {
	namespace := serviceObj.Namespace

	labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: serviceObj.Spec.Selector,
	})
	if err != nil {
		return nil, err
	}

	podList, err := kubeInterface.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	})
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods found for service %s", serviceObj.Name)
	}

	return &podList.Items[0], nil
}
