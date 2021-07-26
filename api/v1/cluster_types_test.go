/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

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
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
					},
				},
			},
		}

		Expect(cluster.ShouldCreateApplicationDatabase()).To(BeTrue())
	})

	It("will not create an application database if not requested", func() {
		Expect(Cluster{}.ShouldCreateApplicationDatabase()).To(BeFalse())
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

var _ = Describe("external server list", func() {
	cluster := Cluster{
		Spec: ClusterSpec{
			ExternalClusters: []ExternalCluster{
				{
					Name: "testServer",
					ConnectionParameters: map[string]string{
						"dbname": "test",
					},
				},
			},
		},
	}
	It("can be looked up by name", func() {
		server, ok := cluster.ExternalServer("testServer")
		Expect(ok).To(BeTrue())
		Expect(server.Name).To(Equal("testServer"))
		Expect(server.ConnectionParameters["dbname"]).To(Equal("test"))
	})
	It("fails for non existent replicas", func() {
		_, ok := cluster.ExternalServer("nonExistentServer")
		Expect(ok).To(BeFalse())
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
		Expect(len(cluster.GetClusterAltDNSNames())).To(Equal(9))
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
