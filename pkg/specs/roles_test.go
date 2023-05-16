/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package specs

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
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
				},
			},
		},
	}

	It("are created with the cluster name for pure k8s", func() {
		serviceAccount := CreateRole(cluster, nil)
		Expect(serviceAccount.Name).To(Equal(cluster.Name))
		Expect(serviceAccount.Namespace).To(Equal(cluster.Namespace))
		Expect(len(serviceAccount.Rules)).To(Equal(7))
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
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "thisTest",
			Namespace: "default",
		},
	}

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
		Expect(secrets).To(ConsistOf("test-secret", "test-access", "test-endpoint-ca-name"))
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
		var secretsPolicy v1.PolicyRule
		for _, policy := range serviceAccount.Rules {
			if len(policy.Resources) > 0 && policy.Resources[0] == "secrets" {
				secretsPolicy = policy
			}
		}
		Expect(secretsPolicy.ResourceNames).To(ContainElements("my_secret1", "my_secret3"))
	})
})
