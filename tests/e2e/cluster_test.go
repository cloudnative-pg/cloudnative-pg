/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package e2e

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	clusterv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/utils"

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
				cr := &corev1.Namespace{}
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      namespace,
				}

				Eventually(func() string {
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
			By("having a Cluster with 3 masters ready", func() {
				// Setting up a cluster with three pods is slow, usually 200-300s
				timeout := 400
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				cr := &clusterv1alpha1.Cluster{}

				Eventually(func() int32 {
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
				cr := &corev1.Namespace{}
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      namespace,
				}

				Eventually(func() string {
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
			By("having a Cluster with 3 masters ready", func() {
				// Setting up a cluster with three pods is slow, usually 200-300s
				timeout := 400
				namespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      clusterName,
				}
				cr := &clusterv1alpha1.Cluster{}

				Eventually(func() int32 {
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
				cr := &clusterv1alpha1.Cluster{}
				Eventually(func() int32 {
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
				cr := &clusterv1alpha1.Cluster{}
				Eventually(func() int32 {
					if err := client.Get(ctx, namespacedName, cr); err != nil {
						Fail("Unable to get Cluster " + clusterName)
					}
					return cr.Status.ReadyInstances
				}, timeout).Should(BeEquivalentTo(3))
			})
		})
	})
})
