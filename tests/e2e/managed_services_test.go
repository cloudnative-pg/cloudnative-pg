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

package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster with some post-init-sql query and verifying that they are really executed

// Set of tests in which we check that the initdb options are really applied
var _ = Describe("Managed services tests", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		level           = tests.Medium
		namespacePrefix = "managed-services"
	)
	var namespace string

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("should create and delete a rw managed service", func(ctx SpecContext) {
		const clusterManifest = fixturesDir + "/managed_services/cluster-managed-services-rw.yaml.template"
		const serviceName = "test-rw"
		namespace, err := env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})

		clusterName, err := env.GetResourceNameFromYAML(clusterManifest)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, clusterManifest, env)

		cluster, err := env.GetCluster(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("ensuring the service is created", func() {
			baseRWService := specs.CreateClusterReadWriteService(*cluster)
			var serviceRW corev1.Service
			err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: serviceName}, &serviceRW)
			Expect(err).ToNot(HaveOccurred())
			Expect(serviceRW.Spec.Selector).To(Equal(baseRWService.Spec.Selector))
			Expect(serviceRW.Labels).ToNot(BeNil())
			Expect(serviceRW.Labels["test-label"]).To(Equal("true"),
				fmt.Sprintf("found labels: %s", serviceRW.Labels))
			Expect(serviceRW.Annotations).ToNot(BeNil())
			Expect(serviceRW.Annotations["test-annotation"]).To(Equal("true"))
		})

		By("ensuring the service is deleted when removed from the additional field", func() {
			cluster.Spec.Managed.Services.Additional = []apiv1.ManagedService{}
			err = env.Client.Update(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
			AssertClusterIsReady(namespace, clusterName, testTimeouts[utils.ManagedServices], env)
			Eventually(func(g Gomega) {
				var serviceRW corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: namespace}, &serviceRW)
				g.Expect(apierrs.IsNotFound(err)).To(BeTrue())
			}, testTimeouts[utils.ManagedServices]).Should(Succeed())
		})
	})

	It("should properly handle disabledDefaultServices field", func(ctx SpecContext) {
		const clusterManifest = fixturesDir + "/managed_services/cluster-managed-services-no-default.yaml.template"

		namespace, err := env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})

		clusterName, err := env.GetResourceNameFromYAML(clusterManifest)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, clusterManifest, env)

		cluster, err := env.GetCluster(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		ro := specs.CreateClusterReadOnlyService(*cluster)
		rw := specs.CreateClusterReadWriteService(*cluster)
		r := specs.CreateClusterReadService(*cluster)

		By("not creating the disabled services", func() {
			err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ro.Name}, &corev1.Service{})
			Expect(apierrs.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("service: %s should not be found", ro.Name))
			err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: r.Name}, &corev1.Service{})
			Expect(apierrs.IsNotFound(err)).To(BeTrue(), fmt.Sprintf("service: %s should not be found", r.Name))
		})

		By("ensuring rw service is present", func() {
			err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rw.Name}, &corev1.Service{})
			Expect(err).ToNot(HaveOccurred())
		})

		By("creating them when they are re-enabled", func() {
			cluster.Spec.Managed.Services.DisabledDefaultServices = []apiv1.ServiceSelectorType{}
			err = env.Client.Update(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())

			AssertClusterIsReady(namespace, clusterName, testTimeouts[utils.ManagedServices], env)

			Eventually(func(g Gomega) {
				var service corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rw.Name}, &service)
				g.Expect(err).ToNot(HaveOccurred())
			}, testTimeouts[utils.ManagedServices]).Should(Succeed())

			Eventually(func(g Gomega) {
				var service corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ro.Name}, &service)
				g.Expect(err).ToNot(HaveOccurred())
			}, testTimeouts[utils.ManagedServices]).Should(Succeed())

			Eventually(func(g Gomega) {
				var service corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: r.Name}, &service)
				g.Expect(err).ToNot(HaveOccurred())
			}, testTimeouts[utils.ManagedServices]).Should(Succeed())
		})
	})
})
