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

package specs

import (
	"context"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/testing"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type mockPodMonitorManager struct {
	isEnabled  bool
	podMonitor *v1.PodMonitor
}

func (m *mockPodMonitorManager) IsPodMonitorEnabled() bool {
	return m.isEnabled
}

func (m *mockPodMonitorManager) BuildPodMonitor() *v1.PodMonitor {
	return m.podMonitor
}

var _ = Describe("CreateOrPatchPodMonitor", func() {

	var (
		mockCtx             context.Context
		fakeCli             k8client.Client
		fakeDiscoveryClient discovery.DiscoveryInterface
		mockManager         *mockPodMonitorManager
		podManager          PodMonitorManagerController
	)

	BeforeEach(func() {
		mockCtx = context.Background()
		mockManager = &mockPodMonitorManager{}
		mockManager.isEnabled = true
		mockManager.podMonitor = &v1.PodMonitor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}

		fakeCli = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).Build()
		fakeDiscoveryClient = &fakediscovery.FakeDiscovery{
			Fake: &testing.Fake{
				Resources: []*metav1.APIResourceList{
					{
						GroupVersion: "monitoring.coreos.com/v1",
						APIResources: []metav1.APIResource{
							{
								Name:       "podmonitors",
								Kind:       "PodMonitor",
								Namespaced: true,
							},
						},
					},
				},
			},
		}

		podManager = PodMonitorManagerController{
			Manager:   mockManager,
			Ctx:       mockCtx,
			Discovery: fakeDiscoveryClient,
			Client:    fakeCli,
		}
	})

	It("should create the PodMonitor  when it is enabled and doesn't already exists", func() {
		err := podManager.CreateOrPatchPodMonitor()
		Expect(err).ToNot(HaveOccurred())

		podMonitor := &v1.PodMonitor{}
		err = fakeCli.Get(
			mockCtx,
			types.NamespacedName{
				Name:      mockManager.podMonitor.Name,
				Namespace: mockManager.podMonitor.Namespace,
			},
			podMonitor,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(podMonitor.Name).To(Equal(mockManager.podMonitor.Name))
		Expect(podMonitor.Namespace).To(Equal(mockManager.podMonitor.Namespace))
	})

	It("should not return an error when PodMonitor is disabled", func() {
		mockManager.isEnabled = false
		err := podManager.CreateOrPatchPodMonitor()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should remove the PodMonitor if it is disabled when the PodMonitor exists", func() {
		// Create the PodMonitor with the fake client
		err := fakeCli.Create(mockCtx, mockManager.podMonitor)
		Expect(err).ToNot(HaveOccurred())

		mockManager.isEnabled = false
		err = podManager.CreateOrPatchPodMonitor()
		Expect(err).ToNot(HaveOccurred())

		// Ensure the PodMonitor doesn't exist anymore
		podMonitor := &v1.PodMonitor{}
		err = fakeCli.Get(
			mockCtx,
			types.NamespacedName{
				Name:      mockManager.podMonitor.Name,
				Namespace: mockManager.podMonitor.Namespace,
			},
			podMonitor,
		)
		Expect(err).To(HaveOccurred())
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})

	It("should patch the PodMonitor with updated labels and annotations", func() {
		initialLabels := map[string]string{"label1": "value1"}
		initialAnnotations := map[string]string{"annotation1": "value1"}

		mockManager.podMonitor.Labels = initialLabels
		mockManager.podMonitor.Annotations = initialAnnotations
		err := fakeCli.Create(mockCtx, mockManager.podMonitor)
		Expect(err).ToNot(HaveOccurred())

		updatedLabels := map[string]string{"label1": "changedValue1", "label2": "value2"}
		updatedAnnotations := map[string]string{"annotation1": "changedValue1", "annotation2": "value2"}

		mockManager.podMonitor.Labels = updatedLabels
		mockManager.podMonitor.Annotations = updatedAnnotations

		err = podManager.CreateOrPatchPodMonitor()
		Expect(err).ToNot(HaveOccurred())

		podMonitor := &v1.PodMonitor{}
		err = fakeCli.Get(
			mockCtx,
			types.NamespacedName{
				Name:      mockManager.podMonitor.Name,
				Namespace: mockManager.podMonitor.Namespace,
			},
			podMonitor,
		)
		Expect(err).ToNot(HaveOccurred())

		Expect(podMonitor.Labels).To(Equal(updatedLabels))
		Expect(podMonitor.Annotations).To(Equal(updatedAnnotations))
	})
})
