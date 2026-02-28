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

package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

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
	var err error

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("should create and delete a rw managed service", func(ctx SpecContext) {
		const clusterManifest = fixturesDir + "/managed_services/cluster-managed-services-rw.yaml.template"
		const serviceName = "test-rw"
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, clusterManifest, env)

		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
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
			Eventually(func(g Gomega) error {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				cluster.Spec.Managed.Services.Additional = []apiv1.ManagedService{}
				return env.Client.Update(ctx, cluster)
			}, RetryTimeout, PollingTime).Should(Succeed())

			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ManagedServices], env)
			Eventually(func(g Gomega) {
				var serviceRW corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: namespace}, &serviceRW)
				g.Expect(apierrs.IsNotFound(err)).To(BeTrue())
			}, testTimeouts[timeouts.ManagedServices]).Should(Succeed())
		})
	})

	It("should properly handle disabledDefaultServices field", func(ctx SpecContext) {
		const clusterManifest = fixturesDir + "/managed_services/cluster-managed-services-no-default.yaml.template"

		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, clusterManifest, env)

		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
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
			Eventually(func(g Gomega) error {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				cluster.Spec.Managed.Services.DisabledDefaultServices = []apiv1.DisabledDefaultServiceSelectorType{}
				return env.Client.Update(ctx, cluster)
			}, RetryTimeout, PollingTime).Should(Succeed())

			AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ManagedServices], env)

			Eventually(func(g Gomega) {
				var service corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: rw.Name}, &service)
				g.Expect(err).ToNot(HaveOccurred())
			}, testTimeouts[timeouts.ManagedServices]).Should(Succeed())

			Eventually(func(g Gomega) {
				var service corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: ro.Name}, &service)
				g.Expect(err).ToNot(HaveOccurred())
			}, testTimeouts[timeouts.ManagedServices]).Should(Succeed())

			Eventually(func(g Gomega) {
				var service corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: r.Name}, &service)
				g.Expect(err).ToNot(HaveOccurred())
			}, testTimeouts[timeouts.ManagedServices]).Should(Succeed())
		})
	})

	It("should properly handle replace update strategy", func(ctx SpecContext) {
		const clusterManifest = fixturesDir + "/managed_services/cluster-managed-services-replace-strategy.yaml.template"
		const serviceName = "test-rw"
		namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())

		clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, clusterManifest, env)

		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		var creationTimestamp metav1.Time
		var uid types.UID
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

			creationTimestamp = serviceRW.CreationTimestamp
			uid = serviceRW.UID
		})

		By("updating the service definition", func() {
			Eventually(func(g Gomega) error {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				g.Expect(err).ToNot(HaveOccurred())
				cluster.Spec.Managed.Services.Additional[0].ServiceTemplate.ObjectMeta.Labels["new-label"] = "new"
				return env.Client.Update(ctx, cluster)
			}, RetryTimeout, PollingTime).Should(Succeed())
		})

		By("expecting the service to be recreated", func() {
			Eventually(func(g Gomega) {
				var service corev1.Service
				err = env.Client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: serviceName}, &service)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(service.Labels["new-label"]).To(Equal("new"))
				g.Expect(service.UID).ToNot(Equal(uid))
				g.Expect(service.CreationTimestamp).ToNot(Equal(creationTimestamp))
			}, testTimeouts[timeouts.ManagedServices]).Should(Succeed())
		})
	})
})
