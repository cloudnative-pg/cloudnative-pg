/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Roles", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thisTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{
			Bootstrap: &apiv1.BootstrapConfiguration{
				PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
					Source: "testCluster",
					Secret: &apiv1.LocalObjectReference{
						Name: "testSecretBootstrapRecovery",
					},
				},
			},

			Certificates: &apiv1.CertificatesConfiguration{
				ServerCASecret:       "testServerCASecret",
				ServerTLSSecret:      "testServerTLSSecret",
				ReplicationTLSSecret: "testReplicationTLSSecret",
				ClientCASecret:       "testClientCASecret",
				ServerAltDNSNames:    nil,
			},

			ExternalClusters: []apiv1.ExternalCluster{
				{
					Name:                 "testCluster",
					ConnectionParameters: nil,
					SSLCert: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "testSSLCert",
						},
					},
					SSLKey: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "testSSLKey",
						},
					},
					SSLRootCert: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "testSSLRootCert",
						},
					},
					Password: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: "testPassword",
						},
					},
				},
			},
			PostgresConfiguration: apiv1.PostgresConfiguration{
				LDAP: &apiv1.LDAPConfig{
					BindSearchAuth: &apiv1.LDAPBindSearchAuth{
						BindPassword: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "testLDAPBindPasswordSecret",
							},
							Key: "key",
						},
					},
				},
			},
			Monitoring: &apiv1.MonitoringConfiguration{
				CustomQueriesConfigMap: []apiv1.ConfigMapKeySelector{
					{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testConfigMapKeySelector",
						},
						Key: "configMapKeySelector",
					},
				},
				CustomQueriesSecret: []apiv1.SecretKeySelector{
					{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testSecretKeySelector",
						},
						Key: "secretKeySelector",
					},
				},
			},
			SuperuserSecret: &apiv1.LocalObjectReference{Name: "testSuperUserSecretName"},
		},
	}

	backupOrigin := apiv1.Backup{
		Status: apiv1.BackupStatus{
			BarmanCredentials: apiv1.BarmanCredentials{
				Azure: &apiv1.AzureCredentials{
					StorageAccount: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testAzureStorageAccount",
						},
						Key: "storageAccount",
					},
					StorageKey: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testAzureStorageKey",
						},
						Key: "storageKey",
					},
					StorageSasToken: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testAzureStorageSasToken",
						},
						Key: "sasToken",
					},
				},
				AWS: &apiv1.S3Credentials{
					SecretAccessKeyReference: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testS3Secret",
						},
					},
					AccessKeyIDReference: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testS3Access",
						},
					},
					RegionReference: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testS3Region",
						},
					},
					SessionToken: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "testS3Session",
						},
					},
				},
			},
		},
	}

	It("are created with the cluster name for pure k8s", func() {
		serviceAccount := CreateRole(cluster, nil)
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		Expect(serviceAccount.Rules).To(HaveLen(15))
	})

	It("should contain every secret of the origin backup and backup configuration of every external cluster", func() {
		serviceAccount := CreateRole(cluster, &backupOrigin)
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		Expect(serviceAccount.Rules[0].ResourceNames).To(ConsistOf("thisTest", "testConfigMapKeySelector"))
		Expect(serviceAccount.Rules[1].ResourceNames).To(ConsistOf(
			"testReplicationTLSSecret",
			"testClientCASecret",
			"testServerCASecret",
			"testServerTLSSecret",
			"testSecretBootstrapRecovery",
			"testSuperUserSecretName",
			"testLDAPBindPasswordSecret",
			"testSecretKeySelector",
			"testS3Secret",
			"testS3Access",
			"testS3Region",
			"testS3Session",
			"testAzureStorageAccount",
			"testAzureStorageKey",
			"testAzureStorageSasToken",
			"testSSLCert",
			"testSSLRootCert",
			"testSSLKey",
			"testPassword",
		))
	})
})

var _ = Describe("Secrets", func() {
	var (
		cluster apiv1.Cluster
		backup  apiv1.Backup
	)

	BeforeEach(func() {
		cluster = apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "thisTest",
				Namespace: "default",
			},
		}
		backup = apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testBackup",
				Namespace: "default",
			},
			Status: apiv1.BackupStatus{
				BarmanCredentials: apiv1.BarmanCredentials{
					AWS: &apiv1.S3Credentials{
						AccessKeyIDReference: &apiv1.SecretKeySelector{
							LocalObjectReference: apiv1.LocalObjectReference{
								Name: "aws-status-secret-test",
							},
						},
					},
					Azure: &apiv1.AzureCredentials{
						StorageKey: &apiv1.SecretKeySelector{
							LocalObjectReference: apiv1.LocalObjectReference{
								Name: "azure-storage-key-secret-test",
							},
						},
					},
					Google: &apiv1.GoogleCredentials{
						ApplicationCredentials: &apiv1.SecretKeySelector{
							LocalObjectReference: apiv1.LocalObjectReference{
								Name: "google-application-secret-test",
							},
						},
					},
				},
			},
		}
	})

	It("are properly backed up", func() {
		secrets := backupSecrets(cluster, nil)
		Expect(secrets).To(BeEmpty())

		cluster.Spec = apiv1.ClusterSpec{
			Backup: &apiv1.BackupConfiguration{
				BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
					BarmanCredentials: apiv1.BarmanCredentials{
						AWS: &apiv1.S3Credentials{
							SecretAccessKeyReference: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{Name: "test-secret"},
							},
							AccessKeyIDReference: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{Name: "test-access"},
							},
							RegionReference: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{Name: "test-region"},
							},
							SessionToken: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{Name: "test-session"},
							},
						},
					},
					EndpointCA: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{Name: "test-endpoint-ca-name"},
						Key:                  "test-endpoint-ca-key",
					},
				},
			},
		}
		secrets = backupSecrets(cluster, nil)
		Expect(secrets).To(ConsistOf("test-secret", "test-access", "test-region", "test-session", "test-endpoint-ca-name"))
	})

	It("should contain default secrets only", func() {
		Expect(getInvolvedSecretNames(cluster, nil)).To(Equal([]string{
			"thisTest-app",
			"thisTest-ca",
			"thisTest-replication",
			"thisTest-server",
			"thisTest-superuser",
		}))
	})

	It("should created an ordered string list with the backup secrets", func() {
		Expect(getInvolvedSecretNames(cluster, &backup)).To(Equal([]string{
			"aws-status-secret-test",
			"azure-storage-key-secret-test",
			"google-application-secret-test",
			"thisTest-app",
			"thisTest-ca",
			"thisTest-replication",
			"thisTest-server",
			"thisTest-superuser",
		}))
	})
})

var _ = Describe("Managed Roles", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thisTest",
			Namespace: "default",
		},
		Spec: apiv1.ClusterSpec{
			Managed: &apiv1.ManagedConfiguration{
				Roles: []apiv1.RoleConfiguration{
					{
						Name: "role1",
						PasswordSecret: &apiv1.LocalObjectReference{
							Name: "my_secret1",
						},
					},
					{
						Name: "role2hasNoPassword",
					},
					{
						Name: "role3",
						PasswordSecret: &apiv1.LocalObjectReference{
							Name: "my_secret3",
						},
					},
					{
						Name: "role4",
						// This combination is prevented by the webhook, but it
						// can be forced. In this case, the instance manager
						// will not use this secret at all.
						PasswordSecret: &apiv1.LocalObjectReference{
							Name: "my_secret4",
						},
						DisablePassword: true,
					},
				},
			},
		},
	}

	It("gets the list of secrets needed by the managed roles", func() {
		Expect(managedRolesSecrets(cluster)).
			To(ConsistOf("my_secret1", "my_secret3"))
		serviceAccount := CreateRole(cluster, nil)
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		var secretsPolicy rbacv1.PolicyRule
		for _, policy := range serviceAccount.Rules {
			if len(policy.Resources) > 0 && policy.Resources[0] == "secrets" {
				secretsPolicy = policy
			}
		}
		Expect(secretsPolicy.ResourceNames).To(ContainElements("my_secret1", "my_secret3"))
	})
})

var _ = Describe("Cross-Namespace Database RBAC", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "postgres",
		},
		Spec: apiv1.ClusterSpec{
			Instances:                     1,
			EnableCrossNamespaceDatabases: true,
		},
	}

	Describe("GetCrossNamespaceDatabaseRoleName", func() {
		It("returns the correct naming format", func() {
			name := GetCrossNamespaceDatabaseRoleName(cluster)
			Expect(name).To(Equal("cnpg-postgres-my-cluster-cross-ns-db"))
		})

		It("returns unique names for different clusters", func() {
			cluster2 := cluster.DeepCopy()
			cluster2.Name = "other-cluster"
			cluster2.Namespace = "other-ns"

			name1 := GetCrossNamespaceDatabaseRoleName(cluster)
			name2 := GetCrossNamespaceDatabaseRoleName(*cluster2)

			Expect(name1).NotTo(Equal(name2))
			Expect(name2).To(Equal("cnpg-other-ns-other-cluster-cross-ns-db"))
		})
	})

	Describe("CreateCrossNamespaceDatabaseRole", func() {
		It("creates a ClusterRole with correct metadata", func() {
			role := CreateCrossNamespaceDatabaseRole(cluster)

			Expect(role.Name).To(Equal("cnpg-postgres-my-cluster-cross-ns-db"))
			Expect(role.Labels).To(HaveKeyWithValue("cnpg.io/cluster", "my-cluster"))
			Expect(role.Labels).To(HaveKeyWithValue("cnpg.io/clusterNamespace", "postgres"))
			Expect(role.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "cloudnative-pg"))
		})

		It("creates a ClusterRole with correct rules for databases", func() {
			role := CreateCrossNamespaceDatabaseRole(cluster)

			Expect(role.Rules).To(HaveLen(2))

			// Check databases rule
			dbRule := role.Rules[0]
			Expect(dbRule.APIGroups).To(ConsistOf("postgresql.cnpg.io"))
			Expect(dbRule.Resources).To(ConsistOf("databases"))
			Expect(dbRule.Verbs).To(ConsistOf("get", "update", "list", "watch"))

			// Check databases/status rule
			statusRule := role.Rules[1]
			Expect(statusRule.APIGroups).To(ConsistOf("postgresql.cnpg.io"))
			Expect(statusRule.Resources).To(ConsistOf("databases/status"))
			Expect(statusRule.Verbs).To(ConsistOf("get", "patch", "update"))
		})
	})

	Describe("CreateCrossNamespaceDatabaseRoleBinding", func() {
		It("creates a ClusterRoleBinding with correct metadata", func() {
			binding := CreateCrossNamespaceDatabaseRoleBinding(cluster)

			Expect(binding.Name).To(Equal("cnpg-postgres-my-cluster-cross-ns-db"))
			Expect(binding.Labels).To(HaveKeyWithValue("cnpg.io/cluster", "my-cluster"))
			Expect(binding.Labels).To(HaveKeyWithValue("cnpg.io/clusterNamespace", "postgres"))
			Expect(binding.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "cloudnative-pg"))
		})

		It("binds the ServiceAccount to the ClusterRole", func() {
			binding := CreateCrossNamespaceDatabaseRoleBinding(cluster)

			Expect(binding.Subjects).To(HaveLen(1))
			Expect(binding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(binding.Subjects[0].Name).To(Equal("my-cluster"))
			Expect(binding.Subjects[0].Namespace).To(Equal("postgres"))

			Expect(binding.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
			Expect(binding.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(binding.RoleRef.Name).To(Equal("cnpg-postgres-my-cluster-cross-ns-db"))
		})
	})
})
