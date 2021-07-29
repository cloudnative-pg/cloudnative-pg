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

var _ = Describe("Create  primary job via recovery", func() {
	// Define a cluster
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterName",
			Namespace: "default",
		},
		Spec: apiv1.ClusterSpec{
			Bootstrap: &apiv1.BootstrapConfiguration{
				Recovery: &apiv1.BootstrapRecovery{RecoveryTarget: &apiv1.RecoveryTarget{}},
			},
		},
	}

	// Define a backup with S3 credentials
	sBackup := apiv1.Backup{
		Status: apiv1.BackupStatus{
			S3Credentials: &apiv1.S3Credentials{
				AccessKeyIDReference:     apiv1.SecretKeySelector{},
				SecretAccessKeyReference: apiv1.SecretKeySelector{},
			},
		},
	}

	// Define a backup with Azure credentials
	aBackup := apiv1.Backup{
		Status: apiv1.BackupStatus{
			AzureCredentials: &apiv1.AzureCredentials{
				ConnectionString: &apiv1.SecretKeySelector{},
				StorageAccount:   &apiv1.SecretKeySelector{},
				StorageKey:       &apiv1.SecretKeySelector{},
				StorageSasToken:  &apiv1.SecretKeySelector{},
			},
		},
	}

	It("retrieves S3 Credentials", func() {
		sJobs := CreatePrimaryJobViaRecovery(cluster, 0, &sBackup)
		Expect(sJobs.Spec.Template.Spec.Containers[0].Env[6].Name).To(BeEquivalentTo("AWS_ACCESS_KEY_ID"))
		Expect(sJobs.Spec.Template.Spec.Containers[0].Env[7].Name).To(BeEquivalentTo("AWS_SECRET_ACCESS_KEY"))
	})

	It("retrieves Azure Credentials", func() {
		aJobs := CreatePrimaryJobViaRecovery(cluster, 0, &aBackup)
		Expect(aJobs.Spec.Template.Spec.Containers[0].Env[6].Name).To(BeEquivalentTo("AZURE_STORAGE_ACCOUNT"))
		Expect(aJobs.Spec.Template.Spec.Containers[0].Env[7].Name).To(BeEquivalentTo("AZURE_STORAGE_KEY"))
	})
})
