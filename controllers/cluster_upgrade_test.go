/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/postgres"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pod upgrade", func() {
	cluster := apiv1.Cluster{
		Spec: apiv1.ClusterSpec{
			ImageName: "postgres:13.0",
		},
	}
	It("will not require a restart for just created Pods", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		Expect(isPodNeedingRestart(&cluster, postgres.PostgresqlStatus{Pod: *pod})).
			To(BeFalse())
	})

	It("checks when we are running a different image name", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		pod.Spec.Containers[0].Image = "postgres:13.1"
		oldImage, newImage, err := isPodNeedingUpgradedImage(&cluster, *pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(oldImage).NotTo(BeEmpty())
		Expect(newImage).NotTo(BeEmpty())
	})

	It("checks when the image name of the operator is different", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		pod.Spec.InitContainers[0].Image = pod.Spec.InitContainers[0].Image + ".1"
		oldImage, newImage, err := isPodNeedingUpgradedImage(&cluster, *pod)
		Expect(err).NotTo(HaveOccurred())
		Expect(oldImage).NotTo(BeEmpty())
		Expect(newImage).NotTo(BeEmpty())
	})

	It("checks when a restart has been scheduled on the cluster", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		clusterRestart := cluster
		clusterRestart.Annotations = make(map[string]string)
		clusterRestart.Annotations[specs.ClusterRestartAnnotationName] = "now"
		Expect(isPodNeedingRestart(&clusterRestart, postgres.PostgresqlStatus{Pod: *pod})).
			To(BeTrue())
		Expect(isPodNeedingRestart(&cluster, postgres.PostgresqlStatus{Pod: *pod})).
			To(BeFalse())
	})

	It("checks when a restart is being needed by PostgreSQL", func() {
		pod := specs.PodWithExistingStorage(cluster, 1)
		Expect(isPodNeedingRestart(&cluster, postgres.PostgresqlStatus{Pod: *pod})).
			To(BeFalse())

		Expect(isPodNeedingRestart(&cluster,
			postgres.PostgresqlStatus{
				Pod:            *pod,
				PendingRestart: true,
			})).
			To(BeTrue())
	})
})
