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
	"os"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rolling updates", Label(tests.LabelPostgresConfiguration), func() {
	const level = tests.Medium
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	// gatherClusterInfo returns the current lists of pods, pod UIDs and pvc UIDs in a given cluster
	gatherClusterInfo := func(namespace string, clusterName string) ([]string, []types.UID, []types.UID, error) {
		var podNames []string
		var podUIDs []types.UID
		var pvcUIDs []types.UID
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podNames = append(podNames, pod.GetName())
			podUIDs = append(podUIDs, pod.GetUID())
			pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
			pvc := &corev1.PersistentVolumeClaim{}
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      pvcName,
			}
			if err := env.Client.Get(env.Ctx, namespacedPVCName, pvc); err != nil {
				return nil, nil, nil, err
			}
			pvcUIDs = append(pvcUIDs, pvc.GetUID())
		}
		return podNames, podUIDs, pvcUIDs, nil
	}

	// Verify that after an update all the pods are ready and running
	// an updated image
	AssertUpdateImage := func(namespace string, clusterName string) {
		// TODO: the nodes are downloading the image sequentially,
		// slowing this down
		timeout := 900

		// Update to the latest minor
		updatedImageName := os.Getenv("POSTGRES_IMG")
		if updatedImageName == "" {
			updatedImageName = configuration.Current.PostgresImageName
		}

		// We should be able to apply the conf containing the new
		// image
		cluster := &apiv1.Cluster{}
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}
		Eventually(func(g Gomega) error {
			err := env.Client.Get(env.Ctx, namespacedName, cluster)
			g.Expect(err).ToNot(HaveOccurred())

			cluster.Spec.ImageName = updatedImageName
			return env.Client.Update(env.Ctx, cluster)
		}, RetryTimeout, PollingTime).Should(BeNil())

		// All the postgres containers should have the updated image
		Eventually(func() (int32, error) {
			podList, err := env.GetClusterPodList(namespace, clusterName)
			updatedPods := int32(0)
			for _, pod := range podList.Items {
				// We need to check if a pod is ready, otherwise we
				// may end up asking the status of a container that
				// doesn't exist yet
				if utils.IsPodActive(pod) && utils.IsPodReady(pod) {
					for _, data := range pod.Spec.Containers {
						if data.Name != specs.PostgresContainerName {
							continue
						}

						if data.Image == updatedImageName {
							updatedPods++
						}
					}
				}
			}
			return updatedPods, err
		}, timeout).Should(BeEquivalentTo(cluster.Spec.Instances))

		// Setting up a cluster with three pods is slow, usually 200-600s
		AssertClusterIsReady(namespace, clusterName, testTimeouts[testsUtils.ClusterIsReady], env)
	}

	// Verify that the pod name changes amount to an expected number
	AssertChangedNames := func(namespace string, clusterName string,
		originalPodNames []string, expectedUnchangedNames int,
	) {
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		matchingNames := 0
		for _, pod := range podList.Items {
			if utils.IsPodActive(pod) && utils.IsPodReady(pod) {
				for _, oldName := range originalPodNames {
					if pod.GetName() == oldName {
						matchingNames++
					}
				}
			}
		}
		Expect(matchingNames).To(BeEquivalentTo(expectedUnchangedNames))
	}

	// Verify that the pod UIDs changes are the expected number
	AssertNewPodsUID := func(namespace string, clusterName string,
		originalPodUID []types.UID, expectedUnchangedUIDs int,
	) {
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		matchingUID := 0
		for _, pod := range podList.Items {
			if utils.IsPodActive(pod) && utils.IsPodReady(pod) {
				for _, oldUID := range originalPodUID {
					if pod.GetUID() == oldUID {
						matchingUID++
					}
				}
			}
		}
		Expect(matchingUID).To(BeEquivalentTo(expectedUnchangedUIDs))
	}

	// Verify that the PVC UIDs changes are the expected number
	AssertChangedPvcUID := func(namespace string, clusterName string,
		originalPVCUID []types.UID, expectedUnchangedPvcUIDs int,
	) {
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		matchingPVC := 0
		for _, pod := range podList.Items {
			pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName

			pvc := &corev1.PersistentVolumeClaim{}
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      pvcName,
			}
			err := env.Client.Get(env.Ctx, namespacedPVCName, pvc)
			Expect(err).ToNot(HaveOccurred())
			for _, oldUID := range originalPVCUID {
				if pvc.GetUID() == oldUID {
					matchingPVC++
				}
			}
		}
		Expect(matchingPVC).To(BeEquivalentTo(expectedUnchangedPvcUIDs))
	}

	// Verify that the -rw endpoint points to the expected primary
	AssertPrimary := func(namespace string, clusterName string, expectedPrimaryIdx int) {
		podName := clusterName + "-" + strconv.Itoa(expectedPrimaryIdx)
		pod := &corev1.Pod{}
		podNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      podName,
		}
		err := env.Client.Get(env.Ctx, podNamespacedName, pod)
		Expect(err).ToNot(HaveOccurred())

		endpointName := clusterName + "-rw"
		endpointNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      endpointName,
		}
		// we give 10 seconds to the apiserver to update the endpoint
		timeout := 10
		Eventually(func() (string, error) {
			endpoint := &corev1.Endpoints{}
			err := env.Client.Get(env.Ctx, endpointNamespacedName, endpoint)
			return testsUtils.FirstEndpointIP(endpoint), err
		}, timeout).Should(BeEquivalentTo(pod.Status.PodIP))
	}

	// Verify that the IPs of the pods match the ones in the -r endpoint and
	// that the amount of pods is the expected one
	AssertReadyEndpoint := func(namespace string, clusterName string, expectedEndpoints int) {
		endpointName := clusterName + "-r"
		endpoint := &corev1.Endpoints{}
		endpointNamespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      endpointName,
		}
		err := env.Client.Get(env.Ctx, endpointNamespacedName,
			endpoint)
		Expect(err).ToNot(HaveOccurred())
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(expectedEndpoints, err).To(BeEquivalentTo(len(podList.Items)))
		matchingIP := 0
		for _, pod := range podList.Items {
			ip := pod.Status.PodIP
			for _, addr := range endpoint.Subsets[0].Addresses {
				if ip == addr.IP {
					matchingIP++
				}
			}
		}
		Expect(matchingIP).To(BeEquivalentTo(expectedEndpoints))
	}

	AssertRollingUpdate := func(namespace string, clusterName string,
		sampleFile string, expectedPrimaryIdx int,
	) {
		var originalPodNames []string
		var originalPodUID []types.UID
		var originalPVCUID []types.UID

		AssertCreateCluster(namespace, clusterName, sampleFile, env)
		// Gather the number of instances in this Cluster
		cluster := &apiv1.Cluster{}
		namespacedName := types.NamespacedName{
			Namespace: namespace,
			Name:      clusterName,
		}
		err := env.Client.Get(env.Ctx, namespacedName, cluster)
		Expect(err).ToNot(HaveOccurred())
		clusterInstances := cluster.Spec.Instances

		By("Gathering info on the current state", func() {
			originalPodNames, originalPodUID, originalPVCUID, err = gatherClusterInfo(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
		})
		By("updating the cluster definition", func() {
			AssertUpdateImage(namespace, clusterName)
		})
		// Since we're using a pvc, after the update the pods should
		// have been created with the same name using the same pvc.
		// Here we check that the names we've saved at the beginning
		// of the It are the same names of the current pods.
		By("checking that the names of the pods have not changed", func() {
			AssertChangedNames(namespace, clusterName, originalPodNames, clusterInstances)
		})
		// Even if they have the same names, they should have different
		// UIDs, as the pods are new. Here we check that the UID
		// we've saved at the beginning of the It don't match the
		// current ones.
		By("checking that the pods are new ones", func() {
			AssertNewPodsUID(namespace, clusterName, originalPodUID, 0)
		})
		// The PVC get reused, so they should have the same UID
		By("checking that the PVCs are the same", func() {
			AssertChangedPvcUID(namespace, clusterName, originalPVCUID, clusterInstances)
			AssertPvcHasLabels(namespace, clusterName)
		})
		// The operator should upgrade the primary last and the primary role
		// should go to our new TargetPrimary.
		// In case of single-instance cluster, we expect the primary to just
		// be deleted and recreated.
		By("having the current primary on the new TargetPrimary", func() {
			AssertPrimary(namespace, clusterName, expectedPrimaryIdx)
		})
		// Check that the new pods are included in the endpoint
		By("having each pod included in the -r service", func() {
			AssertReadyEndpoint(namespace, clusterName, clusterInstances)
		})
	}

	Context("Three Instances", func() {
		const namespacePrefix = "cluster-rolling-e2e-three-instances"
		const sampleFile = fixturesDir + "/rolling_updates/cluster-three-instances.yaml.template"
		const clusterName = "postgresql-three-instances"
		var namespace string
		It("can do a rolling update", func() {
			var err error
			// We set up a cluster with a previous release of the same PG major
			// The yaml has been previously generated from a template and
			// the image name has to be tagged as foo:MAJ.MIN. We'll update
			// it to foo:MAJ, representing the latest minor.
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})
			AssertRollingUpdate(namespace, clusterName, sampleFile, 2)
		})
	})

	Context("Single Instance", func() {
		const namespacePrefix = "cluster-rolling-e2e-single-instance"
		const sampleFile = fixturesDir + "/rolling_updates/cluster-single-instance.yaml.template"
		const clusterName = "postgresql-single-instance"
		var namespace string
		It("can do a rolling updates on a single instance", func() {
			var err error
			// We set up a cluster with a previous release of the same PG major
			// The yaml has been previously generated from a template and
			// the image name has to be tagged as foo:MAJ.MIN. We'll update
			// it to foo:MAJ, representing the latest minor.
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})
			AssertRollingUpdate(namespace, clusterName, sampleFile, 1)
		})
	})

	Context("primaryUpdateMethod set to restart", func() {
		const sampleFile = fixturesDir + "/rolling_updates/cluster-using-primary-update-method.yaml.template"
		var namespace, clusterName string

		It("can do rolling update", func() {
			const namespacePrefix = "cluster-rolling-with-primary-update-method"
			var err error
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				if CurrentSpecReport().Failed() {
					env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
				}
				return env.DeleteNamespace(namespace)
			})

			clusterName, err = env.GetResourceNameFromYAML(sampleFile)
			Expect(err).ToNot(HaveOccurred())
			AssertRollingUpdate(namespace, clusterName, sampleFile, 1)
		})
	})
})
