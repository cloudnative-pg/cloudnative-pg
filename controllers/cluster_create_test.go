/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
)

var _ = Describe("cluster_create unit tests", func() {
	It("should make sure that reconcilePostgresSecrets works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		poolerSecretName := pooler.Name
		cluster.Status.PoolerIntegrations = &apiv1.PoolerIntegrations{
			PgBouncerIntegration: apiv1.PgBouncerIntegrationStatus{
				Secrets: []string{poolerSecretName},
			},
		}

		By("creating prerequisites", func() {
			generateFakeCASecret(cluster.GetClientCASecretName(), namespace, "testdomain.com")
		})

		By("executing reconcilePostgresSecrets", func() {
			err := clusterReconciler.reconcilePostgresSecrets(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the superUser secret have been created", func() {
			superUser := corev1.Secret{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetSuperuserSecretName(), Namespace: namespace},
				&superUser,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the appUserSecret have been created", func() {
			appUser := corev1.Secret{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetApplicationSecretName(), Namespace: namespace},
				&appUser,
			)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the pooler secrets have been created", func() {
			poolerSecret := corev1.Secret{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Name: poolerSecretName, Namespace: namespace},
				&poolerSecret,
			)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	It("should make sure that createPostgresServices works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)

		By("executing createPostgresServices", func() {
			err := clusterReconciler.createPostgresServices(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the services have been created", func() {
			assertResourceExists(cluster.GetServiceAnyName(), namespace, &corev1.Service{})
			assertResourceExists(cluster.GetServiceReadOnlyName(), namespace, &corev1.Service{})
			assertResourceExists(cluster.GetServiceReadWriteName(), namespace, &corev1.Service{})
			assertResourceExists(cluster.GetServiceReadName(), namespace, &corev1.Service{})
		})
	})

	It("should make sure that reconcilePodDisruptionBudget works correctly", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pdbReplicaName := specs.BuildReplicasPodDisruptionBudget(cluster).Name
		pdbPrimaryName := specs.BuildPrimaryPodDisruptionBudget(cluster).Name
		reconcilePDB := func() {
			err := clusterReconciler.reconcilePodDisruptionBudget(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		}

		By("creating the primary and replica PDB in a standard case scenario", func() {
			reconcilePDB()
		})

		By("making sure PDB exists", func() {
			assertResourceExists(
				pdbPrimaryName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
			assertResourceExists(
				pdbReplicaName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
		})

		By("enabling the cluster maintenance mode", func() {
			reusePVC := true
			cluster.Spec.NodeMaintenanceWindow = &apiv1.NodeMaintenanceWindow{
				InProgress: true,
				ReusePVC:   &reusePVC,
			}
		})

		By("reconciling pdb during the maintenance mode", func() {
			reconcilePDB()
		})

		By("making sure that the replicas PDB are deleted", func() {
			assertResourceDoesntExist(
				pdbReplicaName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
		})

		By("scaling the instances to 1 during maintenance mode", func() {
			cluster.Spec.Instances = 1
			cluster.Status.Instances = 1
		})

		By("reconciling pdb during the maintenance mode with a single node", func() {
			reconcilePDB()
		})

		By("making sure that both the replicas and main PDB are deleted", func() {
			assertResourceDoesntExist(
				pdbPrimaryName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
			assertResourceDoesntExist(
				pdbReplicaName,
				namespace,
				&policyv1beta1.PodDisruptionBudget{},
			)
		})
	})
})
