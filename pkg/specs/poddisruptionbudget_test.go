/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("POD Disruption Budget specifications", func() {
	instancesNum := int32(3)
	minAvailablePrimary := int32(1)
	replicas := instancesNum - minAvailablePrimary
	minAvailableReplicas := replicas - 1
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
		Spec: apiv1.ClusterSpec{Instances: instancesNum},
	}

	It("have the same name as the PostgreSQL cluster", func() {
		result := BuildReplicasPodDisruptionBudget(cluster)
		Expect(result.Name).To(Equal(cluster.Name))
		Expect(result.Namespace).To(Equal(cluster.Namespace))
	})

	It("require not more than one unavailable replicas", func() {
		result := BuildReplicasPodDisruptionBudget(cluster)
		Expect(result.Spec.MinAvailable.IntVal).To(Equal(minAvailableReplicas))
	})

	It("require at least one primary instance to be available at all times", func() {
		result := BuildPrimaryPodDisruptionBudget(cluster)
		Expect(result.Spec.MinAvailable.IntVal).To(Equal(minAvailablePrimary))
	})
})
