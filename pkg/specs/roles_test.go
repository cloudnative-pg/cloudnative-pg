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
	cluster := &apiv1.Cluster{
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

	backupOrigin := &apiv1.Backup{
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
		serviceAccount := CreateRole(RoleOptions{Cluster: cluster})
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		Expect(serviceAccount.Rules).To(HaveLen(18))
	})

	It("should contain every secret of the origin backup and backup configuration of every external cluster", func() {
		serviceAccount := CreateRole(RoleOptions{Cluster: cluster, BackupOrigin: backupOrigin})
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
		cluster *apiv1.Cluster
		backup  *apiv1.Backup
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "thisTest",
				Namespace: "default",
			},
		}
		backup = &apiv1.Backup{
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
				EndpointCA: &apiv1.SecretKeySelector{
					LocalObjectReference: apiv1.LocalObjectReference{
						Name: "backup-origin-endpoint-ca-test",
					},
					Key: "ca.crt",
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

	It("includes the backup origin's endpoint CA secret", func() {
		secrets := backupSecrets(cluster, backup)
		Expect(secrets).To(ContainElement("backup-origin-endpoint-ca-test"))
	})

	It("should contain default secrets only", func() {
		Expect(getInvolvedSecretNames(RoleOptions{Cluster: cluster})).To(Equal([]string{
			"thisTest-app",
			"thisTest-ca",
			"thisTest-replication",
			"thisTest-server",
			"thisTest-superuser",
		}))
	})

	It("should created an ordered string list with the backup secrets", func() {
		Expect(getInvolvedSecretNames(RoleOptions{Cluster: cluster, BackupOrigin: backup})).To(Equal([]string{
			"aws-status-secret-test",
			"azure-storage-key-secret-test",
			"backup-origin-endpoint-ca-test",
			"google-application-secret-test",
			"thisTest-app",
			"thisTest-ca",
			"thisTest-replication",
			"thisTest-server",
			"thisTest-superuser",
		}))
	})

	It("includes the recovery backup's direct endpoint CA secret", func() {
		cluster.Spec.Bootstrap = &apiv1.BootstrapConfiguration{
			Recovery: &apiv1.BootstrapRecovery{
				Backup: &apiv1.BackupSource{
					LocalObjectReference: apiv1.LocalObjectReference{Name: "some-backup"},
					EndpointCA: &apiv1.SecretKeySelector{
						LocalObjectReference: apiv1.LocalObjectReference{
							Name: "recovery-backup-endpoint-ca-test",
						},
						Key: "ca.crt",
					},
				},
			},
		}

		Expect(getInvolvedSecretNames(RoleOptions{Cluster: cluster})).To(
			ContainElement("recovery-backup-endpoint-ca-test"))
	})
})

var _ = Describe("Database Roles", func() {
	crdRoles := []apiv1.DatabaseRole{
		{
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					PasswordSecret: &apiv1.LocalObjectReference{
						Name: "my_secret5",
					},
					Name: "role5",
				},
			},
		},
		{
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					Name: "role6",
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "role7"},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					Name: "role7",
				},
				Password: &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "role8"},
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					Name: "role8",
				},
				Password: &apiv1.PasswordConfiguration{Mode: apiv1.PasswordModeGenerate, Secret: "my_secret8"},
			},
		},
	}
	cluster := &apiv1.Cluster{
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
		serviceAccount := CreateRole(RoleOptions{Cluster: cluster, Roles: crdRoles})
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		var secretsPolicy rbacv1.PolicyRule
		for _, policy := range serviceAccount.Rules {
			if len(policy.Resources) > 0 && policy.Resources[0] == "secrets" {
				secretsPolicy = policy
			}
		}
		// The instance manager needs to read the Secrets the operator generates
		// too, or it cannot apply the password it did not choose.
		Expect(secretsPolicy.ResourceNames).To(ContainElements(
			"my_secret1", "my_secret3", "my_secret5", "role7-password", "my_secret8"))
	})
})

var _ = Describe("CRD database role secret name", func() {
	It("should be empty when password is disabled", func() {
		role := apiv1.DatabaseRole{
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					DisablePassword: true,
					PasswordSecret:  &apiv1.LocalObjectReference{},
				},
			},
		}
		secrets := crdRoleSecretName(&role)
		Expect(secrets).To(BeEmpty())
	})
	It("should be empty when password secret is nil", func() {
		role := apiv1.DatabaseRole{
			Spec: apiv1.DatabaseRoleSpec{},
		}
		secrets := crdRoleSecretName(&role)
		Expect(secrets).To(BeEmpty())
	})
	It("should be empty when password secret name is empty", func() {
		role := apiv1.DatabaseRole{
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					PasswordSecret: &apiv1.LocalObjectReference{},
				},
			},
		}
		secrets := crdRoleSecretName(&role)
		Expect(secrets).To(BeEmpty())
	})
	It("should work properly when the password secret name is set", func() {
		role := apiv1.DatabaseRole{
			Spec: apiv1.DatabaseRoleSpec{
				RoleConfiguration: apiv1.RoleConfiguration{
					PasswordSecret: &apiv1.LocalObjectReference{
						Name: "secret-name",
					},
				},
			},
		}
		secrets := crdRoleSecretName(&role)
		Expect(secrets).To(BeIdenticalTo("secret-name"))
	})
})

var _ = Describe("Cross-Namespace RBAC", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: "postgres",
		},
		Spec: apiv1.ClusterSpec{
			Instances:                     1,
			EnableCrossNamespaceResources: true,
		},
	}
	expectedName := GetCrossNamespaceRoleName(cluster)

	Describe("GetCrossNamespaceRoleName", func() {
		It("returns a valid name embedding a hash of the cluster", func() {
			Expect(expectedName).To(MatchRegexp(`^cnpg-cross-ns-[0-9a-f]{16}$`))
			Expect(GetCrossNamespaceRoleNameFor("postgres", "my-cluster")).To(Equal(expectedName))
		})

		It("returns unique names for different clusters", func() {
			Expect(GetCrossNamespaceRoleNameFor("other-ns", "other-cluster")).NotTo(Equal(expectedName))
		})

		It("is not ambiguous when the namespace and the name contain dashes", func() {
			Expect(GetCrossNamespaceRoleNameFor("app-db", "one")).
				NotTo(Equal(GetCrossNamespaceRoleNameFor("app", "db-one")))
		})
	})

	Describe("IsCrossNamespaceRBACOwnedBy", func() {
		It("recognizes the objects created for the cluster", func() {
			labels := CreateCrossNamespaceRole(cluster).Labels
			Expect(IsCrossNamespaceRBACOwnedBy(labels, "postgres", "my-cluster")).To(BeTrue())
		})

		It("rejects the objects of another cluster or not managed by the operator", func() {
			labels := CreateCrossNamespaceRole(cluster).Labels
			Expect(IsCrossNamespaceRBACOwnedBy(labels, "postgres", "other")).To(BeFalse())
			Expect(IsCrossNamespaceRBACOwnedBy(labels, "other", "my-cluster")).To(BeFalse())
			Expect(IsCrossNamespaceRBACOwnedBy(map[string]string{
				"cnpg.io/cluster":          "my-cluster",
				"cnpg.io/clusterNamespace": "postgres",
			}, "postgres", "my-cluster")).To(BeFalse())
		})
	})

	Describe("CreateCrossNamespaceRole", func() {
		It("creates a ClusterRole with correct metadata", func() {
			role := CreateCrossNamespaceRole(cluster)

			Expect(role.Name).To(Equal(expectedName))
			Expect(role.Labels).To(HaveKeyWithValue("cnpg.io/cluster", "my-cluster"))
			Expect(role.Labels).To(HaveKeyWithValue("cnpg.io/clusterNamespace", "postgres"))
			Expect(role.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "cloudnative-pg"))
		})

		It("grants access to every resource that may refer to the cluster", func() {
			role := CreateCrossNamespaceRole(cluster)

			Expect(role.Rules).To(HaveLen(2))

			resourceRule := role.Rules[0]
			Expect(resourceRule.APIGroups).To(ConsistOf("postgresql.cnpg.io"))
			Expect(resourceRule.Resources).To(ConsistOf(
				"databases", "databaseroles", "publications", "subscriptions"))
			Expect(resourceRule.Verbs).To(ConsistOf("get", "update", "list", "watch"))

			statusRule := role.Rules[1]
			Expect(statusRule.APIGroups).To(ConsistOf("postgresql.cnpg.io"))
			Expect(statusRule.Resources).To(ConsistOf(
				"databases/status", "databaseroles/status", "publications/status", "subscriptions/status"))
			Expect(statusRule.Verbs).To(ConsistOf("get", "patch", "update"))
		})
	})

	Describe("CreateCrossNamespaceRoleBinding", func() {
		It("creates a ClusterRoleBinding with correct metadata", func() {
			binding := CreateCrossNamespaceRoleBinding(cluster)

			Expect(binding.Name).To(Equal(expectedName))
			Expect(binding.Labels).To(HaveKeyWithValue("cnpg.io/cluster", "my-cluster"))
			Expect(binding.Labels).To(HaveKeyWithValue("cnpg.io/clusterNamespace", "postgres"))
			Expect(binding.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "cloudnative-pg"))
		})

		It("binds the ServiceAccount to the ClusterRole", func() {
			binding := CreateCrossNamespaceRoleBinding(cluster)

			Expect(binding.Subjects).To(HaveLen(1))
			Expect(binding.Subjects[0].Kind).To(Equal("ServiceAccount"))
			Expect(binding.Subjects[0].Name).To(Equal("my-cluster"))
			Expect(binding.Subjects[0].Namespace).To(Equal("postgres"))

			Expect(binding.RoleRef.APIGroup).To(Equal("rbac.authorization.k8s.io"))
			Expect(binding.RoleRef.Kind).To(Equal("ClusterRole"))
			Expect(binding.RoleRef.Name).To(Equal(expectedName))
		})

		It("binds the ServiceAccount set in serviceAccountName", func() {
			sharedAccountCluster := cluster.DeepCopy()
			sharedAccountCluster.Spec.ServiceAccountName = "shared-sa"

			binding := CreateCrossNamespaceRoleBinding(*sharedAccountCluster)

			Expect(binding.Subjects).To(Equal([]rbacv1.Subject{{
				Kind: "ServiceAccount", Name: "shared-sa", Namespace: "postgres",
			}}))
		})
	})

	Describe("cross-namespace role password Secrets", func() {
		roleWithSecret := func(name, secret string, disabled bool) apiv1.DatabaseRole {
			return apiv1.DatabaseRole{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "tenant"},
				Spec: apiv1.DatabaseRoleSpec{
					RoleConfiguration: apiv1.RoleConfiguration{
						Name:            name,
						PasswordSecret:  &apiv1.LocalObjectReference{Name: secret},
						DisablePassword: disabled,
					},
				},
			}
		}

		It("lists the sorted password Secrets, skipping the roles without a password", func() {
			Expect(GetCrossNamespaceSecretNames([]apiv1.DatabaseRole{
				roleWithSecret("bob", "bob-secret", false),
				roleWithSecret("alice", "alice-secret", false),
				roleWithSecret("again", "alice-secret", false),
				roleWithSecret("carol", "carol-secret", true),
			})).To(Equal([]string{"alice-secret", "bob-secret"}))
		})

		It("creates a Role in the tenant namespace reading only the passed Secrets", func() {
			role := CreateCrossNamespaceSecretsRole(cluster, "tenant", []string{"alice-secret"})

			Expect(role.Name).To(Equal(expectedName))
			Expect(role.Namespace).To(Equal("tenant"))
			Expect(IsCrossNamespaceRBACOwnedBy(role.Labels, "postgres", "my-cluster")).To(BeTrue())
			Expect(role.Rules).To(Equal([]rbacv1.PolicyRule{{
				APIGroups:     []string{""},
				Resources:     []string{"secrets"},
				Verbs:         []string{"get", "watch"},
				ResourceNames: []string{"alice-secret"},
			}}))
		})

		It("binds the Role of the tenant namespace to the ServiceAccount of the cluster", func() {
			binding := CreateCrossNamespaceSecretsRoleBinding(cluster, "tenant")

			Expect(binding.Name).To(Equal(expectedName))
			Expect(binding.Namespace).To(Equal("tenant"))
			Expect(IsCrossNamespaceRBACOwnedBy(binding.Labels, "postgres", "my-cluster")).To(BeTrue())
			Expect(binding.Subjects).To(Equal([]rbacv1.Subject{{
				Kind: "ServiceAccount", Name: "my-cluster", Namespace: "postgres",
			}}))
			Expect(binding.RoleRef).To(Equal(rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: expectedName,
			}))
		})

		It("binds the Role of the tenant namespace to the ServiceAccount set in serviceAccountName", func() {
			sharedAccountCluster := cluster.DeepCopy()
			sharedAccountCluster.Spec.ServiceAccountName = "shared-sa"

			binding := CreateCrossNamespaceSecretsRoleBinding(*sharedAccountCluster, "tenant")

			Expect(binding.Subjects).To(Equal([]rbacv1.Subject{{
				Kind: "ServiceAccount", Name: "shared-sa", Namespace: "postgres",
			}}))
		})
	})
})
