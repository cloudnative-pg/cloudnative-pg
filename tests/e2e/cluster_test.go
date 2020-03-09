/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package e2e

import (
	"fmt"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/specs"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"

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
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get namespace " + namespace)
					}
					return cr.GetName()
				}, timeout).Should(BeEquivalentTo(namespace))
			})
			By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
				_, _, err := run("kubectl create -n " + namespace + " -f " + sample)
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
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))
			})
			By("having three PostgreSQL pods with status ready", func() {
				podList := &corev1.PodList{}
				if err := client.List(
					ctx, podList, ctrlclient.InNamespace(namespace),
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
		const clusterName = "postgresql-emptydir"
		BeforeEach(func() {
			if err := createNamespace(ctx, namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := deleteNamespace(ctx, namespace); err != nil {
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
			if err := createNamespace(ctx, namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := deleteNamespace(ctx, namespace); err != nil {
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
			if err := createNamespace(ctx, namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := deleteNamespace(ctx, namespace); err != nil {
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
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get namespace " + namespace)
					}
					return cr.GetName()
				}, timeout).Should(BeEquivalentTo(namespace))
			})
			By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
				_, _, err := run("kubectl create -n " + namespace + " -f " + sampleFile)
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
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))
			})
			By("adding a node to the cluster", func() {
				_, _, err := run(fmt.Sprintf("kubectl scale --replicas=4 -n %v cluster/%v", namespace, clusterName))
				Expect(err).To(BeNil())
				timeout := 200
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				Eventually(func() int32 {
					cr := &clusterv1alpha1.Cluster{}
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(4))
			})
			By("removing a node from the cluster", func() {
				_, _, err := run(fmt.Sprintf("kubectl scale --replicas=3 -n %v cluster/%v", namespace, clusterName))
				Expect(err).To(BeNil())
				timeout := 30
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				Eventually(func() int32 {
					cr := &clusterv1alpha1.Cluster{}
					if err := client.Get(ctx, namespacedName, cr); err != nil {
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
		const clusterName = "postgresql-emptydir"
		var pods []string
		var currentPrimary, targetPrimary, pausedReplica string
		BeforeEach(func() {
			if err := createNamespace(ctx, namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := deleteNamespace(ctx, namespace); err != nil {
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
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get namespace " + namespace)
					}
					return cr.GetName()
				}, timeout).Should(BeEquivalentTo(namespace))
			})
			By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
				_, _, err := run("kubectl create -n " + namespace + " -f " + sampleFile)
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
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))
			})
			By("checking that CurrentPrimary and TargetPrimary are the same", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				cr := &clusterv1alpha1.Cluster{}
				if err := client.Get(ctx, namespacedName, cr); err != nil {
					Fail("Unable to get Cluster " + clusterName)
				}
				Expect(cr.Status.CurrentPrimary).To(BeEquivalentTo(cr.Status.TargetPrimary))
				currentPrimary = cr.Status.CurrentPrimary

				// Gather pod names
				var podList corev1.PodList
				if err := client.List(ctx, &podList,
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
			By("pausing the replication on the 2nd node of the Cluster", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      pausedReplica,
				}
				pausedPod := corev1.Pod{}
				if err := client.Get(ctx, namespacedName, &pausedPod); err != nil {
					Fail("Unable to get Pod " + pausedReplica)
				}
				twoSeconds := time.Second * 2
				_, _, err := utils.ExecCommand(ctx, pausedPod, "postgres", &twoSeconds,
					"psql", "-c", "SELECT pg_wal_replay_pause()")
				Expect(err).To(BeNil())
			})
			By("generating some WAL traffic in the Cluster", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      currentPrimary,
				}
				primaryPod := corev1.Pod{}
				if err := client.Get(ctx, namespacedName, &primaryPod); err != nil {
					Fail("Unable to get Pod " + pausedReplica)
				}
				twoSeconds := time.Second * 2
				_, _, err := utils.ExecCommand(ctx, primaryPod, "postgres", &twoSeconds,
					"psql", "-c", "CHECKPOINT; SELECT pg_switch_wal()")
				Expect(err).To(BeNil())
			})
			By("deleting the CurrentPrimary node to trigger a failover", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				// Delete the primary
				err := deletePod(ctx, namespace, currentPrimary)
				Expect(err).To(BeNil())
				timeout := 30
				var cr *clusterv1alpha1.Cluster
				Eventually(func() string {
					cr = &clusterv1alpha1.Cluster{}
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.TargetPrimary
				}, timeout).ShouldNot(BeEquivalentTo(currentPrimary))
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
					if err := client.Get(ctx, namespacedName, cr); err != nil {
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
		const clusterName = "postgresql-emptydir"
		var pods []string
		var oldPrimary, targetPrimary string
		BeforeEach(func() {
			if err := createNamespace(ctx, namespace); err != nil {
				Fail(fmt.Sprintf("Unable to create %v namespace", namespace))
			}
		})
		AfterEach(func() {
			if err := deleteNamespace(ctx, namespace); err != nil {
				Fail(fmt.Sprintf("Unable to delete %v namespace", namespace))
			}
		})
		It("react to switchover requests", func() {
			By(fmt.Sprintf("having a %v namespace", namespace), func() {
				// Creating a namespace should be quick
				timeout := 20
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      namespace,
				}

				Eventually(func() string {
					cr := &corev1.Namespace{}
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get namespace " + namespace)
					}
					return cr.GetName()
				}, timeout).Should(BeEquivalentTo(namespace))
			})
			By(fmt.Sprintf("creating a Cluster in the %v namespace", namespace), func() {
				_, _, err := run("kubectl create -n " + namespace + " -f " + sampleFile)
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
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))
			})
			By("checking that CurrentPrimary and TargetPrimary are the same", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				cr := &clusterv1alpha1.Cluster{}
				if err := client.Get(ctx, namespacedName, cr); err != nil {
					Fail("Unable to get Cluster " + clusterName)
				}
				Expect(cr.Status.CurrentPrimary).To(BeEquivalentTo(cr.Status.TargetPrimary))
				oldPrimary = cr.Status.CurrentPrimary

				// Gather pod names
				var podList corev1.PodList
				if err := client.List(ctx, &podList,
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
				if err := client.Get(ctx, namespacedName, cr); err != nil {
					Fail("Unable to get Cluster " + clusterName)
				}
				cr.Status.TargetPrimary = targetPrimary
				Expect(client.Status().Update(ctx, cr)).To(BeNil())
			})
			By("waiting that the TargetPrimary become also CurrentPrimary", func() {
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				timeout := 45
				Eventually(func() string {
					cr := &clusterv1alpha1.Cluster{}
					if err := client.Get(ctx, namespacedName, cr); err != nil {
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
					if err := client.Get(ctx, namespacedName, &pod); err != nil {
						Fail("Unable to get Pod " + oldPrimary)
					}
					return utils.IsPodReady(pod)
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
					if err := client.Get(ctx, namespacedName, &pod); err != nil {
						Fail("Unable to get Pod " + oldPrimary)
					}
					return specs.IsPodStandby(pod)
				}, timeout).Should(BeTrue())
			})
		})
	})
})
