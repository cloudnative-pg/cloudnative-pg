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

	"github.com/onsi/ginkgo/v2/types"
	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests for config map for the operator. It is useful to configure the operator globally to survive
// the upgrades (especially in OLM installation like OpenShift).
var _ = Describe("Config support", Serial, Ordered, Label(tests.LabelDisruptive, tests.LabelClusterMetadata), func() {
	const (
		clusterWithInheritedLabelsFile = fixturesDir + "/configmap-support/config-support.yaml.template"
		configMapFile                  = fixturesDir + "/configmap-support/configmap.yaml"
		secretFile                     = fixturesDir + "/configmap-support/secret.yaml"
		configName                     = "cnpg-controller-manager-config"
		clusterName                    = "configmap-support"
		namespacePrefix                = "configmap-support-e2e"
		level                          = tests.Low
	)
	var operatorNamespace, curlPodName, namespace string

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
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	AfterAll(func() {
		if CurrentSpecReport().State.Is(types.SpecStateSkipped) {
			return
		}
		// Delete the configmap and restore the previous behaviour
		configMap := &corev1.ConfigMap{}
		err := env.Client.Get(env.Ctx, ctrlclient.ObjectKey{Namespace: operatorNamespace, Name: configName}, configMap)
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

	It("creates the configuration map and secret", func() {
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

	It("creates a cluster", func() {
		var err error
		namespace, err = env.CreateUniqueNamespace(namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() error {
			return env.DeleteNamespace(namespace)
		})

		// Create the curl client pod and wait for it to be ready.
		By("setting up curl client pod", func() {
			curlClient := utils.CurlClient(namespace)
			err := utils.PodCreateAndWaitForReady(env, &curlClient, 240)
			Expect(err).ToNot(HaveOccurred())
			curlPodName = curlClient.GetName()
		})

		AssertCreateCluster(namespace, clusterName, clusterWithInheritedLabelsFile, env)
	})

	It("verify label's and annotation's inheritance when global config-map changed", func() {
		cluster, err := env.GetCluster(namespace, clusterName)
		Expect(err).NotTo(HaveOccurred())

		By("checking the cluster has the requested labels", func() {
			expectedLabels := map[string]string{"environment": "qaEnv"}
			Expect(utils.ClusterHasLabels(cluster, expectedLabels)).To(BeTrue())
		})
		By("checking the pods inherit labels matching the ones in the configuration secret", func() {
			expectedLabels := map[string]string{"environment": "qaEnv"}
			Eventually(func() (bool, error) {
				return utils.AllClusterPodsHaveLabels(env, namespace, clusterName, expectedLabels)
			}, 180).Should(BeTrue())
		})
		By("checking the pods inherit labels matching wildcard ones in the configuration secret", func() {
			expectedLabels := map[string]string{
				"example.com/qa":   "qa",
				"example.com/prod": "prod",
			}
			Eventually(func() (bool, error) {
				return utils.AllClusterPodsHaveLabels(env, namespace, clusterName, expectedLabels)
			}, 180).Should(BeTrue())
		})
		By("checking the cluster has the requested annotation", func() {
			expectedAnnotations := map[string]string{"categories": "DatabaseApplication"}
			Expect(utils.ClusterHasAnnotations(cluster, expectedAnnotations)).To(BeTrue())
		})
		By("checking the pods inherit annotations matching the ones in the configuration configMap", func() {
			expectedAnnotations := map[string]string{"categories": "DatabaseApplication"}
			Eventually(func() (bool, error) {
				return utils.AllClusterPodsHaveAnnotations(env, namespace, clusterName, expectedAnnotations)
			}, 180).Should(BeTrue())
		})
		By("checking the pods inherit annotations matching wildcard ones in the configuration configMap", func() {
			expectedAnnotations := map[string]string{
				"example.com/qa":   "qa",
				"example.com/prod": "prod",
			}
			Eventually(func() (bool, error) {
				return utils.AllClusterPodsHaveLabels(env, namespace, clusterName, expectedAnnotations)
			}, 180).Should(BeTrue())
		})
	})

	// Setting MONITORING_QUERIES_CONFIGMAP: "" should disable monitoring
	// queries on new cluster. We expect those metrics to be missing.
	It("verify metrics details when updated default monitoring configMap queries parameter is set to be empty", func() {
		collectAndAssertDefaultMetricsPresentOnEachPod(namespace, clusterName, curlPodName, false)
	})
})
