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

// Package pgbouncer provides Ginkgo/Gomega assertions over PGBouncer
// poolers: deployment readiness, pod recreation, endpoint slice routing,
// and connectivity through the pooler service.
package pgbouncer

import (
	"fmt"
	"time"

	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/replication"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	testsutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/deployments"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	pgutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/services"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// AssertPgBouncerPoolerIsSetUp creates a pooler from the given YAML file,
// waits for its Deployment to reach the expected ready replica count, and
// verifies the underlying pods are ready.
func AssertPgBouncerPoolerIsSetUp(
	env *environment.TestingEnvironment,
	namespace, poolerYamlFilePath string,
	expectedInstanceCount int,
) {
	GinkgoHelper()
	resources.CreateResourceFromFile(env, namespace, poolerYamlFilePath)
	Eventually(func(g Gomega) {
		poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
		g.Expect(err).ToNot(HaveOccurred())
		// Wait for the deployment to be ready
		deployment := &appsv1.Deployment{}
		err = env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: poolerName}, deployment)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(deployment.Status.ReadyReplicas).To(BeEquivalentTo(expectedInstanceCount))
	}, 300).Should(Succeed())

	AssertPgBouncerPodsAreReady(env, namespace, poolerYamlFilePath, expectedInstanceCount)
}

// AssertPgBouncerPoolerDeploymentStrategy verifies the pooler's Deployment
// reports the expected rolling-update strategy.
func AssertPgBouncerPoolerDeploymentStrategy(
	env *environment.TestingEnvironment,
	namespace, poolerYamlFilePath string,
	expectedMaxSurge, expectedMaxUnavailable string,
) {
	GinkgoHelper()
	By("verify pooler deployment has expected rolling update configuration", func() {
		Eventually(func(g Gomega) {
			poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
			g.Expect(err).ToNot(HaveOccurred())
			deployment := &appsv1.Deployment{}
			err = env.Client.Get(env.Ctx, types.NamespacedName{Namespace: namespace, Name: poolerName}, deployment)
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(deployment.Spec.Strategy.RollingUpdate.MaxSurge.String()).To(BeEquivalentTo(expectedMaxSurge))
			g.Expect(deployment.Spec.Strategy.RollingUpdate.MaxUnavailable.String()).To(BeEquivalentTo(expectedMaxUnavailable))
		}, 300).Should(Succeed())
	})
}

// AssertPgBouncerPodsAreReady verifies that the pooler's pods are Active
// and Ready in the expected count.
func AssertPgBouncerPodsAreReady(
	env *environment.TestingEnvironment,
	namespace, poolerYamlFilePath string,
	expectedPodCount int,
) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
		g.Expect(err).ToNot(HaveOccurred())
		podList := &corev1.PodList{}
		err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
			ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
		g.Expect(err).ToNot(HaveOccurred())

		podItemsCount := len(podList.Items)
		g.Expect(podItemsCount).To(BeEquivalentTo(expectedPodCount))

		activeAndReadyPodCount := 0
		for _, item := range podList.Items {
			if utils.IsPodActive(item) && utils.IsPodReady(item) {
				activeAndReadyPodCount++
			}
			continue
		}
		g.Expect(activeAndReadyPodCount).To(BeEquivalentTo(expectedPodCount))
	}, 90).Should(Succeed())
}

// AssertReadWriteConnectionUsingPgBouncerService routes a connection
// through the pooler service and verifies it lands on the appropriate end
// (primary if isPoolerRW, replica otherwise).
func AssertReadWriteConnectionUsingPgBouncerService(
	env *environment.TestingEnvironment,
	namespace,
	clusterName,
	poolerYamlFilePath string,
	isPoolerRW bool,
) {
	GinkgoHelper()
	poolerService, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	appUser, generatedAppUserPassword, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix,
	)
	Expect(err).ToNot(HaveOccurred())

	cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
	Expect(err).ToNot(HaveOccurred())

	dbName := cluster.GetApplicationDatabaseName()
	if dbName == "" {
		dbName = apiv1.DefaultApplicationDatabaseName
	}

	if isPoolerRW {
		replication.AssertWritesToPrimarySucceeds(env, namespace, poolerService, dbName, appUser,
			generatedAppUserPassword)
	} else {
		replication.AssertWritesToReplicaFails(env, namespace, poolerService, dbName, appUser,
			generatedAppUserPassword)
	}
}

// AssertPodIsRecreated deletes the single pooler pod and verifies a
// fresh pod (different name) comes up in its place.
func AssertPodIsRecreated(env *environment.TestingEnvironment, namespace, poolerSampleFile string) {
	GinkgoHelper()
	var podNameBeforeDelete string
	poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())

	By(fmt.Sprintf("deleting pooler '%s' pod", poolerName), func() {
		podList := &corev1.PodList{}
		err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
			ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(podList.Items)).Should(BeEquivalentTo(1))
		podNameBeforeDelete = podList.Items[0].GetName()

		Expect(env.Client.Delete(env.Ctx, &podList.Items[0])).To(Succeed())
	})
	By(fmt.Sprintf("verifying pooler '%s' pod has been recreated", poolerName), func() {
		Eventually(func() (bool, error) {
			podList := &corev1.PodList{}
			err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
			if err != nil {
				return false, err
			}
			if len(podList.Items) == 1 {
				if utils.IsPodActive(podList.Items[0]) && utils.IsPodReady(podList.Items[0]) {
					if podNameBeforeDelete != podList.Items[0].GetName() {
						return true, err
					}
				}
			}
			return false, err
		}, 120).Should(BeTrue())
	})
}

// AssertDeploymentIsRecreated deletes the pooler's Deployment and waits
// for the operator to recreate it with fresh pods.
func AssertDeploymentIsRecreated(env *environment.TestingEnvironment, namespace, poolerSampleFile string) {
	GinkgoHelper()
	var deploymentUID types.UID

	poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerSampleFile)
	Expect(err).ToNot(HaveOccurred())

	deploymentNamespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      poolerName,
	}
	deployment := &appsv1.Deployment{}
	Eventually(func(g Gomega) {
		err := env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())
	err = deployments.WaitForReady(env.Ctx, env.Client, deployment, 60)
	Expect(err).ToNot(HaveOccurred())
	deploymentName := deployment.GetName()

	podList := &corev1.PodList{}
	err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
		ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
	Expect(err).ToNot(HaveOccurred())
	uids := make([]types.UID, len(podList.Items))
	for i, p := range podList.Items {
		uids[i] = p.UID
	}

	By(fmt.Sprintf("deleting pgbouncer '%s' deployment", deploymentName), func() {
		deploymentUID = deployment.UID
		err := env.Client.Delete(env.Ctx, deployment)
		Expect(err).ToNot(HaveOccurred())
	})
	By(fmt.Sprintf("verifying new deployment '%s' has been recreated", deploymentName), func() {
		Eventually(func() (types.UID, error) {
			err = env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
			return deployment.UID, err
		}, 300).ShouldNot(BeEquivalentTo(deploymentUID))
	})
	By(fmt.Sprintf("new '%s' deployment has new pods ready", deploymentName), func() {
		err := deployments.WaitForReady(env.Ctx, env.Client, deployment, 120)
		Expect(err).ToNot(HaveOccurred())
	})
	By("verifying UIDs of pods have changed", func() {
		Eventually(func() (int, error) {
			err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
			return len(podList.Items), err
		}, 60).Should(BeNumerically("==", *deployment.Spec.Replicas))
		newuids := make([]types.UID, len(podList.Items))
		for i, p := range podList.Items {
			newuids[i] = p.UID
		}
		Expect(len(funk.Join(uids, newuids, funk.InnerJoin).([]types.UID))).To(BeEquivalentTo(0))
	})
}

// AssertPgBouncerEndpointsContainsPodsIP verifies the pooler service's
// EndpointSlice points at exactly the expected pooler pods.
func AssertPgBouncerEndpointsContainsPodsIP(
	env *environment.TestingEnvironment,
	namespace,
	poolerYamlFilePath string,
	expectedPodCount int,
) {
	GinkgoHelper()
	poolerServiceName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	endpointSlice := &discoveryv1.EndpointSlice{}
	Eventually(func(g Gomega) {
		var err error
		endpointSlice, err = testsutils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, poolerServiceName)
		g.Expect(err).ToNot(HaveOccurred())
	}).Should(Succeed())

	poolerName, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())
	podList := &corev1.PodList{}
	err = env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(namespace),
		ctrlclient.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
	Expect(err).ToNot(HaveOccurred())
	Expect(endpointSlice.Endpoints).ToNot(BeEmpty())

	var pgBouncerPods []*corev1.Pod
	for _, endpoint := range endpointSlice.Endpoints {
		ip := endpoint.Addresses[0]
		for podIndex, pod := range podList.Items {
			if pod.Status.PodIP == ip {
				pgBouncerPods = append(pgBouncerPods, &podList.Items[podIndex])
				continue
			}
		}
	}

	Expect(pgBouncerPods).Should(HaveLen(expectedPodCount), "Pod length or IP mismatch in endpoint")
}

// AssertPgBouncerHasServiceNameInsideHostParameter verifies that the
// pgbouncer.ini inside every pooler pod references the given service name
// in its host= parameter.
func AssertPgBouncerHasServiceNameInsideHostParameter(
	env *environment.TestingEnvironment,
	serviceName string,
	podList *corev1.PodList,
) {
	GinkgoHelper()
	expected := fmt.Sprintf("host=%s", serviceName)
	commandTimeout := 10 * time.Second
	for i := range podList.Items {
		pod := &podList.Items[i]
		out, _, err := env.EventuallyExecCommand(env.Ctx, *pod, "pgbouncer", &commandTimeout,
			"grep", expected, "controller/configs/pgbouncer.ini")
		Expect(err).ToNot(HaveOccurred())
		Expect(out).To(ContainSubstring(expected))
	}
}

// DeleteTableUsingPgBouncerService connects to the pooler service as the
// application user and runs DROP TABLE table1 via psql inside pod.
func DeleteTableUsingPgBouncerService(
	env *environment.TestingEnvironment,
	namespace,
	clusterName,
	poolerYamlFilePath string,
	pod *corev1.Pod,
) {
	GinkgoHelper()
	poolerService, err := yaml.GetResourceNameFromYAML(env.Scheme, poolerYamlFilePath)
	Expect(err).ToNot(HaveOccurred())

	appUser, generatedAppUserPassword, err := secrets.GetCredentials(
		env.Ctx, env.Client,
		clusterName, namespace, apiv1.ApplicationUserSecretSuffix,
	)
	Expect(err).ToNot(HaveOccurred())
	pgasserts.AssertConnection(env, namespace, poolerService, pgutils.AppDBName, appUser, generatedAppUserPassword)

	connectionTimeout := time.Second * 10
	dsn := services.CreateDSN(poolerService, appUser, pgutils.AppDBName, generatedAppUserPassword,
		services.Require, 5432)
	_, _, err = env.EventuallyExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &connectionTimeout,
		"psql", dsn, "-tAc", "DROP TABLE table1")
	Expect(err).ToNot(HaveOccurred())
}
