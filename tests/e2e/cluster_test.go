/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package e2e

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/2ndquadrant/cloud-native-postgresql/pkg/specs"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"

	clusterv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster", func() {

	// AssertSetup tests that the pods that should have been created by the sample
	// are there and are in ready state
	AssertSetup := func(namespace string, clusterName string, sample string) {
		It("sets up a cluster", func() {
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

				Eventually(func() int32 {
					cr := &clusterv1alpha1.Cluster{}
					if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))
			})
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
		})
	}

	Context("Cluster setup using emptydir", func() {
		const namespace = "pg-emptydir-e2e"
		const sampleFile = samplesDir + "/cluster-emptydir.yaml"
		const clusterName = "cluster-emptydir"
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

	Context("Cluster setup using storage class", func() {
		const namespace = "cluster-storageclass-e2e"
		const sampleFile = samplesDir + "/cluster-storage-class.yaml"
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

	Context("Cluster scale up and down", func() {
		const namespace = "cluster-scale-e2e"
		const sampleFile = samplesDir + "/cluster-storage-class.yaml"
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
				_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sampleFile)
				Expect(err).To(BeNil())
			})
			By("having a Cluster with 3 nodes ready", func() {
				// Setting up a cluster with three pods is slow, usually 200-300s
				timeout := 400
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
			By("adding a node to the cluster", func() {
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
			By("removing a node from the cluster", func() {
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
		})
	})

	Context("Failover", func() {
		const namespace = "failover-e2e"
		const sampleFile = samplesDir + "/cluster-emptydir.yaml"
		const clusterName = "cluster-emptydir"
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
				_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sampleFile)
				Expect(err).To(BeNil())
			})
			By("having a Cluster with 3 nodes ready", func() {
				// Setting up a cluster with three pods is slow, usually 200-300s
				timeout := 400
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
				twoSeconds := time.Second * 2
				_, _, err := utils.ExecCommand(env.Ctx, pausedPod, "postgres", &twoSeconds,
					"psql", "-c", "SELECT pg_wal_replay_pause()")
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
				twoSeconds := time.Second * 2
				_, _, err := utils.ExecCommand(env.Ctx, primaryPod, "postgres", &twoSeconds,
					"psql", "-c", "CHECKPOINT; SELECT pg_switch_wal()")
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
		const sampleFile = samplesDir + "/cluster-emptydir.yaml"
		const clusterName = "cluster-emptydir"
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
				_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sampleFile)
				Expect(err).To(BeNil())
			})
			By("having a Cluster with 3 nodes ready", func() {
				// Setting up a cluster with three pods is slow, usually 200-300s
				timeout := 400
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
					return utils.IsPodActive(pod)
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
		// Tests will be shared between emptydir and pvc setups, so we
		// declare them in assertions
		AssertUpdateImage := func(namespace string, clusterName string, updatedSample string) {
			timeout := 400
			// We should be able to apply the conf containing the new
			// image
			_, _, err := tests.Run("kubectl apply -n " + namespace +
				" -f " + updatedSample)
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
					if utils.IsPodActive(pod) {
						for _, data := range pod.Status.ContainerStatuses {
							imageName := data.Image
							if data.Name != specs.PostgresContainerName {
								continue
							}

							if imageName == "docker.io/k8s/postgresql:e2e-update" {
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

		AssertSetup := func(namespace string, clusterName string, sampleFile string) {
			By(fmt.Sprintf("having a %v namespace", namespace), func() {
				// Creating a namespace should be quick
				timeout := 20
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      namespace,
				}

				Eventually(func() (string, error) {
					cr := &corev1.Namespace{}
					err := env.Client.Get(env.Ctx, namespacedName, cr)
					return cr.GetName(), err
				}, timeout).Should(BeEquivalentTo(namespace))
			})
			By(fmt.Sprintf("creating a cluster in the %v namespace", namespace), func() {
				_, _, err := tests.Run("kubectl create -n " + namespace + " -f " + sampleFile)
				Expect(err).To(BeNil())
			})
			By("having a cluster with 3 instances ready", func() {
				// Setting up a cluster with three pods is slow, usually 200-300s
				timeout := 400
				Eventually(func() int32 {
					cr := &clusterv1alpha1.Cluster{}
					namespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      clusterName,
					}
					if err := env.Client.Get(env.Ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))
			})
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
				if utils.IsPodActive(pod) {
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
				if utils.IsPodActive(pod) {
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
			const sampleFile = samplesDir + "/cluster-storage-class.yaml"
			const clusterName = "postgresql-storage-class"
			fixtureFile := fixturesDir + "/rolling-update/rolling-update-storage-class.yaml"
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

				AssertSetup(namespace, clusterName, sampleFile)
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
					AssertUpdateImage(namespace, clusterName, fixtureFile)
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
		Context("Emptydir", func() {
			const namespace = "cluster-rolling-e2e-emptydir"
			const sampleFile = samplesDir + "/cluster-emptydir.yaml"
			const clusterName = "cluster-emptydir"
			fixtureFile := fixturesDir + "/rolling-update/rolling-update-emptydir.yaml"
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

				AssertSetup(namespace, clusterName, sampleFile)
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
					}
				})
				By("updating the cluster definition", func() {
					AssertUpdateImage(namespace, clusterName, fixtureFile)
				})
				// Since we're using emptydir, pod should be created again from
				// scratch. Here we check that the names we've saved at the
				// beginning of the It are different from the current ones.
				By("checking that the names of the pods have changed", func() {
					AssertChangedNames(namespace, clusterName, originalPodNames, 0)
				})
				// Pods are new, so they have different UIDs. Here we check that
				// the UIDs we've saved at the beginning of the It don't match
				// the current ones.
				By("checking that the pods are new ones", func() {
					AssertNewPodsUID(namespace, clusterName, originalPodUID, 0)
				})
				// The operator should update the primary last, so the last
				// to be updated is node1, and the primary role should go
				// to node4
				By("having the current primary on node4", func() {
					AssertPrimary(namespace, clusterName, 4)
				})
				// Check that the new pods are included in the endpoint
				By("having each pod included in the -ready service", func() {
					AssertReadyEndpoint(namespace, clusterName, 3)
				})
			})
		})
	})
})
