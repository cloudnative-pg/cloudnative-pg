/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Roles", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
	}

	It("are created with the cluster name for pure k8s", func() {
		serviceAccount := CreateRole(cluster)
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		Expect(len(serviceAccount.Rules)).To(Equal(7))
	})
})

var _ = Describe("Secrets", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thistest",
			Namespace: "default",
		},
	}

	It("are properly backed up", func() {
		secrets := backupSecrets(cluster)
		Expect(secrets).To(BeEmpty())

		cluster.Spec = apiv1.ClusterSpec{
			Backup: &apiv1.BackupConfiguration{
				BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
					S3Credentials: &apiv1.S3Credentials{
						SecretAccessKeyReference: apiv1.SecretKeySelector{
							LocalObjectReference: apiv1.LocalObjectReference{Name: "test-secret"},
						},
						AccessKeyIDReference: apiv1.SecretKeySelector{
							LocalObjectReference: apiv1.LocalObjectReference{Name: "test-access"},
						},
					},
				},
			},
		}
		secrets = backupSecrets(cluster)
		Expect(secrets[0]).To(BeEquivalentTo("test-secret"))
		Expect(secrets[1]).To(BeEquivalentTo("test-access"))
	})
})
