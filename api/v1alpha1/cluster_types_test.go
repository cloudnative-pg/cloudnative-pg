/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package v1alpha1

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL cluster type", func() {
	postgresql := Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: "clustername",
		},
	}

	It("correctly set the name of the secret of the PostgreSQL superuser", func() {
		Expect(postgresql.GetSuperuserSecretName()).To(Equal("clustername-superuser"))
	})

	It("correctly set the name of the secret of the application user", func() {
		Expect(postgresql.GetApplicationSecretName()).To(Equal("clustername-app"))
	})
})

var _ = Describe("PostgreSQL services name", func() {
	postgresql := Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: "clustername",
		},
	}

	It("has a correct service-any name", func() {
		Expect(postgresql.GetServiceAnyName()).To(Equal("clustername-any"))
	})

	It("has a correct service-read name", func() {
		Expect(postgresql.GetServiceReadName()).To(Equal("clustername-r"))
	})

	It("has a correct service-write name", func() {
		Expect(postgresql.GetServiceReadWriteName()).To(Equal("clustername-rw"))
	})
})

var _ = Describe("Detect persistent storage", func() {
	It("by defaults work with emptyDir storage", func() {
		var cluster = Cluster{}
		Expect(cluster.IsUsingPersistentStorage()).To(BeFalse())
	})

	It("consider the presence of storage configuration", func() {
		var storageClassName = "default-storage-class"
		var cluster = Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: &StorageConfiguration{
					StorageClass: &storageClassName,
				},
			},
		}
		Expect(cluster.IsUsingPersistentStorage()).To(BeTrue())
	})
})

var _ = Describe("Primary update strategy", func() {
	It("defaults to switchover", func() {
		emptyCluster := Cluster{}
		Expect(emptyCluster.GetPrimaryUpdateStrategy()).To(BeEquivalentTo(PrimaryUpdateStrategyUnsupervised))
	})

	It("respect the preference of the user", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Instances:             0,
				PrimaryUpdateStrategy: PrimaryUpdateStrategySupervised,
			},
		}
		Expect(cluster.GetPrimaryUpdateStrategy()).To(BeEquivalentTo(PrimaryUpdateStrategySupervised))
	})
})

var _ = Describe("Node maintenance window", func() {
	It("default maintenance not in progress", func() {
		cluster := Cluster{}
		Expect(cluster.IsNodeMaintenanceWindowInProgress()).To(BeFalse())
		Expect(cluster.IsNodeMaintenanceWindowReusePVC()).To(BeFalse())
	})

	It("is enabled when specified, and by default ReusePVC is enabled", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				NodeMaintenanceWindow: &NodeMaintenanceWindow{
					InProgress: true,
				},
			},
		}
		Expect(cluster.IsNodeMaintenanceWindowInProgress()).To(BeTrue())
		Expect(cluster.IsNodeMaintenanceWindowReusePVC()).To(BeTrue())
	})

	It("is enabled and you required to reuse PVC", func() {
		falseVal := false
		cluster := Cluster{
			Spec: ClusterSpec{
				NodeMaintenanceWindow: &NodeMaintenanceWindow{
					InProgress: true,
					ReusePVC:   &falseVal,
				},
			},
		}

		Expect(cluster.IsNodeMaintenanceWindowInProgress()).To(BeTrue())
		Expect(cluster.IsNodeMaintenanceWindowReusePVC()).To(BeFalse())
	})
})
