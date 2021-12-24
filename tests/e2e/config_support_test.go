/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	clusterapiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Set of tests for config map for the operator. It is useful to configure the operator globally to survive
// the upgrades (especially in OLM installation like OpenShift).
var _ = Describe("Config support", Serial, Ordered, Label(tests.LabelDisruptive), func() {
	const (
		clusterWithInheritedLabelsFile = fixturesDir + "/configmap-support/config-support.yaml"
		configMapFile                  = fixturesDir + "/configmap-support/configmap.yaml"
		secretFile                     = fixturesDir + "/configmap-support/secret.yaml"
		configName                     = "postgresql-operator-controller-manager-config"
		level                          = tests.Low
	)
	var operatorNamespace, clusterName, namespace string

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}

		operatorDeployment, err := env.GetOperatorDeployment()
		Expect(err).ToNot(HaveOccurred())

		operatorNamespace = operatorDeployment.GetNamespace()
	})
	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})
	AfterAll(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())

		// Delete the configmap and restore the previous behaviour
		configMap := &corev1.ConfigMap{}
		err = env.Client.Get(env.Ctx, ctrlclient.ObjectKey{Namespace: operatorNamespace, Name: configName}, configMap)
		Expect(err).ToNot(HaveOccurred())
		err = env.Client.Delete(env.Ctx, configMap)
		Expect(err).NotTo(HaveOccurred())

		// Delete the secret and restore the previous behaviour
		secret := &corev1.Secret{}
		err = env.Client.Get(env.Ctx, ctrlclient.ObjectKey{Namespace: operatorNamespace, Name: configName}, secret)
		Expect(err).ToNot(HaveOccurred())
		// If the secret exists, we remove it
		err = env.Client.Delete(env.Ctx, secret)
		Expect(err).NotTo(HaveOccurred())

		err = utils.ReloadOperatorDeployment(env, 120)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can create the configuration map and secret", func() {
		// create a config map where operator is deployed
		cmd := fmt.Sprintf("kubectl apply -n %v -f %v", operatorNamespace, configMapFile)
		_, _, err := utils.Run(cmd)
		Expect(err).ToNot(HaveOccurred())
		// Check if configmap is created
		Eventually(func() ([]corev1.ConfigMap, error) {
			tempConfigMapList := &corev1.ConfigMapList{}
			err := env.Client.List(
				env.Ctx, tempConfigMapList, ctrlclient.InNamespace(operatorNamespace),
				ctrlclient.MatchingFields{"metadata.name": configName},
			)
			return tempConfigMapList.Items, err
		}, 60).Should(HaveLen(1))

		// create a secret where operator is deployed
		cmd = fmt.Sprintf("kubectl apply -n %v -f %v", operatorNamespace, secretFile)
		_, _, err = utils.Run(cmd)
		Expect(err).ToNot(HaveOccurred())
		// Check if configmap is created
		Eventually(func() ([]corev1.Secret, error) {
			tempSecretList := &corev1.SecretList{}
			err := env.Client.List(
				env.Ctx, tempSecretList, ctrlclient.InNamespace(operatorNamespace),
				ctrlclient.MatchingFields{"metadata.name": configName},
			)
			return tempSecretList.Items, err
		}, 10).Should(HaveLen(1))

		// Reload the operator with the new config
		err = utils.ReloadOperatorDeployment(env, 120)
		Expect(err).ToNot(HaveOccurred())
	})

	It("verify label's and annotation's inheritance support", func() {
		clusterName = "configmap-support"
		namespace = "configmap-support-e2e"

		// Create the cluster namespace
		err := env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
		AssertCreateCluster(namespace, clusterName, clusterWithInheritedLabelsFile, env)
		By("verify labels inherited on cluster and pods", func() {
			// Gathers the cluster list using labels
			clusterList := &clusterapiv1.ClusterList{}
			err = env.Client.List(env.Ctx,
				clusterList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{
					"environment": "qaEnv",
				},
			)
			Expect(len(clusterList.Items)).Should(BeEquivalentTo(1),
				"label is not inherited on cluster")

			// Gathers the pod list using labels
			Eventually(func() int32 {
				podList := &corev1.PodList{}
				err = env.Client.List(
					env.Ctx, podList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{
						"environment": "qaEnv",
					},
				)
				return int32(len(podList.Items))
			}, 180).Should(BeEquivalentTo(1), "label is not inherited on pod")
		})
		By("verify wildcard labels inherited", func() {
			// Gathers pod list using wildcard labels
			Eventually(func() int32 {
				podList := &corev1.PodList{}
				err = env.Client.List(
					env.Ctx, podList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{
						"example.com/qa":   "qa",
						"example.com/prod": "prod",
					},
				)
				return int32(len(podList.Items))
			}, 60).Should(BeEquivalentTo(1),
				"wildcard labels are not inherited on pods")
		})
		By("verify annotations inherited on cluster and pods", func() {
			expectedAnnotationValue := "DatabaseApplication"
			// Gathers the cluster list using annotations
			cluster := &clusterapiv1.Cluster{}
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			err = env.Client.Get(env.Ctx, namespacedName, cluster)
			Expect(err).ShouldNot(HaveOccurred())
			annotation := cluster.ObjectMeta.Annotations["categories"]
			Expect(annotation).ShouldNot(BeEmpty(),
				"annotation key is not inherited on cluster")
			Expect(annotation).Should(BeEquivalentTo(expectedAnnotationValue),
				"annotation value is not inherited on cluster")
			// Gathers the pod list using annotations
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				annotation = pod.ObjectMeta.Annotations["categories"]
				Expect(annotation).ShouldNot(BeEmpty(),
					fmt.Sprintf("annotation key is not inherited on pod %v", pod.ObjectMeta.Name))
				Expect(annotation).Should(BeEquivalentTo(expectedAnnotationValue),
					fmt.Sprintf("annotation value is not inherited on pod %v", pod.ObjectMeta.Name))
			}
		})
		By("verify wildcard annotation inherited", func() {
			// Gathers pod list using wildcard labels
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			for _, pod := range podList.Items {
				wildcardAnnotationOne := pod.ObjectMeta.Annotations["example.com/qa"]
				wildcardAnnotationTwo := pod.ObjectMeta.Annotations["example.com/prod"]

				Expect(wildcardAnnotationOne).ShouldNot(BeEmpty(),
					fmt.Sprintf("wildcard annotaioon key %v is not inherited on pod %v", wildcardAnnotationOne,
						pod.ObjectMeta.Name))
				Expect(wildcardAnnotationTwo).ShouldNot(BeEmpty(),
					fmt.Sprintf("wildcard annotation key %v is not inherited on pod %v", wildcardAnnotationTwo,
						pod.ObjectMeta.Name))
				Expect(wildcardAnnotationOne).Should(BeEquivalentTo("qa"),
					fmt.Sprintf("wildcard annotation value %v is not inherited on pod %v", wildcardAnnotationOne,
						pod.ObjectMeta.Name))
				Expect(wildcardAnnotationTwo).Should(BeEquivalentTo("prod"),
					fmt.Sprintf("wildcard annotation value %v is not inherited on pod %v", wildcardAnnotationTwo,
						pod.ObjectMeta.Name))
			}
		})
	})

	// Setting MONITORING_QUERIES_CONFIGMAP: "" should disable monitoring
	// queries on new cluster. We expect those metrics to be missing.
	It("verify metrics details when updated default monitoring configMap queries parameter is set to be empty", func() {
		collectAndAssertDefaultMetricsPresentOnEachPod(namespace, clusterName, false)
	})
})
