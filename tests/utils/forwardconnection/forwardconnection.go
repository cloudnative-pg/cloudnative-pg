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

package forwardconnection

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// ForwardConnection holds the necessary information to manage a port-forward
// against a service of pod inside Kubernetes
type ForwardConnection struct {
	Forwarder    *portforward.PortForwarder
	stopChannel  chan struct{}
	readyChannel chan struct{}
}

// NewServiceForward returns a PortForwarder against the service specified
func NewServiceForward(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	config *rest.Config,
	namespace, service string,
	outWriter, errWriter io.Writer,
) (*ForwardConnection, error) {
	return newForward(ctx, kubeInterface, config, namespace, service, "", "", outWriter, errWriter)
}

// NewPodForward returns a PortForwarder against the pod specified
func NewPodForward(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	config *rest.Config,
	namespace, pod, podPort string,
	outWriter, errWriter io.Writer,
) (*ForwardConnection, error) {
	return newForward(ctx, kubeInterface, config, namespace, "", pod, podPort, outWriter, errWriter)
}

// newForward returns a PortForwarder against the service or pod specified
// in the giving namespace, service takes precedence over pod
func newForward(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	config *rest.Config,
	namespace, service, pod, podPort string,
	outWriter, errWriter io.Writer,
) (*ForwardConnection, error) {
	var err error
	var portMaps []string

	if service != "" {
		pod, portMaps, err = getPodAndPortsFromService(ctx, kubeInterface, namespace, service)
		if err != nil {
			return nil, err
		}
	}

	// Since pod is not empty we assume that we're passing the podPort, if that's not
	// the case we should error out
	if pod != "" {
		if podPort == "" {
			return nil, fmt.Errorf("podPort is required when pod is specified")
		}
		portMaps = []string{fmt.Sprintf("0:%s", podPort)}
	}

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

	fc := &ForwardConnection{
		stopChannel:  make(chan struct{}),
		readyChannel: make(chan struct{}, 1),
	}

	fc.Forwarder, err = portforward.New(
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

// StartAndWait beings the port forward and wait for it to be ready
func (fc *ForwardConnection) StartAndWait() error {
	var err error
	go func() {
		ginkgo.GinkgoWriter.Printf("Starting port-forward\n")
		err = fc.Forwarder.ForwardPorts()
		if err != nil {
			ginkgo.GinkgoWriter.Printf("port-forward failed with error %s\n", err.Error())
			return
		}
	}()
	select {
	case <-fc.readyChannel:
		ginkgo.GinkgoWriter.Printf("port-forward ready\n")
		return nil
	case <-fc.stopChannel:
		ginkgo.GinkgoWriter.Printf("port-forward closed\n")
		return err
	}
}

// GetLocalPort will return the local port were the forward has started
func (fc *ForwardConnection) GetLocalPort() (string, error) {
	ports, err := fc.Forwarder.GetPorts()
	if err != nil {
		return "", err
	}
	return strconv.Itoa(int(ports[0].Local)), nil
}

// getPortMap takes the first port in the list of ports and return as a map
// with a 0 as the local port for auto-assignment of the local port
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
	namespace, service string,
) (string, []string, error) {
	serviceObj, err := getServiceObject(kubeInterface, namespace, service)
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
	kubeInterface kubernetes.Interface,
	namespace, service string,
) (*corev1.Service, error) {
	return kubeInterface.CoreV1().Services(namespace).Get(context.Background(), service, metav1.GetOptions{})
}

func getPodFromService(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	serviceObj *corev1.Service,
) (*corev1.Pod, error) {
	namespace := serviceObj.Namespace

	labelSelector := metav1.LabelSelector{
		MatchLabels: serviceObj.Spec.Selector,
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
