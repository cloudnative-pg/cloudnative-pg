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

	"github.com/cloudnative-pg/cnpg-i/pkg/lifecycle"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"google.golang.org/grpc"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/json"
	decoder "k8s.io/apimachinery/pkg/util/yaml"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cnpi/plugin"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type fakeLifecycleClient struct {
	capabilitiesError  error
	lifecycleHookError error
	labelInjector      map[string]string
	capabilities       []*lifecycle.LifecycleCapabilities
}

func newFakeLifecycleClient(
	capabilities []*lifecycle.LifecycleCapabilities,
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
	_ *lifecycle.LifecycleCapabilitiesRequest,
	_ ...grpc.CallOption,
) (*lifecycle.LifecycleCapabilitiesResponse, error) {
	return &lifecycle.LifecycleCapabilitiesResponse{LifecycleCapabilities: f.capabilities}, f.capabilitiesError
}

func (f *fakeLifecycleClient) LifecycleHook(
	_ context.Context,
	in *lifecycle.LifecycleRequest,
	_ ...grpc.CallOption,
) (*lifecycle.LifecycleResponse, error) {
	defRes := &lifecycle.LifecycleResponse{
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
	case lifecycle.OperationType_TYPE_CREATE:
		rawInstance, err := json.Marshal(instance)
		if err != nil {
			return defRes, fmt.Errorf("(create) while serializing the instance: %w", err)
		}
		if instance.Labels == nil {
			instance.Labels = map[string]string{}
		}
		for key, value := range f.labelInjector {
			instance.Labels[key] = value
		}

		modifiedInstance, err := json.Marshal(instance)
		if err != nil {
			return defRes, fmt.Errorf("(create) while serializing the modifiedinstance: %w", err)
		}

		res, err := jsonpatch.CreateMergePatch(rawInstance, modifiedInstance)
		return &lifecycle.LifecycleResponse{JsonPatch: res}, err
	case lifecycle.OperationType_TYPE_DELETE:
		rawInstance, err := json.Marshal(instance)
		if err != nil {
			return defRes, fmt.Errorf("(delete) while serializing the instance: %w", err)
		}
		for key := range f.labelInjector {
			delete(instance.Labels, key)
		}
		modifiedInstance, err := json.Marshal(instance)
		if err != nil {
			return defRes, fmt.Errorf("(delete) while serializing the modifiedinstance: %w", err)
		}
		res, err := jsonpatch.CreateMergePatch(rawInstance, modifiedInstance)
		return &lifecycle.LifecycleResponse{JsonPatch: res}, err
	default:
		return defRes, nil
	}
}

func tryDecode[T k8client.Object](rawObj []byte, cast T) error {
	dec := decoder.NewYAMLOrJSONDecoder(bytes.NewReader(rawObj), 1000)

	return dec.Decode(cast)
}

func (f *fakeLifecycleClient) set(d *pluginData) {
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
		capabilities = []*lifecycle.LifecycleCapabilities{
			{
				Group: "",
				Kind:  "Pod",
				OperationType: []*lifecycle.OperationType{
					{
						Type: lifecycle.OperationType_TYPE_CREATE,
					},
					{
						Type: lifecycle.OperationType_TYPE_DELETE,
					},
				},
			},
		}
	)

	BeforeEach(func() {
		d = &data{
			plugins: []pluginData{
				{
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
		f.set(&d.plugins[0])

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
		f.set(&d.plugins[0])

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
