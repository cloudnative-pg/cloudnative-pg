/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs/pgbouncer"
)

var _ = Describe("unit test of pooler_update reconciliation logic", func() {
	It("it should test the deployment update logic", func() {
		ctx := context.Background()
		namespace := newFakeNamespace()
		cluster := newFakeCNPCluster(namespace)
		pooler := newFakePooler(cluster)
		res := &poolerManagedResources{Deployment: nil, Cluster: cluster}

		By("making sure that the deployment doesn't already exists", func() {
			deployment := &appsv1.Deployment{}
			err := k8sClient.Get(
				ctx,
				types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace},
				deployment,
			)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		By("making sure that updateDeployment creates the deployment", func() {
			err := poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).To(BeNil())

			deployment := getPoolerDeployment(ctx, pooler)

			Expect(*deployment.Spec.Replicas).To(Equal(pooler.Spec.Instances))
		})

		By("making sure that if the pooler.spec doesn't change the deployment isn't updated", func() {
			beforeDep := getPoolerDeployment(ctx, pooler)

			err := poolerReconciler.updateDeployment(ctx, pooler, res)
			Expect(err).To(BeNil())

			afterDep := getPoolerDeployment(ctx, pooler)

			Expect(beforeDep.ResourceVersion).To(Equal(afterDep.ResourceVersion))
			Expect(beforeDep.Annotations[pgbouncer.PgbouncerPoolerSpecHash]).
				To(Equal(afterDep.Annotations[pgbouncer.PgbouncerPoolerSpecHash]))
		})

		By("making sure that the deployments gets updated if the pooler.spec changes", func() {
			const instancesNumber int32 = 3
			poolerUpdate := pooler.DeepCopy()
			poolerUpdate.Spec.Instances = instancesNumber

			beforeDep := getPoolerDeployment(ctx, poolerUpdate)

			err := poolerReconciler.updateDeployment(ctx, poolerUpdate, res)
			Expect(err).To(BeNil())

			afterDep := getPoolerDeployment(ctx, poolerUpdate)

			Expect(beforeDep.ResourceVersion).ToNot(Equal(afterDep.ResourceVersion))
			Expect(beforeDep.Annotations[pgbouncer.PgbouncerPoolerSpecHash]).
				ToNot(Equal(afterDep.Annotations[pgbouncer.PgbouncerPoolerSpecHash]))
			Expect(*afterDep.Spec.Replicas).To(Equal(instancesNumber))
		})
	})
})
