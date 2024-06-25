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

package client

import (
	"bytes"
	"context"
	"fmt"

	"github.com/cloudnative-pg/cnpg-i/pkg/backup"
	"github.com/cloudnative-pg/cnpg-i/pkg/identity"
	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	"github.com/cloudnative-pg/cnpg-i/pkg/operator"
	"github.com/cloudnative-pg/cnpg-i/pkg/reconciler"
	"github.com/cloudnative-pg/cnpg-i/pkg/wal"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	decoder "k8s.io/apimachinery/pkg/util/yaml"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin/connection"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeConnection struct {
	lifecycleClient       lifecycle.OperatorLifecycleClient
	lifecycleCapabilities []*lifecycle.OperatorLifecycleCapabilities
	name                  string
}

func (f fakeConnection) Name() string {
	return f.name
}

func (f fakeConnection) Metadata() connection.Metadata {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) LifecycleClient() lifecycle.OperatorLifecycleClient {
	return f.lifecycleClient
}

func (f fakeConnection) OperatorClient() operator.OperatorClient {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) WALClient() wal.WALClient {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) BackupClient() backup.BackupClient {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) ReconcilerHooksClient() reconciler.ReconcilerHooksClient {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) PluginCapabilities() []identity.PluginCapability_Service_Type {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) OperatorCapabilities() []operator.OperatorCapability_RPC_Type {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) WALCapabilities() []wal.WALCapability_RPC_Type {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) LifecycleCapabilities() []*lifecycle.OperatorLifecycleCapabilities {
	return f.lifecycleCapabilities
}

func (f fakeConnection) BackupCapabilities() []backup.BackupCapability_RPC_Type {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) ReconcilerCapabilities() []reconciler.ReconcilerHooksCapability_Kind {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) Ping(_ context.Context) error {
	panic("not implemented") // TODO: Implement
}

func (f fakeConnection) Close() error {
	panic("not implemented") // TODO: Implement
}

type fakeLifecycleClient struct {
	capabilitiesError  error
	lifecycleHookError error
	labelInjector      map[string]string
	capabilities       []*lifecycle.OperatorLifecycleCapabilities
}

func newFakeLifecycleClient(
	capabilities []*lifecycle.OperatorLifecycleCapabilities,
	labelInjector map[string]string,
	capabilitiesError error,
	lifecycleHookError error,
) *fakeLifecycleClient {
	return &fakeLifecycleClient{
		capabilities:       capabilities,
		labelInjector:      labelInjector,
		capabilitiesError:  capabilitiesError,
		lifecycleHookError: lifecycleHookError,
	}
}

func (f *fakeLifecycleClient) GetCapabilities(
	_ context.Context,
	_ *lifecycle.OperatorLifecycleCapabilitiesRequest,
	_ ...grpc.CallOption,
) (*lifecycle.OperatorLifecycleCapabilitiesResponse, error) {
	return &lifecycle.OperatorLifecycleCapabilitiesResponse{LifecycleCapabilities: f.capabilities}, f.capabilitiesError
}

func (f *fakeLifecycleClient) LifecycleHook(
	_ context.Context,
	in *lifecycle.OperatorLifecycleRequest,
	_ ...grpc.CallOption,
) (*lifecycle.OperatorLifecycleResponse, error) {
	defRes := &lifecycle.OperatorLifecycleResponse{
		JsonPatch: nil,
	}

	if f.lifecycleHookError != nil {
		return defRes, f.lifecycleHookError
	}

	var cluster appsv1.Deployment
	if err := tryDecode(in.ClusterDefinition, &cluster); err != nil {
		return nil, fmt.Errorf("invalid cluster supplied: %w", err)
	}

	var instance corev1.Pod
	if err := tryDecode(in.ObjectDefinition, &instance); err != nil {
		return defRes, nil
	}
	var matches bool
	for _, capability := range f.capabilities {
		if capability.Kind != instance.Kind {
			continue
		}
	}

	if matches {
		return defRes, nil
	}

	switch in.OperationType.Type {
	case lifecycle.OperatorOperationType_TYPE_CREATE:
		originalInstance := instance.DeepCopy()
		if instance.Labels == nil {
			instance.Labels = map[string]string{}
		}
		for key, value := range f.labelInjector {
			instance.Labels[key] = value
		}

		res, err := createJSONPatchForLabels(originalInstance, &instance)
		return &lifecycle.OperatorLifecycleResponse{JsonPatch: res}, err
	case lifecycle.OperatorOperationType_TYPE_DELETE:
		originalInstance := instance.DeepCopy()
		for key := range f.labelInjector {
			delete(instance.Labels, key)
		}
		res, err := createJSONPatchForLabels(originalInstance, &instance)
		return &lifecycle.OperatorLifecycleResponse{JsonPatch: res}, err
	default:
		return defRes, nil
	}
}

func tryDecode[T k8client.Object](rawObj []byte, cast T) error {
	dec := decoder.NewYAMLOrJSONDecoder(bytes.NewReader(rawObj), 1000)

	return dec.Decode(cast)
}

func (f *fakeLifecycleClient) set(d *fakeConnection) {
	if d == nil {
		return
	}

	d.lifecycleClient = f
	d.lifecycleCapabilities = f.capabilities
}

var _ = Describe("LifecycleHook", func() {
	var (
		d            *data
		clusterObj   k8client.Object
		capabilities = []*lifecycle.OperatorLifecycleCapabilities{
			{
				Group: "",
				Kind:  "Pod",
				OperationTypes: []*lifecycle.OperatorOperationType{
					{
						Type: lifecycle.OperatorOperationType_TYPE_CREATE,
					},
					{
						Type: lifecycle.OperatorOperationType_TYPE_DELETE,
					},
				},
			},
		}
	)

	BeforeEach(func() {
		d = &data{
			plugins: []connection.Interface{
				&fakeConnection{
					name: "test",
				},
			},
		}

		clusterObj = &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{},
		}
	})

	It("should correctly inject the values in the passed object", func(ctx SpecContext) {
		mapInjector := map[string]string{"test": "test"}
		f := newFakeLifecycleClient(capabilities, mapInjector, nil, nil)
		f.set(d.plugins[0].(*fakeConnection))

		pod := &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{},
		}
		obj, err := d.LifecycleHook(ctx, plugin.OperationVerbCreate, clusterObj, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(obj).ToNot(BeNil())
		podModified, ok := obj.(*corev1.Pod)
		Expect(ok).To(BeTrue())
		Expect(podModified.Labels).To(Equal(mapInjector))
	})

	// TODO: not currently passing
	It("should correctly remove the values in the passed object", func(ctx SpecContext) {
		mapInjector := map[string]string{"test": "test"}
		f := newFakeLifecycleClient(capabilities, mapInjector, nil, nil)
		f.set(d.plugins[0].(*fakeConnection))

		pod := &corev1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"test":  "test",
					"other": "stuff",
				},
			},
		}
		obj, err := d.LifecycleHook(ctx, plugin.OperationVerbDelete, clusterObj, pod)
		Expect(err).ToNot(HaveOccurred())
		Expect(obj).ToNot(BeNil())
		podModified, ok := obj.(*corev1.Pod)
		Expect(ok).To(BeTrue())
		Expect(podModified.Labels).To(Equal(map[string]string{"other": "stuff"}))
	})
})

func createJSONPatchForLabels(originalInstance, instance *corev1.Pod) ([]byte, error) {
	type patch []struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}

	op := "replace"
	if len(originalInstance.Labels) == 0 {
		op = "add"
	}
	p := patch{
		{
			Op:    op,
			Path:  "/metadata/labels",
			Value: instance.Labels,
		},
	}

	return json.Marshal(p)
}
