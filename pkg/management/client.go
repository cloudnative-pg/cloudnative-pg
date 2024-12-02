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

// Package management contains all the features needed by the instance
// manager that runs in each Pod as PID 1
package management

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
)

var (
	// Scheme used for the instance manager
	Scheme = runtime.NewScheme()

	// readinessCheckRetry is used to wait until the API server is reachable
	readinessCheckRetry = wait.Backoff{
		Steps:    5,
		Duration: 1 * time.Second,
		Factor:   3.0,
		Jitter:   0.1,
	}
)

func init() {
	_ = clientgoscheme.AddToScheme(Scheme)
	_ = apiv1.AddToScheme(Scheme)
}

// NewControllerRuntimeClient creates a new typed K8s client where
// the PostgreSQL CRD and some basic k8s resources have been already registered.
//
// While using the typed client you may encounter an error like this:
// no matches for kind "X" in version "Y".
//
// This means that the runtime.Object is missing and needs to registered in the client.
func NewControllerRuntimeClient() (client.WithWatch, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	mapper := meta.NewDefaultRESTMapper([]schema.GroupVersion{apiv1.GroupVersion})
	// add here any resource that need to be registered.
	objectsToRegister := []runtime.Object{
		// custom resources
		&apiv1.Cluster{}, &apiv1.Backup{}, &apiv1.Pooler{}, &apiv1.ImageCatalog{}, &apiv1.ClusterImageCatalog{},
		// k8s resources needed for the typedClient to work properly
		&v1.ConfigMap{}, &v1.Secret{},
	}

	// we register the resources
	for _, obj := range objectsToRegister {
		gvk, err := apiutil.GVKForObject(obj, Scheme)
		if err != nil {
			return nil, err
		}

		mapper.Add(gvk, meta.RESTScopeNamespace)
	}

	return client.NewWithWatch(config, client.Options{
		Scheme: Scheme,
		Mapper: mapper,
	})
}

// newClientGoClient creates a new client-go kubernetes interface.
// It is used only to create event recorders, as controller-runtime do.
func newClientGoClient() (kubernetes.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

// NewEventRecorder creates a new event recorder
func NewEventRecorder() (record.EventRecorder, error) {
	kubeClient, err := newClientGoClient()
	if err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartRecordingToSink(
		&typedcorev1.EventSinkImpl{
			Interface: kubeClient.CoreV1().Events(""),
		})
	recorder := eventBroadcaster.NewRecorder(
		Scheme,
		v1.EventSource{Component: "instance-manager"},
	)

	return recorder, nil
}

// WaitForGetCluster will wait for a successful get cluster to be executed.
// Returns any error encountered.
func WaitForGetCluster(ctx context.Context, clusterObjectKey client.ObjectKey) error {
	logger := log.FromContext(ctx).WithName("wait-for-get-cluster")

	cli, err := NewControllerRuntimeClient()
	if err != nil {
		logger.Error(err, "error while creating a standalone Kubernetes client")
		return err
	}

	return WaitForGetClusterWithClient(ctx, cli, clusterObjectKey)
}

// WaitForGetClusterWithClient will wait for a successful get cluster to be executed
func WaitForGetClusterWithClient(ctx context.Context, cli client.Client, clusterObjectKey client.ObjectKey) error {
	logger := log.FromContext(ctx).WithName("wait-for-get-cluster")

	err := retry.OnError(readinessCheckRetry, resources.RetryAlways, func() error {
		if err := cli.Get(ctx, clusterObjectKey, &apiv1.Cluster{}); err != nil {
			logger.Warning("Encountered an error while executing get cluster. Will wait and retry", "error", err.Error())
			return err
		}
		return nil
	})
	if err != nil {
		const message = "error while waiting for the API server to be reachable"
		logger.Error(err, message)
		return fmt.Errorf("%s: %w", message, err)
	}

	return nil
}
