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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
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

	It("correctly get if the superuser is enabled", func() {
		postgresql.Spec.EnableSuperuserAccess = nil
		Expect(postgresql.GetEnableSuperuserAccess()).To(BeTrue())

		falseValue := false
		postgresql.Spec.EnableSuperuserAccess = &falseValue
		Expect(postgresql.GetEnableSuperuserAccess()).To(BeFalse())
	})

	It("correctly set the name of the secret of the application user", func() {
		Expect(postgresql.GetApplicationSecretName()).To(Equal("clustername-app"))
	})

	It("correctly set the name of the secret containing the CA of the cluster", func() {
		Expect(postgresql.GetServerCASecretName()).To(Equal("clustername-ca"))
	})

	It("correctly set the name of the secret containing the certificate for PostgreSQL", func() {
		Expect(postgresql.GetServerTLSSecretName()).To(Equal("clustername-server"))
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

	It("has a correct service-readonly name", func() {
		Expect(postgresql.GetServiceReadOnlyName()).To(Equal("clustername-ro"))
	})

	It("has a correct service-write name", func() {
		Expect(postgresql.GetServiceReadWriteName()).To(Equal("clustername-rw"))
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
		Expect(cluster.IsReusePVCEnabled()).To(BeTrue())
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
		Expect(cluster.IsReusePVCEnabled()).To(BeTrue())
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
		Expect(cluster.IsReusePVCEnabled()).To(BeFalse())
	})
})

var _ = Describe("Bootstrap via initdb", func() {
	It("will create an application database if specified", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clusterName",
			},
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "appDB",
						Owner:    "appOwner",
						Secret: &LocalObjectReference{
							Name: "appSecret",
						},
					},
				},
			},
		}

		Expect(cluster.ShouldCreateApplicationDatabase()).To(BeTrue())
		Expect(cluster.ShouldCreateApplicationSecret()).To(BeFalse())
		Expect(cluster.GetApplicationDatabaseName()).To(Equal("appDB"))
	})

	It("will run post application sql refs if specified for secrets", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clusterName",
			},
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "appDB",
						Owner:    "appOwner",
						Secret: &LocalObjectReference{
							Name: "appSecret",
						},
						PostInitApplicationSQLRefs: &PostInitApplicationSQLRefs{
							SecretRefs: []SecretKeySelector{
								{
									Key: "secretKey",
									LocalObjectReference: LocalObjectReference{
										Name: "secretName",
									},
								},
							},
						},
					},
				},
			},
		}

		Expect(cluster.ShouldInitDBRunPostInitApplicationSQLRefs()).To(BeTrue())
	})

	It("will run post application sql refs if specified for configmaps", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clusterName",
			},
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "appDB",
						Owner:    "appOwner",
						Secret: &LocalObjectReference{
							Name: "appSecret",
						},
						PostInitApplicationSQLRefs: &PostInitApplicationSQLRefs{
							ConfigMapRefs: []ConfigMapKeySelector{
								{
									Key: "configMapKey",
									LocalObjectReference: LocalObjectReference{
										Name: "configMapName",
									},
								},
							},
						},
					},
				},
			},
		}

		Expect(cluster.ShouldInitDBRunPostInitApplicationSQLRefs()).To(BeTrue())
	})

	It("will not run post application sql refs if not specified", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clusterName",
			},
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "appDB",
						Owner:    "appOwner",
						Secret: &LocalObjectReference{
							Name: "appSecret",
						},
					},
				},
			},
		}

		Expect(cluster.ShouldInitDBRunPostInitApplicationSQLRefs()).To(BeFalse())
	})

	It("will not create an application database if not requested", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clusterName",
			},
		}
		Expect(cluster.ShouldCreateApplicationDatabase()).To(BeFalse())
		Expect(cluster.ShouldCreateApplicationSecret()).To(BeFalse())

		// InitDB is the default bootstrap method, and is triggered by
		// the defaulting webhook if nothing else is specified by the user
		cluster.Default()
		Expect(cluster.ShouldCreateApplicationDatabase()).To(BeTrue())
		Expect(cluster.ShouldCreateApplicationSecret()).To(BeTrue())
	})
})

var _ = Describe("Bootstrap via pg_basebackup", func() {
	It("will create an application database if specified", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clusterName",
			},
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{
						Database: "appDB",
						Owner:    "appOwner",
						Secret: &LocalObjectReference{
							Name: "appSecret",
						},
					},
				},
			},
		}

		Expect(cluster.ShouldPgBaseBackupCreateApplicationDatabase()).To(BeTrue())
		Expect(cluster.ShouldPgBaseBackupCreateApplicationSecret()).To(BeFalse())
		Expect(cluster.GetApplicationDatabaseName()).To(Equal("appDB"))
		Expect(cluster.GetApplicationDatabaseOwner()).To(Equal("appOwner"))
	})

	It("will get default application secrets name if not specified", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clusterName",
			},
		}
		Expect(cluster.ShouldPgBaseBackupCreateApplicationDatabase()).To(BeFalse())
		Expect(cluster.ShouldPgBaseBackupCreateApplicationSecret()).To(BeFalse())
	})
})

var _ = Describe("default UID/GID", func() {
	It("will use 26/26 if not specified", func() {
		cluster := Cluster{}
		Expect(cluster.GetPostgresUID()).To(Equal(int64(26)))
		Expect(cluster.GetPostgresGID()).To(Equal(int64(26)))
	})

	It("will respect user specification", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				PostgresUID: 10,
				PostgresGID: 11,
			},
		}
		Expect(cluster.GetPostgresUID()).To(Equal(int64(10)))
		Expect(cluster.GetPostgresGID()).To(Equal(int64(11)))
	})
})

var _ = Describe("resize in use volumes", func() {
	It("is enabled by default", func() {
		cluster := Cluster{}
		Expect(cluster.ShouldResizeInUseVolumes()).To(BeTrue())
	})

	It("can be disabled if needed", func() {
		falseValue := false
		cluster := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					ResizeInUseVolumes: &falseValue,
				},
			},
		}
		Expect(cluster.ShouldResizeInUseVolumes()).To(BeFalse())
	})
})

var _ = Describe("external cluster list", func() {
	cluster := Cluster{
		Spec: ClusterSpec{
			ExternalClusters: []ExternalCluster{
				{
					Name: "testServer",
					ConnectionParameters: map[string]string{
						"dbname": "test",
					},
					BarmanObjectStore: &BarmanObjectStoreConfiguration{
						ServerName: "testServerRealName",
					},
				},
				{
					Name: "testServer2",
					ConnectionParameters: map[string]string{
						"dbname": "test",
					},
				},
			},
		},
	}
	It("can be looked up by name", func() {
		server, ok := cluster.ExternalCluster("testServer")
		Expect(ok).To(BeTrue())
		Expect(server.Name).To(Equal("testServer"))
		Expect(server.ConnectionParameters["dbname"]).To(Equal("test"))
	})
	It("fails for non existent replicas", func() {
		_, ok := cluster.ExternalCluster("nonExistentServer")
		Expect(ok).To(BeFalse())
	})
	It("return the correct server name", func() {
		server, ok := cluster.ExternalCluster("testServer")
		Expect(ok).To(BeTrue())
		Expect(server.GetServerName()).To(BeEquivalentTo("testServerRealName"), "explicit server name")
		server2, ok2 := cluster.ExternalCluster("testServer2")
		Expect(ok2).To(BeTrue())
		Expect(server2.GetServerName()).To(BeEquivalentTo("testServer2"), "default server name")
	})
})

var _ = Describe("look up for secrets", func() {
	cluster := Cluster{
		ObjectMeta: v1.ObjectMeta{
			Name: "clustername",
		},
	}
	It("retrieves client CA secret name", func() {
		Expect(cluster.GetClientCASecretName()).To(Equal("clustername-ca"))
	})
	It("retrieves server CA secret name", func() {
		Expect(cluster.GetServerCASecretName()).To(Equal("clustername-ca"))
	})
	It("retrieves replication secret name", func() {
		Expect(cluster.GetReplicationSecretName()).To(Equal("clustername-replication"))
	})
	It("retrieves replication secret name", func() {
		Expect(cluster.GetReplicationSecretName()).To(Equal("clustername-replication"))
	})
	It("retrieves all names needed to build a server CA certificate are 9", func() {
		Expect(cluster.GetClusterAltDNSNames()).To(HaveLen(9))
	})
})

var _ = Describe("A secret resource version", func() {
	It("do not contains any secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
		}
		found := cluster.UsesSecret("a-secret")
		Expect(found).To(BeFalse())
	})

	It("do not contains any metrics secret", func() {
		metrics := make(map[string]string, 1)
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				SecretsResourceVersion: SecretsResourceVersion{
					Metrics: metrics,
				},
			},
		}
		found := cluster.UsesSecret("a-secret")
		Expect(found).To(BeFalse())
	})

	It("contains the metrics secret we are looking for", func() {
		metrics := make(map[string]string, 1)
		metrics["a-secret"] = "test-version"
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				SecretsResourceVersion: SecretsResourceVersion{
					Metrics: metrics,
				},
			},
		}
		found := cluster.UsesSecret("a-secret")
		Expect(found).To(BeTrue())
	})

	It("contains the superuser secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
		}
		found := cluster.UsesSecret("clustername-superuser")
		Expect(found).To(BeTrue())
	})

	It("contains the application secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
		}
		found := cluster.UsesSecret("clustername-app")
		Expect(found).To(BeTrue())
	})

	It("contains the client ca secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				Certificates: CertificatesStatus{
					CertificatesConfiguration: CertificatesConfiguration{
						ClientCASecret: "client-ca-secret",
					},
				},
			},
		}
		found := cluster.UsesSecret("client-ca-secret")
		Expect(found).To(BeTrue())
	})

	It("contains the replication secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				Certificates: CertificatesStatus{
					CertificatesConfiguration: CertificatesConfiguration{
						ClientCASecret: "replication-secret",
					},
				},
			},
		}
		found := cluster.UsesSecret("replication-secret")
		Expect(found).To(BeTrue())
	})

	It("contains the replication secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				Certificates: CertificatesStatus{
					CertificatesConfiguration: CertificatesConfiguration{
						ReplicationTLSSecret: "replication-secret",
					},
				},
			},
		}
		found := cluster.UsesSecret("replication-secret")
		Expect(found).To(BeTrue())
	})

	It("contains the server ca secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				Certificates: CertificatesStatus{
					CertificatesConfiguration: CertificatesConfiguration{
						ServerCASecret: "server-ca-secret",
					},
				},
			},
		}
		found := cluster.UsesSecret("server-ca-secret")
		Expect(found).To(BeTrue())
	})

	It("contains the server cert secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				Certificates: CertificatesStatus{
					CertificatesConfiguration: CertificatesConfiguration{
						ServerTLSSecret: "server-cert-secret",
					},
				},
			},
		}
		found := cluster.UsesSecret("server-cert-secret")
		Expect(found).To(BeTrue())
	})

	It("contains the barman endpoint ca secret", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Spec: ClusterSpec{
				Backup: &BackupConfiguration{
					BarmanObjectStore: &BarmanObjectStoreConfiguration{
						EndpointCA: &SecretKeySelector{
							LocalObjectReference: LocalObjectReference{
								Name: "barman-endpoint-ca-secret",
							},
							Key: "ca.crt",
						},
					},
				},
			},
		}
		found := cluster.UsesSecret("barman-endpoint-ca-secret")
		Expect(found).To(BeTrue())
	})

	It("contains the secret generated by the PgBouncer integration", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Spec: ClusterSpec{},
			Status: ClusterStatus{
				PoolerIntegrations: &PoolerIntegrations{
					PgBouncerIntegration: PgBouncerIntegrationStatus{
						Secrets: []string{
							"clustername-pgbouncer-tls",
							"clustername-pgbouncer-basic",
						},
					},
				},
			},
		}

		Expect(cluster.UsesSecret("clustername-pgbouncer-tls")).To(BeTrue())
		Expect(cluster.UsesSecret("clustername-pgbouncer-basic")).To(BeTrue())
	})
})

var _ = Describe("A config map resource version", func() {
	It("do not contains any metrics configmap", func() {
		metrics := make(map[string]string, 1)
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				ConfigMapResourceVersion: ConfigMapResourceVersion{
					Metrics: metrics,
				},
			},
		}
		found := cluster.UsesConfigMap("a-configmap")
		Expect(found).To(BeFalse())
	})

	It("contains the metrics configmap we are looking for", func() {
		metrics := make(map[string]string, 1)
		metrics["a-configmap"] = "test-version"
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Status: ClusterStatus{
				ConfigMapResourceVersion: ConfigMapResourceVersion{
					Metrics: metrics,
				},
			},
		}
		found := cluster.UsesConfigMap("a-configmap")
		Expect(found).To(BeTrue())
	})
})

var _ = Describe("PostgreSQL version detection", func() {
	tests := []struct {
		imageName       string
		postgresVersion int
	}{
		{
			"ghcr.io/cloudnative-pg/postgresql:14.0",
			140000,
		},
		{
			"ghcr.io/cloudnative-pg/postgresql:13.2",
			130002,
		},
		{
			"ghcr.io/cloudnative-pg/postgresql:9.6.3",
			90603,
		},
	}

	It("correctly extract PostgreSQL versions", func() {
		cluster := Cluster{}
		for _, test := range tests {
			cluster.Spec.ImageName = test.imageName
			Expect(cluster.GetPostgresqlVersion()).To(Equal(test.postgresVersion))
		}
	})
})

var _ = Describe("Default Metrics", func() {
	It("correctly says default metrics are not disabled when no monitoring is passed", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Spec: ClusterSpec{},
		}
		Expect(cluster.Spec.Monitoring.AreDefaultQueriesDisabled()).To(BeFalse())
	})

	It("correctly says default metrics are not disabled when explicitly not disabled", func() {
		f := false
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Spec: ClusterSpec{Monitoring: &MonitoringConfiguration{DisableDefaultQueries: &f}},
		}
		Expect(cluster.Spec.Monitoring.AreDefaultQueriesDisabled()).To(BeFalse())
	})

	It("correctly says default metrics are disabled when explicitly disabled", func() {
		t := true
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Name: "clustername",
			},
			Spec: ClusterSpec{Monitoring: &MonitoringConfiguration{DisableDefaultQueries: &t}},
		}
		Expect(cluster.Spec.Monitoring.AreDefaultQueriesDisabled()).To(BeTrue())
	})
})

var _ = Describe("Barman Endpoint CA for replica cluster", func() {
	cluster1 := Cluster{}
	It("is empty if cluster is not replica", func() {
		Expect(cluster1.GetBarmanEndpointCAForReplicaCluster()).To(BeNil())
	})

	cluster2 := Cluster{
		Spec: ClusterSpec{
			ReplicaCluster: &ReplicaClusterConfiguration{
				Source:  "testSource",
				Enabled: true,
			},
		},
	}
	It("is empty if source name does not match external cluster name", func() {
		Expect(cluster2.GetBarmanEndpointCAForReplicaCluster()).To(BeNil())
	})

	cluster3 := Cluster{
		Spec: ClusterSpec{
			ExternalClusters: []ExternalCluster{
				{
					Name: "testReplica",
					ConnectionParameters: map[string]string{
						"dbname": "test",
					},
					BarmanObjectStore: &BarmanObjectStoreConfiguration{
						ServerName: "testServerRealName",
						EndpointCA: &SecretKeySelector{
							LocalObjectReference: LocalObjectReference{
								Name: "barman-endpoint-ca-secret",
							},
							Key: "ca.crt",
						},
					},
				},
			},
			ReplicaCluster: &ReplicaClusterConfiguration{
				Source:  "testReplica",
				Enabled: true,
			},
		},
	}
	It("is defined if source name matches external cluster name", func() {
		Expect(cluster3.GetBarmanEndpointCAForReplicaCluster()).To(Not(BeNil()))
	})
})

var _ = Describe("Fencing annotation", func() {
	When("one instance is fenced", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{
					utils.FencedInstanceAnnotation: "[\"one\"]",
				},
			},
		}

		It("detect when an instance is fenced", func() {
			Expect(cluster.IsInstanceFenced("one")).To(BeTrue())
		})

		It("detect when an instance is not fenced", func() {
			Expect(cluster.IsInstanceFenced("two")).To(BeFalse())
		})
	})

	When("the whole cluster is fenced", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{
					utils.FencedInstanceAnnotation: "[\"*\"]",
				},
			},
		}

		It("detect when an instance is fenced", func() {
			Expect(cluster.IsInstanceFenced("one")).To(BeTrue())
			Expect(cluster.IsInstanceFenced("two")).To(BeTrue())
			Expect(cluster.IsInstanceFenced("three")).To(BeTrue())
		})
	})

	When("the annotation doesn't exist", func() {
		cluster := Cluster{
			ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{},
			},
		}

		It("ensure no instances are fenced", func() {
			Expect(cluster.IsInstanceFenced("one")).To(BeFalse())
		})
	})
})

var _ = Describe("Barman credentials", func() {
	It("can check when they are empty", func() {
		Expect(BarmanCredentials{}.ArePopulated()).To(BeFalse())
	})

	It("can check when they are not empty", func() {
		Expect(BarmanCredentials{
			Azure: &AzureCredentials{},
		}.ArePopulated()).To(BeTrue())
	})
})

var _ = Describe("Replication slots names for instances", func() {
	It("returns an empty name when no replication slots are configured", func() {
		cluster := Cluster{}
		Expect(cluster.GetSlotNameFromInstanceName("cluster-example-1")).To(BeEmpty())

		cluster = Cluster{
			Spec: ClusterSpec{
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: nil,
					UpdateInterval:   0,
				},
			},
		}
		Expect(cluster.GetSlotNameFromInstanceName("cluster-example-1")).To(BeEmpty())
	})

	It("returns the name of the slot for an instance when they are configured", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
					UpdateInterval: 0,
				},
			},
		}
		Expect(cluster.GetSlotNameFromInstanceName("cluster-example-1")).To(Equal(
			"_cnpg_cluster_example_1"))
	})

	It("sanitizes the name of replication slots", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled:    ptr.To(true),
						SlotPrefix: "%232'test_",
					},
					UpdateInterval: 0,
				},
			},
		}
		Expect(cluster.GetSlotNameFromInstanceName("cluster-example-1")).To(Equal(
			"_232_test_cluster_example_1"))
	})
})

var _ = Describe("Managed Roles", func() {
	It("Verify default values", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Managed: &ManagedConfiguration{
					Roles: []RoleConfiguration{
						{
							Name: "test_user",
							PasswordSecret: &LocalObjectReference{
								Name: "test_user_secrets",
							},
						},
					},
				},
			},
		}
		Expect(cluster.ContainsManagedRolesConfiguration()).To(BeTrue())
		Expect(cluster.UsesSecretInManagedRoles("test_user_secrets")).To(BeTrue())
		Expect(cluster.UsesSecretInManagedRoles("test_user_secrets1")).To(BeFalse())
		Expect(cluster.Spec.Managed.Roles[0].GetRoleInherit()).To(BeTrue())
		Expect(cluster.Spec.Managed.Roles[0].GetRoleSecretsName()).To(Equal("test_user_secrets"))
	})

	It("Verifies default values when there are no managed roles", func() {
		cluster := Cluster{
			Spec: ClusterSpec{},
		}
		Expect(cluster.ContainsManagedRolesConfiguration()).To(BeFalse())
		Expect(cluster.UsesSecretInManagedRoles("test_user_secrets")).To(BeFalse())
	})
})

var _ = Describe("SeccompProfile usages", func() {
	It("return a RuntimeDefault profile by default", func() {
		cluster := Cluster{}
		runtimeProfile := &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		}
		seccompProfile := cluster.GetSeccompProfile()
		Expect(seccompProfile).To(BeEquivalentTo(runtimeProfile))
	})

	It("return the specified unconfined seccomprofile", func() {
		profile := &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeUnconfined,
		}
		cluster := Cluster{Spec: ClusterSpec{SeccompProfile: profile}}

		returnedProfile := cluster.GetSeccompProfile()
		Expect(returnedProfile).To(BeEquivalentTo(profile))
	})

	It("return a localhost profile with a path set", func() {
		profilePath := "/path/to/profile"
		profile := &corev1.SeccompProfile{
			Type:             corev1.SeccompProfileTypeLocalhost,
			LocalhostProfile: &profilePath,
		}
		cluster := Cluster{Spec: ClusterSpec{SeccompProfile: profile}}

		returnedProfile := cluster.GetSeccompProfile()
		Expect(returnedProfile).To(BeEquivalentTo(profile))
		Expect(returnedProfile.LocalhostProfile).To(BeEquivalentTo(&profilePath))
	})
})

var _ = Describe("Cluster ShouldRecoveryCreateApplicationDatabase", func() {
	var cluster *Cluster

	BeforeEach(func() {
		cluster = &Cluster{}
	})

	It("should return false if the cluster is a replica", func() {
		cluster.Spec.ReplicaCluster = &ReplicaClusterConfiguration{Enabled: true}
		result := cluster.ShouldRecoveryCreateApplicationDatabase()
		Expect(result).To(BeFalse())
	})

	It("should return false if Spec.Bootstrap is nil", func() {
		result := cluster.ShouldRecoveryCreateApplicationDatabase()
		Expect(result).To(BeFalse())
	})

	It("should return false if Spec.Bootstrap.Recovery is nil", func() {
		cluster.Spec.Bootstrap = &BootstrapConfiguration{Recovery: nil}
		result := cluster.ShouldRecoveryCreateApplicationDatabase()
		Expect(result).To(BeFalse())
	})

	It("should return true if BootstrapRecovery.Owner and BootstrapRecovery.Database are set", func() {
		cluster.Spec.Bootstrap = &BootstrapConfiguration{
			Recovery: &BootstrapRecovery{
				Owner:    "someOwner",
				Database: "someDatabase",
			},
		}
		result := cluster.ShouldRecoveryCreateApplicationDatabase()
		Expect(result).To(BeTrue())
	})

	It("should return false if none of the conditions are met", func() {
		result := cluster.ShouldRecoveryCreateApplicationDatabase()
		Expect(result).To(BeFalse())
	})
})
