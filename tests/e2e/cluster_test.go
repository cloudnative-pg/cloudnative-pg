/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package e2e

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/specs"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"

	clusterv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster", func() {

	// AssertSetup tests that the pods that should have been created by the sample
	// are there and are in ready state
	AssertCreateCluster := func(namespace string, clusterName string, sample string) {
		By(fmt.Sprintf("having a %v namespace", namespace), func() {
			// Creating a namespace should be quick
			timeout := 20
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      namespace,
			}

			Eventually(func() string {
				cr := &corev1.Namespace{}
				if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
					Fail("Unable to get namespace " + namespace)
				}
				return cr.GetName()
			}, timeout).Should(BeEquivalentTo(namespace))
		})
		By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
			_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sample)
			Expect(err).To(BeNil())
		})
		By("having a Cluster with 3 nodes ready", func() {
			// Setting up a cluster with three pods is slow, usually 200-300s
			timeout := 400
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}

			Eventually(func() (int32, error) {
				cr := &clusterv1alpha1.Cluster{}
				err := env.Client.Get(env.Ctx, namespacedName, cr)
				return cr.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(3))
		})
	}

	Context("Cluster setup", func() {

		AssertSetup := func(namespace string, clusterName string, sample string) {
			It("sets up a cluster", func() {
				AssertCreateCluster(namespace, clusterName, sample)

				By("having three PostgreSQL pods with status ready", func() {
					podList := &corev1.PodList{}
					if err := env.Client.List(
						env.Ctx, podList, ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{"postgresql": clusterName},
					); err != nil {
						Fail(fmt.Sprintf("Unable to get %v Cluster pods", clusterName))
					}
					Expect(utils.CountReadyPods(podList.Items)).Should(BeEquivalentTo(3))
				})

				By("being able to restart a killed pod without losing it", func() {
					aSecond := time.Second
					timeout := 60
					podName := clusterName + "-1"
					pod := &corev1.Pod{}
					namespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      podName,
					}
					if err := env.Client.Get(env.Ctx, namespacedName, pod); err != nil {
						Fail("Unable to get Pod " + podName)
					}

					// Put something in the database. We'll check later if it
					// still exists
					query := "CREATE TABLE test (id bigserial PRIMARY KEY, t text)"
					_, _, err := env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &aSecond,
						"psql", "-U", "postgres", "app", "-tAc", query)
					Expect(err).To(BeNil())

					// We kill the pid 1 process.
					// The pod should be restarted and the count of the restarts
					// should increase by one
					restart := int32(-1)
					for _, data := range pod.Status.ContainerStatuses {
						if data.Name == specs.PostgresContainerName {
							restart = data.RestartCount
						}
					}
					_, _, err = env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &aSecond,
						"sh", "-c", "kill 1")
					Expect(err).To(BeNil())
					Eventually(func() int32 {
						pod := &corev1.Pod{}
						if err := env.Client.Get(env.Ctx, namespacedName, pod); err != nil {
							Fail("Unable to get Pod " + podName)
						}

						for _, data := range pod.Status.ContainerStatuses {
							if data.Name == specs.PostgresContainerName {
								return data.RestartCount
							}
						}

						return int32(-1)
					}, timeout).Should(BeEquivalentTo(restart + 1))

					// That pod should also be ready
					Eventually(func() bool {
						pod := &corev1.Pod{}
						if err := env.Client.Get(env.Ctx, namespacedName, pod); err != nil {
							Fail("Unable to get Pod " + podName)
						}
						return utils.IsPodActive(*pod) && utils.IsPodReady(*pod)
					}, timeout).Should(BeTrue())

					// And it should still contain the table we created before,
					// so an empty SELECT would work
					query = "SELECT * FROM test"
					_, _, err = env.ExecCommand(env.Ctx, *pod, specs.PostgresContainerName, &aSecond,
						"psql", "-U", "postgres", "app", "-tAc", query)
					Expect(err).To(BeNil())
				})
			})
		}

		Context("Storage class", func() {
			const namespace = "cluster-storageclass-e2e"
			const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
			const clusterName = "postgresql-storage-class"
			BeforeEach(func() {
				if err := env.CreateNamespace(namespace); err != nil {
					Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
				}
			})
			AfterEach(func() {
				if err := env.DeleteNamespace(namespace); err != nil {
					Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
				}
			})
			AssertSetup(namespace, clusterName, sampleFile)
		})
	})

	Context("Cluster scale up and down", func() {
		AssertScale := func(namespace string, clusterName string) {
			// Add a node to the cluster and verify the cluster has one more
			// element
			By("adding an instance to the cluster", func() {
				_, _, err := tests.Run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName))
				Expect(err).To(BeNil())
				timeout := 200
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				Eventually(func() int32 {
					cr := &clusterv1alpha1.Cluster{}
					if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(4))
			})
			// Remove a node from the cluster and verify the cluster has one
			// element less
			By("removing an instance from the cluster", func() {
				_, _, err := tests.Run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
				Expect(err).To(BeNil())
				timeout := 30
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				Eventually(func() int32 {
					cr := &clusterv1alpha1.Cluster{}
					if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))
			})
		}
		Context("Storage Class", func() {
			const namespace = "cluster-scale-e2e-storage-class"
			const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
			const clusterName = "postgresql-storage-class"
			BeforeEach(func() {
				if err := env.CreateNamespace(namespace); err != nil {
					Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
				}
			})
			AfterEach(func() {
				if err := env.DeleteNamespace(namespace); err != nil {
					Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
				}
			})
			It("can scale the cluster size", func() {
				AssertCreateCluster(namespace, clusterName, sampleFile)
				AssertScale(namespace, clusterName)
			})
		})
	})

	Context("Failover", func() {
		const namespace = "failover-e2e"
		const sampleFile = samplesDir + "/cluster-example.yaml"
		const clusterName = "cluster-example"
		var pods []string
		var currentPrimary, targetPrimary, pausedReplica string
		BeforeEach(func() {
			if err := env.CreateNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := env.DeleteNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
			}
		})
		It("react to primary failure", func() {
			AssertCreateCluster(namespace, clusterName, sampleFile)
			// First we check that the starting situation is the expected one
			By("checking that CurrentPrimary and TargetPrimary are the same", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				cr := &clusterv1alpha1.Cluster{}
				if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
					Fail("Unable to get Cluster " + clusterName)
				}
				Expect(cr.Status.CurrentPrimary).To(BeEquivalentTo(cr.Status.TargetPrimary))
				currentPrimary = cr.Status.CurrentPrimary

				// Gather pod names
				var podList corev1.PodList
				if err := env.Client.List(env.Ctx, &podList,
					ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{specs.ClusterLabelName: clusterName},
				); err != nil {
					Fail("Unable to list Pods in Cluster " + clusterName)
				}
				Expect(len(podList.Items)).To(BeEquivalentTo(3))
				for _, p := range podList.Items {
					pods = append(pods, p.Name)
				}
				sort.Strings(pods)
				Expect(pods[0]).To(BeEquivalentTo(currentPrimary))
				pausedReplica = pods[1]
				targetPrimary = pods[2]
			})
			// We pause the replication on a standby. In this way we know that
			// this standby will be behind the other when we do some work.
			By("pausing the replication on the 2nd node of the Cluster", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      pausedReplica,
				}
				pausedPod := corev1.Pod{}
				if err := env.Client.Get(env.Ctx, namespacedName, &pausedPod); err != nil {
					Fail("Unable to get Pod " + pausedReplica)
				}
				timeout := time.Second * 2
				_, _, err := env.ExecCommand(env.Ctx, pausedPod, "postgres", &timeout,
					"psql", "-U", "postgres", "-c", "SELECT pg_wal_replay_pause()")
				Expect(err).To(BeNil())
			})
			// And now we do a checkpoint and a switch wal, so we're sure
			// the paused standby is behind
			By("generating some WAL traffic in the Cluster", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      currentPrimary,
				}
				primaryPod := corev1.Pod{}
				if err := env.Client.Get(env.Ctx, namespacedName, &primaryPod); err != nil {
					Fail("Unable to get Pod " + pausedReplica)
				}
				timeout := time.Second * 2
				_, _, err := env.ExecCommand(env.Ctx, primaryPod, "postgres", &timeout,
					"psql", "-U", "postgres", "-c", "CHECKPOINT; SELECT pg_switch_wal()")
				Expect(err).To(BeNil())
			})
			// Force-delete the primary. Eventually the cluster should elect a
			// new target primary (and we check that it's the expected one)
			By("deleting the CurrentPrimary node to trigger a failover", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				zero := int64(0)
				forceDelete := &ctrlclient.DeleteOptions{
					GracePeriodSeconds: &zero,
				}
				err := env.DeletePod(namespace, currentPrimary, forceDelete)
				Expect(err).To(BeNil())

				timeout := 30
				Eventually(func() string {
					cr := &clusterv1alpha1.Cluster{}
					if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.TargetPrimary
				}, timeout).ShouldNot(BeEquivalentTo(currentPrimary))
				cr := &clusterv1alpha1.Cluster{}
				if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
					Fail("Unable to get Cluster " + clusterName)
				}
				Expect(cr.Status.TargetPrimary).To(BeEquivalentTo(targetPrimary))
			})
			By("waiting that the TargetPrimary become also CurrentPrimary", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				timeout := 30
				Eventually(func() string {
					cr := &clusterv1alpha1.Cluster{}
					if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.CurrentPrimary
				}, timeout).Should(BeEquivalentTo(targetPrimary))
			})
		})
	})

	Context("Switchover", func() {
		const namespace = "switchover-e2e"
		const sampleFile = samplesDir + "/cluster-example.yaml"
		const clusterName = "cluster-example"
		var pods []string
		var oldPrimary, targetPrimary string
		BeforeEach(func() {
			if err := env.CreateNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := env.DeleteNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
			}
		})
		It("reacts to switchover requests", func() {
			AssertCreateCluster(namespace, clusterName, sampleFile)
			// First we check that the starting situation is the expected one
			By("checking that CurrentPrimary and TargetPrimary are the same", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				cr := &clusterv1alpha1.Cluster{}
				if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
					Fail("Unable to get Cluster " + clusterName)
				}
				Expect(cr.Status.CurrentPrimary).To(BeEquivalentTo(cr.Status.TargetPrimary))
				oldPrimary = cr.Status.CurrentPrimary

				// Gather pod names
				var podList corev1.PodList
				if err := env.Client.List(env.Ctx, &podList,
					ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{specs.ClusterLabelName: clusterName},
				); err != nil {
					Fail("Unable to list Pods in Cluster " + clusterName)
				}
				Expect(len(podList.Items)).To(BeEquivalentTo(3))
				for _, p := range podList.Items {
					pods = append(pods, p.Name)
				}
				sort.Strings(pods)
				Expect(pods[0]).To(BeEquivalentTo(oldPrimary))
				targetPrimary = pods[1]
			})
			By("setting the TargetPrimary node to trigger a switchover", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				cr := &clusterv1alpha1.Cluster{}
				if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
					Fail("Unable to get Cluster " + clusterName)
				}
				cr.Status.TargetPrimary = targetPrimary
				Expect(env.Client.Status().Update(env.Ctx, cr)).To(BeNil())
			})
			By("waiting that the TargetPrimary become also CurrentPrimary", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				timeout := 45
				Eventually(func() string {
					cr := &clusterv1alpha1.Cluster{}
					if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.CurrentPrimary
				}, timeout).Should(BeEquivalentTo(targetPrimary))
			})
			By("waiting that the old primary become ready", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      oldPrimary,
				}
				timeout := 45
				Eventually(func() bool {
					pod := corev1.Pod{}
					if err := env.Client.Get(env.Ctx, namespacedName, &pod); err != nil {
						Fail("Unable to get Pod " + oldPrimary)
					}
					return utils.IsPodActive(pod) && utils.IsPodReady(pod)
				}, timeout).Should(BeTrue())
			})
			By("waiting that the old primary become a standby", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      oldPrimary,
				}
				timeout := 45
				Eventually(func() bool {
					pod := corev1.Pod{}
					if err := env.Client.Get(env.Ctx, namespacedName, &pod); err != nil {
						Fail("Unable to get Pod " + oldPrimary)
					}
					return specs.IsPodStandby(pod)
				}, timeout).Should(BeTrue())
			})
		})
	})

	Context("Cluster rolling updates", func() {
		AssertUpdateImage := func(namespace string, clusterName string) {
			timeout := 400

			// Detect initial image name
			var initialImageName string
			podList := &corev1.PodList{}
			err := env.Client.List(
				env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"postgresql": clusterName},
			)
			Expect(err).To(BeNil())
			Expect(len(podList.Items) > 0).To(BeTrue())
			pod := podList.Items[0]
			for _, data := range pod.Spec.Containers {
				if data.Name != specs.PostgresContainerName {
					continue
				}
				initialImageName = data.Image
				break
			}
			// Update to the latest minor
			var re = regexp.MustCompile(`^(.*:\d+).*$`)
			updatedImageName := re.ReplaceAllString(initialImageName, `$1`)

			// We should be able to apply the conf containing the new
			// image
			cr := &clusterv1alpha1.Cluster{}
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			err = env.Client.Get(env.Ctx, namespacedName, cr)
			Expect(err).To(BeNil())
			cr.Spec.ImageName = updatedImageName
			err = env.Client.Update(env.Ctx, cr)
			Expect(err).To(BeNil())

			// All the postgres containers should have the updated image
			Eventually(func() (int, error) {
				podList := &corev1.PodList{}
				err := env.Client.List(
					env.Ctx, podList, ctrlclient.InNamespace(namespace),
					ctrlclient.MatchingLabels{"postgresql": clusterName},
				)
				updatedPods := 0
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
			}, timeout).Should(BeEquivalentTo(3))

			// All the pods should be ready
			Eventually(func() (int32, error) {
				cr := &clusterv1alpha1.Cluster{}
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				err := env.Client.Get(env.Ctx, namespacedName, cr)
				return cr.Status.ReadyInstances, err
			}, timeout).Should(BeEquivalentTo(3))
		}

		AssertChangedNames := func(namespace string, clusterName string,
			originalPodNames []string, expectedUnchangedNames int) {
			podList := &corev1.PodList{}
			err := env.Client.List(
				env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"postgresql": clusterName},
			)
			Expect(err).To(BeNil())
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

		AssertNewPodsUID := func(namespace string, clusterName string,
			originalPodUID []types.UID, expectedUnchangedUIDs int) {
			podList := &corev1.PodList{}
			err := env.Client.List(
				env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"postgresql": clusterName},
			)
			Expect(err).To(BeNil())
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

		AssertPrimary := func(namespace string, clusterName string, expectedPrimaryIdx int) {
			endpointName := clusterName + "-rw"
			endpointCr := &corev1.Endpoints{}
			endpointNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      endpointName,
			}
			podName := clusterName + "-" + strconv.Itoa(expectedPrimaryIdx)
			podCr := &corev1.Pod{}
			podNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      podName,
			}
			err := env.Client.Get(env.Ctx, endpointNamespacedName,
				endpointCr)
			Expect(err).To(BeNil())
			err = env.Client.Get(env.Ctx, podNamespacedName, podCr)
			Expect(err).To(BeNil())
			Expect(endpointCr.Subsets[0].Addresses[0].IP).To(
				BeEquivalentTo(podCr.Status.PodIP))
		}
		AssertReadyEndpoint := func(namespace string, clusterName string, expectedEndpoints int) {
			endpointName := clusterName + "-r"
			endpointCr := &corev1.Endpoints{}
			endpointNamespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      endpointName,
			}
			err := env.Client.Get(env.Ctx, endpointNamespacedName,
				endpointCr)
			Expect(err).To(BeNil())
			podList := &corev1.PodList{}
			err = env.Client.List(
				env.Ctx, podList, ctrlclient.InNamespace(namespace),
				ctrlclient.MatchingLabels{"postgresql": clusterName},
			)
			Expect(err).To(BeNil())
			matchingIP := 0
			for _, pod := range podList.Items {
				ip := pod.Status.PodIP
				for _, addr := range endpointCr.Subsets[0].Addresses {
					if ip == addr.IP {
						matchingIP++
					}
				}
			}
			Expect(matchingIP).To(BeEquivalentTo(expectedEndpoints))
		}

		Context("Storage Class", func() {
			const namespace = "cluster-rolling-e2e-storage-class"
			// We set up a cluster with a previous release of the same PG major
			// The yaml has been previously generated from a template and
			// the image name has to be tagged as foo:MAJ.MIN. We'll update
			// it to foo:MAJ, representing the latest minor.
			const sampleFile = fixturesDir + "/rolling_updates/cluster-storage-class.yaml"
			const clusterName = "postgresql-storage-class"
			BeforeEach(func() {
				if err := env.CreateNamespace(namespace); err != nil {
					Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
				}
			})
			AfterEach(func() {
				if err := env.DeleteNamespace(namespace); err != nil {
					Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
				}
			})
			It("can do a rolling update", func() {
				var originalPodNames []string
				var originalPodUID []types.UID
				var originalPVCUID []types.UID

				AssertCreateCluster(namespace, clusterName, sampleFile)
				By("Gathering info on the current state", func() {
					podList := &corev1.PodList{}
					if err := env.Client.List(
						env.Ctx, podList, ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{"postgresql": clusterName},
					); err != nil {
						Fail("Unable to get pods in Cluster " + clusterName)
					}
					for _, pod := range podList.Items {
						originalPodNames = append(originalPodNames, pod.GetName())
						originalPodUID = append(originalPodUID, pod.GetUID())
						pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
						pvc := &corev1.PersistentVolumeClaim{}
						namespacedPVCName := types.NamespacedName{
							Namespace: namespace,
							Name:      pvcName,
						}
						if err := env.Client.Get(env.Ctx, namespacedPVCName, pvc); err != nil {
							Fail("Unable to get pvc in Cluster " + clusterName)
						}
						originalPVCUID = append(originalPVCUID, pvc.GetUID())
					}
				})
				By("updating the cluster definition", func() {
					AssertUpdateImage(namespace, clusterName)
				})
				// Since we're using a pvc, after the update the pods should
				// have been created with the same name using the same pvc.
				// Here we check that the names we've saved at the beginning
				// of the It are the same names of the current pods.
				By("checking that the names of the pods have not changed", func() {
					AssertChangedNames(namespace, clusterName, originalPodNames, 3)
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
					podList := &corev1.PodList{}
					err := env.Client.List(
						env.Ctx, podList, ctrlclient.InNamespace(namespace),
						ctrlclient.MatchingLabels{"postgresql": clusterName},
					)
					Expect(err).To(BeNil())
					matchingPVC := 0
					for _, pod := range podList.Items {
						pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName

						pvc := &corev1.PersistentVolumeClaim{}
						namespacedPVCName := types.NamespacedName{
							Namespace: namespace,
							Name:      pvcName,
						}
						err := env.Client.Get(env.Ctx, namespacedPVCName, pvc)
						Expect(err).To(BeNil())
						for _, oldUID := range originalPVCUID {
							if pvc.GetUID() == oldUID {
								matchingPVC++
							}
						}
					}
					Expect(matchingPVC).To(BeEquivalentTo(3))
				})
				// The operator should upgrade the primary last, so the last
				// to be updated is node1, and the primary role should go
				// to node2
				By("having the current primary on node2", func() {
					AssertPrimary(namespace, clusterName, 2)
				})
				// Check that the new pods are included in the endpoint
				By("having each pod included in the -r service", func() {
					AssertReadyEndpoint(namespace, clusterName, 3)
				})
			})
		})
	})
	Context("PVC Deletion", func() {
		const namespace = "cluster-pvc-deletion"
		const sampleFile = fixturesDir + "/base/cluster-storage-class.yaml"
		const clusterName = "postgresql-storage-class"
		BeforeEach(func() {
			if err := env.CreateNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := env.DeleteNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
			}
		})
		It("correctly manages PVCs", func() {
			AssertCreateCluster(namespace, clusterName, sampleFile)
			// Reuse the same pvc after a deletion
			By("recreating a pod with the same PVC after it's deleted", func() {
				// Get a pod we want to delete
				podName := clusterName + "-3"
				pod := &corev1.Pod{}
				podNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				err := env.Client.Get(env.Ctx, podNamespacedName, pod)
				Expect(err).To(BeNil())

				// Get the UID of the pod
				pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
				pvc := &corev1.PersistentVolumeClaim{}
				namespacedPVCName := types.NamespacedName{
					Namespace: namespace,
					Name:      pvcName,
				}
				err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
				Expect(err).To(BeNil())
				originalPVCUID := pvc.GetUID()

				// Delete the pod
				_, _, err = tests.Run(fmt.Sprintf("kubectl delete -n %v pod/%v", namespace, podName))
				Expect(err).To(BeNil())

				// The pod should be back
				timeout := 120
				Eventually(func() (bool, error) {
					pod := &corev1.Pod{}
					err := env.Client.Get(env.Ctx, podNamespacedName, pod)
					return utils.IsPodActive(*pod) && utils.IsPodReady(*pod), err
				}, timeout).Should(BeTrue())

				// The pod should have the same PVC
				pod = &corev1.Pod{}
				err = env.Client.Get(env.Ctx, podNamespacedName, pod)
				Expect(err).To(BeNil())
				pvc = &corev1.PersistentVolumeClaim{}
				err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
				Expect(err).To(BeNil())
				Expect(pvc.GetUID()).To(BeEquivalentTo(originalPVCUID))
			})
			By("removing a PVC and delete the Pod", func() {
				// Get a pod we want to delete
				podName := clusterName + "-3"
				pod := &corev1.Pod{}
				podNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				err := env.Client.Get(env.Ctx, podNamespacedName, pod)
				Expect(err).To(BeNil())

				// Get the UID of the pod
				pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
				pvc := &corev1.PersistentVolumeClaim{}
				namespacedPVCName := types.NamespacedName{
					Namespace: namespace,
					Name:      pvcName,
				}
				err = env.Client.Get(env.Ctx, namespacedPVCName, pvc)
				Expect(err).To(BeNil())
				originalPVCUID := pvc.GetUID()

				// Delete the PVC, this will set the PVC as terminated
				_, _, err = tests.Run(fmt.Sprintf("kubectl delete -n %v pvc/%v --wait=false", namespace, pvcName))
				Expect(err).To(BeNil())
				// Delete the pod
				_, _, err = tests.Run(fmt.Sprintf("kubectl delete -n %v pod/%v", namespace, podName))
				Expect(err).To(BeNil())
				// A new pod should be created
				timeout := 180
				newPodName := clusterName + "-4"
				newPodNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      newPodName,
				}
				Eventually(func() (bool, error) {
					newPod := &corev1.Pod{}
					err := env.Client.Get(env.Ctx, newPodNamespacedName, newPod)
					return utils.IsPodActive(*pod) && utils.IsPodReady(*pod), err
				}, timeout).Should(BeTrue())
				// The pod should have a different PVC
				newPod := &corev1.Pod{}
				err = env.Client.Get(env.Ctx, newPodNamespacedName, newPod)
				Expect(err).To(BeNil())
				newPvcName := newPod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
				newPvc := &corev1.PersistentVolumeClaim{}
				newNamespacedPVCName := types.NamespacedName{
					Namespace: namespace,
					Name:      newPvcName,
				}
				err = env.Client.Get(env.Ctx, newNamespacedPVCName, newPvc)
				Expect(err).To(BeNil())
				Expect(newPvc.GetUID()).NotTo(BeEquivalentTo(originalPVCUID))
			})
		})
	})

	Context("Backup", func() {
		const namespace = "cluster-backup"
		const sampleFile = fixturesDir + "/backup/cluster-with-backup.yaml"
		const clusterName = "postgresql-bkp"
		BeforeEach(func() {
			if err := env.CreateNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := env.DeleteNamespace(namespace); err != nil {
				Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
			}
		})
		It("backs up the cluster", func() {

			// First we create the secrets for minio
			By("creating the cloud storage credentials", func() {
				secretFile := fixturesDir + "/backup/aws-creds.yaml"
				_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
					namespace, secretFile))
				Expect(err).To(BeNil())
			})

			By("setting up minio to hold the backups", func() {
				// Create a PVC-based deployment for the minio version
				// minio/minio:RELEASE.2020-04-23T00-58-49Z
				minioPVCFile := fixturesDir + "/backup/minio-pvc.yaml"
				minioDeploymentFile := fixturesDir +
					"/backup/minio-deployment.yaml"
				_, _, err := tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
					namespace, minioPVCFile))
				Expect(err).To(BeNil())
				_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
					namespace, minioDeploymentFile))
				Expect(err).To(BeNil())

				// Wait for the minio pod to be ready
				timeout := 300
				deploymentName := "minio"
				deploymentNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      deploymentName,
				}
				Eventually(func() (int32, error) {
					deployment := &appsv1.Deployment{}
					err := env.Client.Get(env.Ctx, deploymentNamespacedName, deployment)
					return deployment.Status.ReadyReplicas, err
				}, timeout).Should(BeEquivalentTo(1))

				// Create a minio service
				serviceFile := fixturesDir + "/backup/minio-service.yaml"
				_, _, err = tests.Run(fmt.Sprintf("kubectl apply -n %v -f %v",
					namespace, serviceFile))
				Expect(err).To(BeNil())
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly.
			By("setting up minio client pod", func() {
				clientFile := fixturesDir + "/backup/minio-client.yaml"
				_, _, err := tests.Run(fmt.Sprintf(
					"kubectl apply -n %v -f %v",
					namespace, clientFile))
				Expect(err).To(BeNil())
				timeout := 180
				mcName := "mc"
				mcNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      mcName,
				}
				Eventually(func() (bool, error) {
					mc := &corev1.Pod{}
					err := env.Client.Get(env.Ctx, mcNamespacedName, mc)
					return utils.IsPodReady(*mc), err
				}, timeout).Should(BeTrue())
			})

			// Create the Cluster
			AssertCreateCluster(namespace, clusterName, sampleFile)

			// Create a WAL on the lead-master and check if it arrives on
			// minio within a short time.
			By("archiving WALs on minio", func() {
				primary := clusterName + "-1"
				switchWalCmd := "psql -U postgres app -tAc 'CHECKPOINT; SELECT pg_walfile_name(pg_switch_wal())'"
				out, _, err := tests.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					primary,
					switchWalCmd))
				Expect(err).To(BeNil())
				latestWAL := strings.TrimSpace(out)

				mcName := "mc"
				timeout := 30
				Eventually(func() (int, error) {
					// In the fixture WALs are compressed with gzip
					findCmd := fmt.Sprintf(
						"sh -c 'mc find  minio --name %v.gz | wc -l'",
						latestWAL)
					out, _, err := tests.Run(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						mcName,
						findCmd))
					if err != nil {
						return 0, err
					}
					return strconv.Atoi(strings.Trim(out, "\n"))
				}, timeout).Should(BeEquivalentTo(1))
			})

			By("uploading a backup on minio", func() {
				// We create a Backup
				backupFile := fixturesDir + "/backup/backup.yaml"
				_, _, err := tests.Run(fmt.Sprintf(
					"kubectl apply -n %v -f %v",
					namespace, backupFile))
				Expect(err).To(BeNil())

				// After a while the Backup should be completed
				timeout := 180
				backupName := "cluster-backup"
				backupNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      backupName,
				}
				Eventually(func() (clusterv1alpha1.BackupPhase, error) {
					backup := &clusterv1alpha1.Backup{}
					err := env.Client.Get(env.Ctx, backupNamespacedName, backup)
					return backup.GetStatus().Phase, err
				}, timeout).Should(BeEquivalentTo(clusterv1alpha1.BackupPhaseCompleted))

				// A file called data.tar should be available on minio
				mcName := "mc"
				timeout = 30
				Eventually(func() (int, error) {
					findCmd := "sh -c 'mc find  minio --name data.tar | wc -l'"
					out, _, err := tests.Run(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						mcName,
						findCmd))
					if err != nil {
						return 0, err
					}
					return strconv.Atoi(strings.Trim(out, "\n"))
				}, timeout).Should(BeEquivalentTo(1))
			})

			By("scheduling backups", func() {
				// We create a ScheduledBackup
				backupFile := fixturesDir + "/backup/scheduled-backup.yaml"
				_, _, err := tests.Run(fmt.Sprintf(
					"kubectl apply -n %v -f %v",
					namespace, backupFile))
				Expect(err).To(BeNil())

				// We expect the scheduled backup to be scheduled before a
				// timeout
				timeout := 180
				scheduledBackupName := "scheduled-backup"
				scheduledBackupNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      scheduledBackupName,
				}
				Eventually(func() (*v1.Time, error) {
					scheduledBackup := &clusterv1alpha1.ScheduledBackup{}
					err := env.Client.Get(env.Ctx,
						scheduledBackupNamespacedName, scheduledBackup)
					return scheduledBackup.GetStatus().LastScheduleTime, err
				}, timeout).ShouldNot(BeNil())

				// Within a few minutes we should have at least two backups
				Eventually(func() (int, error) {
					// Get all the backups children of the ScheduledBackup
					scheduledBackup := &clusterv1alpha1.ScheduledBackup{}
					if err := env.Client.Get(env.Ctx,
						scheduledBackupNamespacedName,
						scheduledBackup); err != nil {
						return 0, err
					}
					// Get all the backups children of the ScheduledBackup
					backups := &clusterv1alpha1.BackupList{}
					if err := env.Client.List(env.Ctx, backups,
						ctrlclient.InNamespace(namespace),
					); err != nil {
						return 0, err
					}
					completed := 0
					for _, backup := range backups.Items {
						for _, owner := range backup.GetObjectMeta().GetOwnerReferences() {
							if owner.Name == scheduledBackup.Name &&
								backup.GetStatus().Phase == clusterv1alpha1.BackupPhaseCompleted {
								completed++
							}
						}
					}
					return completed, nil
				}, timeout).Should(BeEquivalentTo(2))

				// Two more data.tar files should be on minio
				mcName := "mc"
				timeout = 30
				Eventually(func() (int, error) {
					findCmd := "sh -c 'mc find  minio --name data.tar | wc -l'"
					out, _, err := tests.Run(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						mcName,
						findCmd))
					if err != nil {
						return 0, err
					}
					return strconv.Atoi(strings.Trim(out, "\n"))
				}, timeout).Should(BeEquivalentTo(3))
			})
		})
	})
})
