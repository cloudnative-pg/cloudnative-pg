/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package sequential

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/manager/controller"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Operator High Availability", func() {
	const namespace = "operator-ha-e2e"
	const sampleFile = fixturesDir + "/operator-ha/operator-ha.yaml"
	const clusterName = "operator-ha"
	var operatorPodNames []string
	var oldLeaderPodName string

	JustAfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			env.DumpClusterEnv(namespace, clusterName,
				"out/"+CurrentGinkgoTestDescription().FullTestText+".log")
		}
	})

	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	It("can work as HA mode", func() {
		// Get Operator Pod name
		operatorPodName, err := env.GetOperatorPod()
		Expect(err).ToNot(HaveOccurred())

		By("having an operator already running", func() {
			// Waiting for the Operator to be up and running
			Eventually(func() (bool, error) {
				return utils.IsPodReady(operatorPodName), err
			}, 120).Should(BeTrue())
		})

		// Get operator namespace
		operatorNamespace, err := env.GetOperatorNamespaceName()
		Expect(err).ToNot(HaveOccurred())

		// Get operator deployment name
		operatorDeployment, err := env.GetOperatorDeployment()
		Expect(err).ToNot(HaveOccurred())

		// Create the cluster namespace
		err = env.CreateNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())

		// Create Cluster
		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		By("verifying current leader", func() {
			// Check for the current Operator Pod leader from ConfigMap
			Expect(getLeaderInfoFromConfigMap(operatorNamespace)).To(HavePrefix(operatorPodName.GetName()))
		})

		By("scale up operator replicas to 3", func() {
			// Set old leader pod name to operator pod name
			oldLeaderPodName = operatorPodName.GetName()

			// Scale up operator deployment to 3 replicas
			cmd := fmt.Sprintf("kubectl scale deploy %v --replicas=3 -n %v",
				operatorDeployment.Name, operatorNamespace)
			_, _, err = tests.Run(cmd)
			Expect(err).ToNot(HaveOccurred())

			// Verify the 3 operator pods are present
			Eventually(func() (int, error) {
				podList, _ := env.GetPodList(operatorNamespace)
				return utils.CountReadyPods(podList.Items), err
			}, 120).Should(BeEquivalentTo(3))

			// Gather pod names from operator deployment
			podList, err := env.GetPodList(operatorNamespace)
			Expect(err).ToNot(HaveOccurred())
			for _, podItem := range podList.Items {
				operatorPodNames = append(operatorPodNames, podItem.GetName())
			}
		})

		By("verifying leader information after scale up", func() {
			// Check for Operator Pod leader from ConfigMap to be the former one
			Eventually(func() (string, error) {
				return getLeaderInfoFromConfigMap(operatorNamespace)
			}, 60).Should(HavePrefix(oldLeaderPodName))
		})

		By("deleting current leader", func() {
			// Force delete former Operator leader Pod
			zero := int64(0)
			forceDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &zero,
			}
			err = env.DeletePod(operatorNamespace, oldLeaderPodName, forceDelete)
			Expect(err).ToNot(HaveOccurred())

			// Verify operator pod should have been deleted
			Eventually(func() []string {
				podList, err := env.GetPodList(operatorNamespace)
				Expect(err).ToNot(HaveOccurred())
				var podNames []string
				for _, podItem := range podList.Items {
					podNames = append(podNames, podItem.GetName())
				}
				return podNames
			}, 120).ShouldNot(ContainElement(oldLeaderPodName))
		})

		By("new leader should be configured", func() {
			// Verify that the leader name is different from the previous one
			Eventually(func() (string, error) {
				return getLeaderInfoFromConfigMap(operatorNamespace)
			}, 120).ShouldNot(HavePrefix(oldLeaderPodName))
		})

		By("verifying reconciliation", func() {
			// Get current CNP cluster's Primary
			currentPrimary, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			oldPrimary := currentPrimary.GetName()

			// Force-delete the primary
			zero := int64(0)
			forceDelete := &ctrlclient.DeleteOptions{
				GracePeriodSeconds: &zero,
			}
			err = env.DeletePod(namespace, currentPrimary.GetName(), forceDelete)
			Expect(err).ToNot(HaveOccurred())

			// Expect a new primary to be elected and promoted
			AssertNewPrimary(namespace, clusterName, oldPrimary)
		})

		By("scale down operator replicas to 1", func() {
			// Scale down operator deployment to one replica
			cmd := fmt.Sprintf("kubectl scale deploy %v --replicas=1 -n %v",
				operatorDeployment.Name, operatorNamespace)
			_, _, err = tests.Run(cmd)
			Expect(err).ToNot(HaveOccurred())

			// Verify there is only one operator pod
			Eventually(func() (int, error) {
				podList := &corev1.PodList{}
				err := env.Client.List(env.Ctx, podList, ctrlclient.InNamespace(operatorNamespace))
				return len(podList.Items), err
			}, 120).Should(BeEquivalentTo(1))

			// And to stay like that
			Consistently(func() int32 {
				podList := &corev1.PodList{}
				err := env.Client.List(
					env.Ctx, podList,
					ctrlclient.InNamespace(operatorNamespace),
				)
				Expect(err).ToNot(HaveOccurred())
				return int32(len(podList.Items))
			}, 10).Should(BeEquivalentTo(1))
		})

		By("verifying leader information after scale down", func() {
			// Get Operator Pod name
			operatorPodName, err := env.GetOperatorPod()
			Expect(err).ToNot(HaveOccurred())

			// Verify the Operator Pod is the leader
			Eventually(func() (string, error) {
				return getLeaderInfoFromConfigMap(operatorNamespace)
			}, 120).Should(HavePrefix(operatorPodName.GetName()))
		})
	})
})

// Gather leader holderIdentity annotation the operator configMap
func getLeaderInfoFromConfigMap(operatorNamespace string) (string, error) {
	// Leader election id is referred as configMap name for store leader details
	leaderElectionID := controller.LeaderElectionID
	configMapList := &corev1.ConfigMapList{}
	err := env.Client.List(
		env.Ctx, configMapList, ctrlclient.InNamespace(operatorNamespace),
		ctrlclient.MatchingFields{"metadata.name": leaderElectionID},
	)
	if err != nil {
		return "", err
	}

	if len(configMapList.Items) != 1 {
		err := fmt.Errorf("configMapList item length is not 1: %v", len(configMapList.Items))
		return "", err
	}

	const leaderAnnotation = "control-plane.alpha.kubernetes.io/leader"
	if annotationInfo, ok :=
		configMapList.Items[0].ObjectMeta.Annotations[leaderAnnotation]; ok {
		mapAnnotationInfo := make(map[string]interface{})
		if err = json.Unmarshal([]byte(annotationInfo), &mapAnnotationInfo); err != nil {
			return "", err
		}
		for key, value := range mapAnnotationInfo {
			if key == "holderIdentity" {
				return fmt.Sprintf("%v", value), nil
			}
		}
		return "", fmt.Errorf("no holderIdentity key found in %v", leaderAnnotation)
	}
	return "", fmt.Errorf("no %v annotation found", leaderAnnotation)
}
