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
	"strings"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("bootstrap methods validation", func() {
	It("doesn't complain if there isn't a configuration", func() {
		emptyCluster := &Cluster{}
		result := emptyCluster.validateBootstrapMethod()
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we are using initdb", func() {
		initdbCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{},
				},
			},
		}
		result := initdbCluster.validateBootstrapMethod()
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we are using recovery", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{},
				},
			},
		}
		result := recoveryCluster.validateBootstrapMethod()
		Expect(result).To(BeEmpty())
	})

	It("complains where there are two active bootstrap methods", func() {
		invalidCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{},
					InitDB:   &BootstrapInitDB{},
				},
			},
		}
		result := invalidCluster.validateBootstrapMethod()
		Expect(result).To(HaveLen(1))
	})
})

var _ = Describe("azure credentials", func() {
	path := field.NewPath("spec", "backupConfiguration", "azureCredentials")

	It("contain only one of storage account key and SAS token", func() {
		azureCredentials := AzureCredentials{
			StorageAccount: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "storageAccount",
			},
			StorageKey: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "storageKey",
			},
			StorageSasToken: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "sasToken",
			},
		}
		Expect(azureCredentials.validateAzureCredentials(path)).ToNot(BeEmpty())

		azureCredentials = AzureCredentials{
			StorageAccount: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "storageAccount",
			},
			StorageKey:      nil,
			StorageSasToken: nil,
		}
		Expect(azureCredentials.validateAzureCredentials(path)).ToNot(BeEmpty())
	})

	It("is correct when the storage key is used", func() {
		azureCredentials := AzureCredentials{
			StorageAccount: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "storageAccount",
			},
			StorageKey: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "storageKey",
			},
			StorageSasToken: nil,
		}
		Expect(azureCredentials.validateAzureCredentials(path)).To(BeEmpty())
	})

	It("is correct when the sas token is used", func() {
		azureCredentials := AzureCredentials{
			StorageAccount: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "storageAccount",
			},
			StorageKey: nil,
			StorageSasToken: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "sasToken",
			},
		}
		Expect(azureCredentials.validateAzureCredentials(path)).To(BeEmpty())
	})

	It("is correct even if only the connection string is specified", func() {
		azureCredentials := AzureCredentials{
			ConnectionString: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "connectionString",
			},
		}
		Expect(azureCredentials.validateAzureCredentials(path)).To(BeEmpty())
	})

	It("it is not correct when the connection string is specified with other parameters", func() {
		azureCredentials := AzureCredentials{
			ConnectionString: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "connectionString",
			},
			StorageAccount: &SecretKeySelector{
				LocalObjectReference: LocalObjectReference{
					Name: "azure-config",
				},
				Key: "storageAccount",
			},
		}
		Expect(azureCredentials.validateAzureCredentials(path)).To(BeEmpty())
	})
})

var _ = Describe("certificates options validation", func() {
	It("doesn't complain if there isn't a configuration", func() {
		emptyCluster := &Cluster{}
		result := emptyCluster.validateCerts()
		Expect(result).To(BeEmpty())
	})
	It("doesn't complain if you specify some valid secret names", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Certificates: &CertificatesConfiguration{
					ServerCASecret:  "test-server-ca",
					ServerTLSSecret: "test-server-tls",
				},
			},
		}
		result := cluster.validateCerts()
		Expect(result).To(BeEmpty())
	})
	It("does complain if you specify the TLS secret and not the CA", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Certificates: &CertificatesConfiguration{
					ServerTLSSecret: "test-server-tls",
				},
			},
		}
		result := cluster.validateCerts()
		Expect(result).To(HaveLen(1))
	})
	It("does complain if you specify the TLS secret and AltDNSNames is not empty", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Certificates: &CertificatesConfiguration{
					ServerCASecret:    "test-server-ca",
					ServerTLSSecret:   "test-server-tls",
					ServerAltDNSNames: []string{"dns-name"},
				},
			},
		}
		result := cluster.validateCerts()
		Expect(result).To(HaveLen(1))
	})
})

var _ = Describe("initdb options validation", func() {
	It("doesn't complain if there isn't a configuration", func() {
		emptyCluster := &Cluster{}
		result := emptyCluster.validateInitDB()
		Expect(result).To(BeEmpty())
	})

	It("complains if you specify the database name but not the owner", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
					},
				},
			},
		}

		result := cluster.validateInitDB()
		Expect(result).To(HaveLen(1))
	})

	It("complains if you specify the owner but not the database name", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Owner: "app",
					},
				},
			},
		}

		result := cluster.validateInitDB()
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if you specify both database name and owner user", func() {
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

		result := cluster.validateInitDB()
		Expect(result).To(BeEmpty())
	})

	It("complain if key is missing in the secretRefs", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &PostInitApplicationSQLRefs{
							SecretRefs: []SecretKeySelector{
								{
									LocalObjectReference: LocalObjectReference{Name: "secret1"},
								},
							},
						},
					},
				},
			},
		}

		result := cluster.validateInitDB()
		Expect(result).To(HaveLen(1))
	})

	It("complain if name is missing in the secretRefs", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &PostInitApplicationSQLRefs{
							SecretRefs: []SecretKeySelector{
								{
									Key: "key",
								},
							},
						},
					},
				},
			},
		}

		result := cluster.validateInitDB()
		Expect(result).To(HaveLen(1))
	})

	It("complain if key is missing in the configMapRefs", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &PostInitApplicationSQLRefs{
							ConfigMapRefs: []ConfigMapKeySelector{
								{
									LocalObjectReference: LocalObjectReference{Name: "configmap1"},
								},
							},
						},
					},
				},
			},
		}

		result := cluster.validateInitDB()
		Expect(result).To(HaveLen(1))
	})

	It("complain if name is missing in the configMapRefs", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &PostInitApplicationSQLRefs{
							ConfigMapRefs: []ConfigMapKeySelector{
								{
									Key: "key",
								},
							},
						},
					},
				},
			},
		}

		result := cluster.validateInitDB()
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if configmapRefs and secretRefs are valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &PostInitApplicationSQLRefs{
							ConfigMapRefs: []ConfigMapKeySelector{
								{
									LocalObjectReference: LocalObjectReference{Name: "configmap1"},
									Key:                  "key",
								},
								{
									LocalObjectReference: LocalObjectReference{Name: "configmap2"},
									Key:                  "key",
								},
							},
							SecretRefs: []SecretKeySelector{
								{
									LocalObjectReference: LocalObjectReference{Name: "secret1"},
									Key:                  "key",
								},
								{
									LocalObjectReference: LocalObjectReference{Name: "secret2"},
									Key:                  "key",
								},
							},
						},
					},
				},
			},
		}

		result := cluster.validateInitDB()
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if superuser secret it's empty", func() {
		cluster := Cluster{
			Spec: ClusterSpec{},
		}

		result := cluster.validateSuperuserSecret()

		Expect(result).To(BeEmpty())
	})

	It("complains if superuser secret name it's empty", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				SuperuserSecret: &LocalObjectReference{
					Name: "",
				},
			},
		}

		result := cluster.validateSuperuserSecret()
		Expect(result).To(HaveLen(1))
	})
})

var _ = Describe("cluster configuration", func() {
	It("defaults to creating an application database", func() {
		cluster := Cluster{}
		cluster.Default()
		Expect(cluster.Spec.Bootstrap.InitDB.Database).To(Equal("app"))
		Expect(cluster.Spec.Bootstrap.InitDB.Owner).To(Equal("app"))
	})

	It("defaults the owner user with the database name", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "appdb",
					},
				},
			},
		}

		cluster.Default()
		Expect(cluster.Spec.Bootstrap.InitDB.Owner).To(Equal("appdb"))
	})

	It("defaults to create an application database if recovery is used", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{},
				},
			},
		}
		cluster.Default()
		Expect(cluster.ShouldRecoveryCreateApplicationDatabase()).Should(BeTrue())
		Expect(cluster.Spec.Bootstrap.Recovery.Database).ShouldNot(BeEmpty())
		Expect(cluster.Spec.Bootstrap.Recovery.Owner).ShouldNot(BeEmpty())
		Expect(cluster.Spec.Bootstrap.Recovery.Secret).Should(BeNil())
	})

	It("defaults the owner user with the database name for recovery", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						Database: "appdb",
					},
				},
			},
		}

		cluster.Default()
		Expect(cluster.Spec.Bootstrap.Recovery.Owner).To(Equal("appdb"))
	})

	It("defaults to create an application database if pg_basebackup is used", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{},
				},
			},
		}
		cluster.Default()
		Expect(cluster.ShouldPgBaseBackupCreateApplicationDatabase()).Should(BeTrue())
		Expect(cluster.Spec.Bootstrap.PgBaseBackup.Database).ShouldNot(BeEmpty())
		Expect(cluster.Spec.Bootstrap.PgBaseBackup.Owner).ShouldNot(BeEmpty())
		Expect(cluster.Spec.Bootstrap.PgBaseBackup.Secret).Should(BeNil())
	})

	It("defaults the owner user with the database name for pg_basebackup", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{
						Database: "appdb",
					},
				},
			},
		}

		cluster.Default()
		Expect(cluster.Spec.Bootstrap.PgBaseBackup.Owner).To(Equal("appdb"))
	})

	It("defaults the PostgreSQL configuration with parameters from the operator", func() {
		cluster := Cluster{}
		cluster.Default()
		Expect(cluster.Spec.PostgresConfiguration.Parameters).ToNot(BeEmpty())
	})

	It("defaults the anti-affinity", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{},
			},
		}
		cluster.Default()
		Expect(cluster.Spec.Affinity.PodAntiAffinityType).To(BeEquivalentTo(PodAntiAffinityTypePreferred))
		Expect(cluster.Spec.Affinity.EnablePodAntiAffinity).To(BeNil())
	})
})

var _ = Describe("ImagePullPolicy validation", func() {
	It("complains if the imagePullPolicy isn't valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImagePullPolicy: "wrong",
			},
		}

		result := cluster.validateImagePullPolicy()
		Expect(result).To(HaveLen(1))
	})
	It("does not complain if the imagePullPolicy is valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImagePullPolicy: "Always",
			},
		}

		result := cluster.validateImagePullPolicy()
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("Defaulting webhook", func() {
	It("should fill the image name if isn't already set", func() {
		cluster := Cluster{}
		cluster.Default()
		Expect(cluster.Spec.ImageName).To(Equal(configuration.Current.PostgresImageName))
	})

	It("shouldn't set the image name if already present", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "test:13",
			},
		}
		cluster.Default()
		Expect(cluster.Spec.ImageName).To(Equal("test:13"))
	})

	It("should setup the application database name", func() {
		cluster := Cluster{}
		cluster.Default()
		Expect(cluster.Spec.Bootstrap.InitDB.Database).To(Equal("app"))
		Expect(cluster.Spec.Bootstrap.InitDB.Owner).To(Equal("app"))
	})

	It("should set the owner name as the database name", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "test",
					},
				},
			},
		}
		cluster.Default()
		Expect(cluster.Spec.Bootstrap.InitDB.Database).To(Equal("test"))
		Expect(cluster.Spec.Bootstrap.InitDB.Owner).To(Equal("test"))
	})

	It("should not overwrite application database and owner settings", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "testdb",
						Owner:    "testuser",
					},
				},
			},
		}
		cluster.Default()
		Expect(cluster.Spec.Bootstrap.InitDB.Database).To(Equal("testdb"))
		Expect(cluster.Spec.Bootstrap.InitDB.Owner).To(Equal("testuser"))
	})
})

var _ = Describe("Image name validation", func() {
	It("doesn't complain if the user simply accept the default", func() {
		var cluster Cluster
		Expect(cluster.validateImageName()).To(BeEmpty())

		// Let's apply the defaulting webhook, too
		cluster.Default()
		Expect(cluster.validateImageName()).To(BeEmpty())
	})

	It("complains when the 'latest' tag is detected", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:latest",
			},
		}
		Expect(cluster.validateImageName()).To(HaveLen(1))
	})

	It("doesn't complain when a alpha tag is used", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:15alpha1",
			},
		}
		Expect(cluster.validateImageName()).To(BeEmpty())
	})

	It("doesn't complain when a beta tag is used", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:15beta1",
			},
		}
		Expect(cluster.validateImageName()).To(BeEmpty())
	})

	It("doesn't complain when a release candidate tag is used", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:15rc1",
			},
		}
		Expect(cluster.validateImageName()).To(BeEmpty())
	})

	It("complains when only the sha is passed", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres@sha256:cff94de382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866",
			},
		}
		Expect(cluster.validateImageName()).To(HaveLen(1))
	})

	It("doesn't complain if the tag is valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		Expect(cluster.validateImageName()).To(BeEmpty())
	})

	It("doesn't complain if the tag is valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:14.4-1",
			},
		}
		Expect(cluster.validateImageName()).To(BeEmpty())
	})

	It("doesn't complain if the tag is valid and has sha", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.4@sha256:cff94de382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866",
			},
		}
		Expect(cluster.validateImageName()).To(BeEmpty())
	})

	It("complain when the tag name is not a PostgreSQL version", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:test_12",
			},
		}
		Expect(cluster.validateImageName()).To(HaveLen(1))
	})
})

var _ = DescribeTable("parsePostgresQuantityValue",
	func(value string, parsedValue resource.Quantity, expectError bool) {
		quantity, err := parsePostgresQuantityValue(value)
		if !expectError {
			Expect(quantity, err).Should(BeComparableTo(parsedValue))
		} else {
			Expect(err).Should(HaveOccurred())
		}
	},
	Entry("bare", "1", resource.MustParse("1Mi"), false),
	Entry("B", "1B", resource.MustParse("1"), false),
	Entry("kB", "1kB", resource.MustParse("1Ki"), false),
	Entry("MB", "1MB", resource.MustParse("1Mi"), false),
	Entry("GB", "1GB", resource.MustParse("1Gi"), false),
	Entry("TB", "1TB", resource.MustParse("1Ti"), false),
	Entry("spaceB", "1 B", resource.MustParse("1"), false),
	Entry("spaceMB", "1 MB", resource.MustParse("1Mi"), false),
	Entry("reject kb", "1kb", resource.Quantity{}, true),
	Entry("reject Mb", "1Mb", resource.Quantity{}, true),
	Entry("reject G", "1G", resource.Quantity{}, true),
	Entry("reject random unit", "1random", resource.Quantity{}, true),
	Entry("reject non-numeric", "non-numeric", resource.Quantity{}, true),
)

var _ = Describe("configuration change validation", func() {
	It("doesn't complain when the configuration is exactly the same", func() {
		clusterOld := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		clusterNew := clusterOld
		Expect(clusterNew.validateConfigurationChange(&clusterOld)).To(BeEmpty())
	})

	It("doesn't complain when we change a setting which is not fixed", func() {
		clusterOld := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		clusterNew := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.4",
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers": "4G",
					},
				},
			},
		}
		Expect(clusterNew.validateConfigurationChange(&clusterOld)).To(BeEmpty())
	})

	It("complains when changing postgres major version and settings", func() {
		clusterOld := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		clusterNew := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.5",
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers": "4G",
					},
				},
			},
		}
		Expect(clusterNew.validateConfigurationChange(&clusterOld)).To(HaveLen(1))
	})

	It("produces no error when WAL size settings are correct", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "80MB",
						"max_wal_size": "1024",
					},
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(BeEmpty())

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "1500",
						"max_wal_size": "2 GB",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "3Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(BeEmpty())

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "1.5GB",
						"max_wal_size": "2000",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(BeEmpty())

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"max_wal_size": "1GB",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(BeEmpty())

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "100MB",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(BeEmpty())

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{},
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(BeEmpty())
	})

	It("produces one complaint when min_wal_size is bigger than max_wal_size", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "1500",
						"max_wal_size": "1GB",
					},
				},
				StorageConfiguration: StorageConfiguration{
					Size: "2Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(1))

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "2G",
						"max_wal_size": "1GB",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "4Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(1))
	})

	It("produces one complaint when max_wal_size is bigger than WAL storage", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"max_wal_size": "2GB",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "1G",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "4Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(1))

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "80MB",
						"max_wal_size": "1500",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "1G",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "4Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(1))
	})

	It("produces two complaints when min_wal_size is bigger than WAL storage and max_wal_size", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "3GB",
						"max_wal_size": "1GB",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(2))
	})

	It("complains about invalid value for min_wal_size and max_wal_size", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "xxx",
						"max_wal_size": "1GB",
					},
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(1))

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "80",
						"max_wal_size": "1Gb",
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(1))
	})

	It("doesn't compare default values for min_wal_size and max_wal_size with WalStorage", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{},
				},
				WalStorage: &StorageConfiguration{
					Size: "100Mi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(BeEmpty())

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "1.5GB", // default for max_wal_size is 1GB
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(1))

		clusterNew = Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"max_wal_size": "70M", // default for min_wal_size is 80M
					},
				},
				WalStorage: &StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: StorageConfiguration{
					Size: "4Gi",
				},
			},
		}
		Expect(clusterNew.validateConfiguration()).To(HaveLen(1))
	})

	It("should detect an invalid `shared_buffers` value", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers": "invalid",
					},
				},
			},
		}

		Expect(cluster.validateConfiguration()).To(HaveLen(1))
	})
})

var _ = Describe("validate image name change", func() {
	It("doesn't complain with no changes", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{},
		}
		Expect(clusterNew.validateImageChange("")).To(BeEmpty())
	})

	It("complains if versions are wrong", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:12.0",
			},
		}
		Expect(clusterNew.validateImageChange("12:1")).To(HaveLen(1))
	})

	It("complains if can't upgrade between mayor versions", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:11.0",
			},
		}
		Expect(clusterNew.validateImageChange("postgres:12.0")).To(HaveLen(1))
	})

	It("doesn't complain if image change it's valid", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:12.0",
			},
		}
		Expect(clusterNew.validateImageChange("postgres:12.1")).To(BeEmpty())
	})
})

var _ = Describe("recovery target", func() {
	It("is mutually exclusive", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							TargetTLI:       "",
							TargetXID:       "",
							TargetName:      "",
							TargetLSN:       "1/1",
							TargetTime:      "2021-09-01 10:22:47.000000+06",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(HaveLen(1))
	})

	It("Requires BackupID to perform PITR with TargetName", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							BackupID:        "20220616T031500",
							TargetTLI:       "",
							TargetXID:       "",
							TargetName:      "restore_point_1",
							TargetLSN:       "",
							TargetTime:      "",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(BeEmpty())
	})

	It("Fails when no BackupID is provided to perform PITR with TargetXID", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							BackupID:        "",
							TargetTLI:       "",
							TargetXID:       "1/1",
							TargetName:      "",
							TargetLSN:       "",
							TargetTime:      "",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(HaveLen(1))
	})

	It("TargetTime's format as `YYYY-MM-DD HH24:MI:SS.FF6TZH` is valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							TargetTLI:       "",
							TargetXID:       "",
							TargetName:      "",
							TargetLSN:       "",
							TargetTime:      "2021-09-01 10:22:47.000000+06",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(BeEmpty())
	})

	It("TargetTime's format as YYYY-MM-DD HH24:MI:SS.FF6TZH:TZM` is valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							TargetTLI:       "",
							TargetXID:       "",
							TargetName:      "",
							TargetLSN:       "",
							TargetTime:      "2021-09-01 10:22:47.000000+06:00",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(BeEmpty())
	})

	It("TargetTime's format as YYYY-MM-DD HH24:MI:SS.FF6 TZH:TZM` is invalid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							TargetTLI:       "",
							TargetXID:       "",
							TargetName:      "",
							TargetLSN:       "",
							TargetTime:      "2021-09-01 10:22:47.000000 +06:00",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(HaveLen(1))
	})

	It("raises errors for invalid LSN", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							TargetTLI:       "",
							TargetXID:       "",
							TargetName:      "",
							TargetLSN:       "28734982739847293874823974928738423/987429837498273498723984723",
							TargetTime:      "",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(HaveLen(1))
	})

	It("valid LSN", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							TargetTLI:       "",
							TargetXID:       "",
							TargetName:      "",
							TargetLSN:       "1/1",
							TargetTime:      "",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(BeEmpty())
	})

	It("can be specified", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							TargetTime: "2020-01-01 01:01:00",
						},
					},
				},
			},
		}

		Expect(cluster.validateRecoveryTarget()).To(BeEmpty())
	})

	When("recoveryTLI is specified", func() {
		It("allows 'latest'", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					Bootstrap: &BootstrapConfiguration{
						Recovery: &BootstrapRecovery{
							RecoveryTarget: &RecoveryTarget{
								TargetTLI: "latest",
							},
						},
					},
				},
			}
			Expect(cluster.validateRecoveryTarget()).To(BeEmpty())
		})

		It("allows a positive integer", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					Bootstrap: &BootstrapConfiguration{
						Recovery: &BootstrapRecovery{
							RecoveryTarget: &RecoveryTarget{
								TargetTLI: "23",
							},
						},
					},
				},
			}
			Expect(cluster.validateRecoveryTarget()).To(BeEmpty())
		})

		It("prevents 0 value", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					Bootstrap: &BootstrapConfiguration{
						Recovery: &BootstrapRecovery{
							RecoveryTarget: &RecoveryTarget{
								TargetTLI: "0",
							},
						},
					},
				},
			}
			Expect(cluster.validateRecoveryTarget()).To(HaveLen(1))
		})

		It("prevents negative values", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					Bootstrap: &BootstrapConfiguration{
						Recovery: &BootstrapRecovery{
							RecoveryTarget: &RecoveryTarget{
								TargetTLI: "-5",
							},
						},
					},
				},
			}
			Expect(cluster.validateRecoveryTarget()).To(HaveLen(1))
		})

		It("prevents everything else beside the empty string", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					Bootstrap: &BootstrapConfiguration{
						Recovery: &BootstrapRecovery{
							RecoveryTarget: &RecoveryTarget{
								TargetTLI: "I don't remember",
							},
						},
					},
				},
			}
			Expect(cluster.validateRecoveryTarget()).To(HaveLen(1))
		})
	})
})

var _ = Describe("primary update strategy", func() {
	It("allows 'unsupervised'", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				PrimaryUpdateStrategy: PrimaryUpdateStrategyUnsupervised,
				Instances:             3,
			},
		}
		Expect(cluster.validatePrimaryUpdateStrategy()).To(BeEmpty())
	})

	It("allows 'supervised'", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				PrimaryUpdateStrategy: PrimaryUpdateStrategySupervised,
				Instances:             3,
			},
		}
		Expect(cluster.validatePrimaryUpdateStrategy()).To(BeEmpty())
	})

	It("prevents 'supervised' for single-instance clusters", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				PrimaryUpdateStrategy: PrimaryUpdateStrategySupervised,
				Instances:             1,
			},
		}
		Expect(cluster.validatePrimaryUpdateStrategy()).ToNot(BeEmpty())
	})

	It("allows 'unsupervised' for single-instance clusters", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				PrimaryUpdateStrategy: PrimaryUpdateStrategyUnsupervised,
				Instances:             1,
			},
		}
		Expect(cluster.validatePrimaryUpdateStrategy()).To(BeEmpty())
	})

	It("prevents everything else", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				PrimaryUpdateStrategy: "maybe",
				Instances:             3,
			},
		}
		Expect(cluster.validatePrimaryUpdateStrategy()).ToNot(BeEmpty())
	})
})

var _ = Describe("Number of synchronous replicas", func() {
	It("should be a positive integer", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Instances:       3,
				MaxSyncReplicas: -3,
			},
		}
		Expect(cluster.validateMaxSyncReplicas()).ToNot(BeEmpty())
	})

	It("should not be equal than the number of replicas", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Instances:       3,
				MaxSyncReplicas: 3,
			},
		}
		Expect(cluster.validateMaxSyncReplicas()).ToNot(BeEmpty())
	})

	It("should not be greater than the number of replicas", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Instances:       3,
				MaxSyncReplicas: 5,
			},
		}
		Expect(cluster.validateMaxSyncReplicas()).ToNot(BeEmpty())
	})

	It("can be zero", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Instances:       3,
				MaxSyncReplicas: 0,
			},
		}
		Expect(cluster.validateMaxSyncReplicas()).To(BeEmpty())
	})

	It("can be lower than the number of replicas", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Instances:       3,
				MaxSyncReplicas: 2,
			},
		}
		Expect(cluster.validateMaxSyncReplicas()).To(BeEmpty())
	})
})

var _ = Describe("storage configuration validation", func() {
	It("complains if the size is being reduced", func() {
		clusterOld := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size: "1G",
				},
			},
		}

		clusterNew := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size: "512M",
				},
			},
		}

		Expect(clusterNew.validateStorageChange(&clusterOld)).ToNot(BeEmpty())
	})

	It("does not complain if nothing has been changed", func() {
		one := "one"
		clusterOld := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size:         "1G",
					StorageClass: &one,
				},
			},
		}

		clusterNew := clusterOld.DeepCopy()

		Expect(clusterNew.validateStorageChange(&clusterOld)).To(BeEmpty())
	})

	It("works fine is the size is being enlarged", func() {
		clusterOld := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size: "8G",
				},
			},
		}

		clusterNew := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size: "10G",
				},
			},
		}

		Expect(clusterNew.validateStorageChange(&clusterOld)).To(BeEmpty())
	})
})

var _ = Describe("Cluster name validation", func() {
	It("should be a valid DNS label", func() {
		cluster := Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test.one",
			},
		}
		Expect(cluster.validateName()).ToNot(BeEmpty())
	})

	It("should not be too long", func() {
		cluster := Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "abcdefghi" +
					"abcdefghi" +
					"abcdefghi" +
					"abcdefghi" +
					"abcdefghi" +
					"abcdefghi" +
					"abcdefghi" +
					"abcdefghi" +
					"abcdefghi",
			},
		}
		Expect(cluster.validateName()).ToNot(BeEmpty())
	})

	It("should not raise errors when the name is ok", func() {
		cluster := Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "abcdefghi" +
					"abcdefghi" +
					"abcdefghi" +
					"abcdefghi",
			},
		}
		Expect(cluster.validateName()).To(BeEmpty())
	})

	It("should return errors when the name is not DNS-1035 compliant", func() {
		cluster := Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "4b96d026-a956-47eb-bae8-a99b840805c3",
			},
		}
		Expect(cluster.validateName()).NotTo(BeEmpty())
	})

	It("should return errors when the name length is greater than 50", func() {
		cluster := Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("toomuchlong", 4) + "-" + "after4times",
			},
		}
		Expect(cluster.validateName()).NotTo(BeEmpty())
	})

	It("should return errors when having a name with dots", func() {
		cluster := Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "wrong.name",
			},
		}
		Expect(cluster.validateName()).NotTo(BeEmpty())
	})
})

var _ = Describe("validation of the list of external clusters", func() {
	It("is correct when it's empty", func() {
		cluster := Cluster{}
		Expect(cluster.validateExternalClusters()).To(BeEmpty())
	})

	It("complains when the list of clusters contains duplicates", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{
						Name: "one",
						ConnectionParameters: map[string]string{
							"dbname": "postgres",
						},
					},
					{
						Name: "one",
						ConnectionParameters: map[string]string{
							"dbname": "postgres",
						},
					},
				},
			},
		}
		Expect(cluster.validateExternalClusters()).ToNot(BeEmpty())
	})

	It("should not raise errors is the cluster name is unique", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{
						Name: "one",
						ConnectionParameters: map[string]string{
							"dbname": "postgres",
						},
					},
					{
						Name: "two",
						ConnectionParameters: map[string]string{
							"dbname": "postgres",
						},
					},
				},
			},
		}
		Expect(cluster.validateExternalClusters()).To(BeEmpty())
	})
})

var _ = Describe("validation of an external cluster", func() {
	It("ensure that one of connectionParameters and barmanObjectStore is set", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{},
				},
			},
		}
		Expect(cluster.validateExternalClusters()).To(Not(BeEmpty()))

		cluster.Spec.ExternalClusters[0].ConnectionParameters = map[string]string{
			"dbname": "postgres",
		}
		cluster.Spec.ExternalClusters[0].BarmanObjectStore = nil
		Expect(cluster.validateExternalClusters()).To(BeEmpty())

		cluster.Spec.ExternalClusters[0].ConnectionParameters = nil
		cluster.Spec.ExternalClusters[0].BarmanObjectStore = &BarmanObjectStoreConfiguration{}
		Expect(cluster.validateExternalClusters()).To(BeEmpty())
	})
})

var _ = Describe("bootstrap base backup validation", func() {
	It("complains if you specify the database name but not the owner for pg_basebackup", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{
						Database: "app",
					},
				},
			},
		}

		result := cluster.validatePgBaseBackupApplicationDatabase()
		Expect(result).To(HaveLen(1))
	})

	It("complains if you specify the owner but not the database name for pg_basebackup", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{
						Owner: "app",
					},
				},
			},
		}

		result := cluster.validatePgBaseBackupApplicationDatabase()
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if you specify both database name and owner user for pg_basebackup", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{
						Database: "app",
						Owner:    "app",
					},
				},
			},
		}

		result := cluster.validatePgBaseBackupApplicationDatabase()
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we are not bootstrapping using pg_basebackup", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{},
			},
		}
		result := recoveryCluster.validateBootstrapPgBaseBackupSource()
		Expect(result).To(BeEmpty())
	})

	It("complain when the source cluster doesn't exist", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{
						Source: "test",
					},
				},
			},
		}
		result := recoveryCluster.validateBootstrapPgBaseBackupSource()
		Expect(result).ToNot(BeEmpty())
	})
})

var _ = Describe("bootstrap recovery validation", func() {
	It("complains if you specify the database name but not the owner for recovery", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						Database: "app",
					},
				},
			},
		}

		result := cluster.validateRecoveryApplicationDatabase()
		Expect(result).To(HaveLen(1))
	})

	It("complains if you specify the owner but not the database name for recovery", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						Owner: "app",
					},
				},
			},
		}

		result := cluster.validateRecoveryApplicationDatabase()
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if you specify both database name and owner user for recovery", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						Database: "app",
						Owner:    "app",
					},
				},
			},
		}

		result := cluster.validateRecoveryApplicationDatabase()
		Expect(result).To(BeEmpty())
	})

	It("does not complain when bootstrap recovery source matches one of the names of external clusters", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						Source: "test",
					},
				},
				ExternalClusters: []ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		errorsList := recoveryCluster.validateBootstrapRecoverySource()
		Expect(errorsList).To(BeEmpty())
	})

	It("complains when bootstrap recovery source does not match one of the names of external clusters", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						Source: "test",
					},
				},
				ExternalClusters: []ExternalCluster{
					{
						Name: "another-test",
					},
				},
			},
		}
		errorsList := recoveryCluster.validateBootstrapRecoverySource()
		Expect(errorsList).ToNot(BeEmpty())
	})
})

var _ = Describe("toleration validation", func() {
	It("doesn't complain if we provide a proper toleration", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{
					Tolerations: []corev1.Toleration{
						{
							Key:      "test",
							Operator: "Exists",
							Effect:   "NoSchedule",
						},
					},
				},
			},
		}
		result := recoveryCluster.validateTolerations()
		Expect(result).To(BeEmpty())
	})

	It("complain when the toleration ", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{
					Tolerations: []corev1.Toleration{
						{
							Key:      "",
							Operator: "Equal",
							Effect:   "NoSchedule",
						},
					},
				},
			},
		}
		result := recoveryCluster.validateTolerations()
		Expect(result).ToNot(BeEmpty())
	})
})

var _ = Describe("validate anti-affinity", func() {
	t := true
	f := false
	It("doesn't complain if we provide an empty affinity section", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{},
			},
		}
		result := cluster.validateAntiAffinity()
		Expect(result).To(BeEmpty())
	})
	It("doesn't complain if we provide a proper PodAntiAffinity with anti-affinity enabled", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{
					EnablePodAntiAffinity: &t,
					PodAntiAffinityType:   "required",
				},
			},
		}
		result := cluster.validateAntiAffinity()
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we provide a proper PodAntiAffinity with anti-affinity disabled", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{
					EnablePodAntiAffinity: &f,
					PodAntiAffinityType:   "required",
				},
			},
		}
		result := recoveryCluster.validateAntiAffinity()
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we provide a proper PodAntiAffinity with anti-affinity enabled", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{
					EnablePodAntiAffinity: &t,
					PodAntiAffinityType:   "preferred",
				},
			},
		}
		result := recoveryCluster.validateAntiAffinity()
		Expect(result).To(BeEmpty())
	})
	It("doesn't complain if we provide a proper PodAntiAffinity default with anti-affinity enabled", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{
					EnablePodAntiAffinity: &t,
					PodAntiAffinityType:   "",
				},
			},
		}
		result := recoveryCluster.validateAntiAffinity()
		Expect(result).To(BeEmpty())
	})

	It("complains if we provide a wrong PodAntiAffinity with anti-affinity disabled", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{
					EnablePodAntiAffinity: &f,
					PodAntiAffinityType:   "error",
				},
			},
		}
		result := recoveryCluster.validateAntiAffinity()
		Expect(result).NotTo(BeEmpty())
	})

	It("complains if we provide a wrong PodAntiAffinity with anti-affinity enabled", func() {
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Affinity: AffinityConfiguration{
					EnablePodAntiAffinity: &t,
					PodAntiAffinityType:   "error",
				},
			},
		}
		result := recoveryCluster.validateAntiAffinity()
		Expect(result).NotTo(BeEmpty())
	})
})

var _ = Describe("validation of the list of external clusters", func() {
	It("is correct when it's empty", func() {
		cluster := Cluster{}
		Expect(cluster.validateExternalClusters()).To(BeEmpty())
	})

	It("complains when the list of servers contains duplicates", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{
						Name:                 "one",
						ConnectionParameters: map[string]string{},
					},
					{
						Name:                 "one",
						ConnectionParameters: map[string]string{},
					},
				},
			},
		}
		Expect(cluster.validateExternalClusters()).ToNot(BeEmpty())
	})

	It("should not raise errors is the server name is unique", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{
						Name:                 "one",
						ConnectionParameters: map[string]string{},
					},
					{
						Name:                 "two",
						ConnectionParameters: map[string]string{},
					},
				},
			},
		}
		Expect(cluster.validateExternalClusters()).To(BeEmpty())
	})
})

var _ = Describe("bootstrap base backup validation", func() {
	It("complain when the source cluster doesn't exist", func() {
		bootstrap := BootstrapConfiguration{}
		bpb := BootstrapPgBaseBackup{Source: "test"}
		bootstrap.PgBaseBackup = &bpb
		recoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{
						Source: "test",
					},
				},
			},
		}
		result := recoveryCluster.validateBootstrapPgBaseBackupSource()
		Expect(result).ToNot(BeEmpty())
	})
})

var _ = Describe("unix permissions identifiers change validation", func() {
	It("complains if the PostgresGID is changed", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{
				PostgresGID: defaultPostgresGID,
			},
		}
		cluster := &Cluster{
			Spec: ClusterSpec{
				PostgresGID: 53,
			},
		}
		Expect(cluster.validateUnixPermissionIdentifierChange(oldCluster)).NotTo(BeEmpty())
	})

	It("complains if the PostgresUID is changed", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{
				PostgresUID: defaultPostgresUID,
			},
		}
		cluster := &Cluster{
			Spec: ClusterSpec{
				PostgresGID: 74,
			},
		}
		Expect(cluster.validateUnixPermissionIdentifierChange(oldCluster)).NotTo(BeEmpty())
	})

	It("should not complain if the values havn't been changed", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{
				PostgresUID: 74,
				PostgresGID: 76,
			},
		}
		cluster := &Cluster{
			Spec: ClusterSpec{
				PostgresUID: 74,
				PostgresGID: 76,
			},
		}
		Expect(cluster.validateUnixPermissionIdentifierChange(oldCluster)).To(BeEmpty())
	})
})

var _ = Describe("replica mode validation", func() {
	It("complains if the bootstrap method is not specified", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicaCluster: &ReplicaClusterConfiguration{
					Enabled: true,
					Source:  "test",
				},
				ExternalClusters: []ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		Expect(cluster.validateReplicaMode()).ToNot(BeEmpty())
	})

	It("complains if the initdb bootstrap method is used", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicaCluster: &ReplicaClusterConfiguration{
					Enabled: true,
					Source:  "test",
				},
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{},
				},
				ExternalClusters: []ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		Expect(cluster.validateReplicaMode()).ToNot(BeEmpty())
	})

	It("is valid when the pg_basebackup bootstrap option is used", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicaCluster: &ReplicaClusterConfiguration{
					Enabled: true,
					Source:  "test",
				},
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{},
				},
				ExternalClusters: []ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		result := cluster.validateReplicaMode()
		Expect(result).To(BeEmpty())
	})

	It("is valid when the restore bootstrap option is used", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicaCluster: &ReplicaClusterConfiguration{
					Enabled: true,
					Source:  "test",
				},
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{},
				},
				ExternalClusters: []ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		result := cluster.validateReplicaMode()
		Expect(result).To(BeEmpty())
	})

	It("complains when the external cluster doesn't exist", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicaCluster: &ReplicaClusterConfiguration{
					Enabled: true,
					Source:  "test",
				},
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{},
				},
				ExternalClusters: []ExternalCluster{},
			},
		}

		cluster.Spec.Bootstrap.PgBaseBackup = nil
		result := cluster.validateReplicaMode()
		Expect(result).ToNot(BeEmpty())
	})

	It("complains when enabled on an existing cluster with no replica mode configured", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{},
		}
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicaCluster: &ReplicaClusterConfiguration{
					Enabled: true,
					Source:  "test",
				},
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{},
				},
				ExternalClusters: []ExternalCluster{
					{Name: "test"},
				},
			},
		}
		Expect(cluster.validateReplicaMode()).To(BeEmpty())
		Expect(cluster.validateReplicaModeChange(oldCluster)).ToNot(BeEmpty())
	})

	It("complains when enabled on an existing cluster with replica mode disabled", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{
				ReplicaCluster: &ReplicaClusterConfiguration{
					Enabled: false,
					Source:  "test",
				},
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{},
				},
				ExternalClusters: []ExternalCluster{
					{Name: "test"},
				},
			},
		}
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicaCluster: &ReplicaClusterConfiguration{
					Enabled: true,
					Source:  "test",
				},
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{},
				},
				ExternalClusters: []ExternalCluster{
					{Name: "test"},
				},
			},
		}
		Expect(cluster.validateReplicaMode()).To(BeEmpty())
		Expect(cluster.validateReplicaModeChange(oldCluster)).ToNot(BeEmpty())
	})
})

var _ = Describe("Validation changes", func() {
	It("doesn't complain if given old cluster is nil", func() {
		newCluster := &Cluster{}
		err := newCluster.ValidateChanges(nil)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("Backup validation", func() {
	It("complain if there's no credentials", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Backup: &BackupConfiguration{
					BarmanObjectStore: &BarmanObjectStoreConfiguration{},
				},
			},
		}
		err := cluster.validateBackupConfiguration()
		Expect(err).To(HaveLen(1))
	})

	It("doesn't complain if given policy is not provided", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Backup: &BackupConfiguration{},
			},
		}
		err := cluster.validateBackupConfiguration()
		Expect(err).To(BeNil())
	})

	It("doesn't complain if given policy is valid", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Backup: &BackupConfiguration{
					RetentionPolicy: "90d",
				},
			},
		}
		err := cluster.validateBackupConfiguration()
		Expect(err).To(BeNil())
	})

	It("complain if a given policy is not valid", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Backup: &BackupConfiguration{
					BarmanObjectStore: &BarmanObjectStoreConfiguration{},
					RetentionPolicy:   "09",
				},
			},
		}
		err := cluster.validateBackupConfiguration()
		Expect(err).To(HaveLen(2))
	})
})

var _ = Describe("Default monitoring queries", func() {
	It("correctly set the default monitoring queries configmap and secret when none is already specified", func() {
		cluster := &Cluster{}
		cluster.defaultMonitoringQueries(&configuration.Data{
			MonitoringQueriesSecret:    "test-secret",
			MonitoringQueriesConfigmap: "test-configmap",
		})
		Expect(cluster.Spec.Monitoring).NotTo(BeNil())
		Expect(cluster.Spec.Monitoring.CustomQueriesConfigMap).NotTo(BeEmpty())
		Expect(cluster.Spec.Monitoring.CustomQueriesConfigMap).
			To(ContainElement(ConfigMapKeySelector{
				LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringConfigMapName},
				Key:                  DefaultMonitoringKey,
			}))
		Expect(cluster.Spec.Monitoring.CustomQueriesSecret).NotTo(BeEmpty())
		Expect(cluster.Spec.Monitoring.CustomQueriesSecret).
			To(ContainElement(SecretKeySelector{
				LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringSecretName},
				Key:                  DefaultMonitoringKey,
			}))
	})
	testCluster := &Cluster{Spec: ClusterSpec{Monitoring: &MonitoringConfiguration{
		CustomQueriesConfigMap: []ConfigMapKeySelector{
			{
				LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringConfigMapName},
				Key:                  "test2",
			},
		},
		CustomQueriesSecret: []SecretKeySelector{
			{
				LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringConfigMapName},
				Key:                  "test3",
			},
		},
	}}}
	It("correctly set the default monitoring queries configmap when other metrics are already specified", func() {
		modifiedCluster := testCluster.DeepCopy()
		modifiedCluster.defaultMonitoringQueries(&configuration.Data{
			MonitoringQueriesConfigmap: "test-configmap",
		})

		Expect(modifiedCluster.Spec.Monitoring).NotTo(BeNil())
		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesConfigMap).NotTo(BeEmpty())
		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesSecret).NotTo(BeEmpty())
		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesConfigMap).
			To(ContainElement(ConfigMapKeySelector{
				LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringConfigMapName},
				Key:                  "test2",
			}))

		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesSecret).
			To(BeEquivalentTo(testCluster.Spec.Monitoring.CustomQueriesSecret))
		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesConfigMap).
			To(ContainElements(testCluster.Spec.Monitoring.CustomQueriesConfigMap))
	})
	It("correctly set the default monitoring queries secret when other metrics are already specified", func() {
		modifiedCluster := testCluster.DeepCopy()
		modifiedCluster.defaultMonitoringQueries(&configuration.Data{
			MonitoringQueriesSecret: "test-secret",
		})

		Expect(modifiedCluster.Spec.Monitoring).NotTo(BeNil())
		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesSecret).NotTo(BeEmpty())
		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesConfigMap).NotTo(BeEmpty())
		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesSecret).
			To(ContainElement(SecretKeySelector{
				LocalObjectReference: LocalObjectReference{Name: DefaultMonitoringSecretName},
				Key:                  "test3",
			}))

		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesConfigMap).
			To(BeEquivalentTo(testCluster.Spec.Monitoring.CustomQueriesConfigMap))
		Expect(modifiedCluster.Spec.Monitoring.CustomQueriesSecret).
			To(ContainElements(testCluster.Spec.Monitoring.CustomQueriesSecret))
	})
})

var _ = Describe("validation of imports", func() {
	It("rejects unrecognized import type", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type: "fooBar",
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(HaveLen(1))
	})

	It("rejects microservice import with roles", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MicroserviceSnapshotType,
							Databases: []string{"foo"},
							Roles:     []string{"bar"},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(HaveLen(1))
	})

	It("rejects microservice import without exactly one database", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MicroserviceSnapshotType,
							Databases: []string{"foo", "bar"},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(HaveLen(1))
	})

	It("rejects microservice import with a wildcard on the database name", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MicroserviceSnapshotType,
							Databases: []string{"*foo"},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(HaveLen(1))
	})

	It("accepts microservice import when well specified", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MicroserviceSnapshotType,
							Databases: []string{"foo"},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(BeEmpty())
	})

	It("rejects monolith import with no databases", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MonolithSnapshotType,
							Databases: []string{},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(HaveLen(1))
	})

	It("rejects monolith import with PostImport Application SQL", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:                     MonolithSnapshotType,
							Databases:                []string{"foo"},
							PostImportApplicationSQL: []string{"select * from bar"},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(HaveLen(1))
	})

	It("rejects monolith import with wildcards alongside specific values", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MonolithSnapshotType,
							Databases: []string{"bar", "*"},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(HaveLen(1))

		cluster = &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MonolithSnapshotType,
							Databases: []string{"foo"},
							Roles:     []string{"baz", "*"},
						},
					},
				},
			},
		}

		result = cluster.validateImport()
		Expect(result).To(HaveLen(1))
	})

	It("accepts monolith import with proper values", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MonolithSnapshotType,
							Databases: []string{"foo"},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(BeEmpty())
	})

	It("accepts monolith import with wildcards", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					InitDB: &BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &Import{
							Type:      MonolithSnapshotType,
							Databases: []string{"*"},
							Roles:     []string{"*"},
						},
					},
				},
			},
		}

		result := cluster.validateImport()
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("validation of replication slots configuration", func() {
	It("prevents using replication slots on PostgreSQL 10 and older", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ImageName: "ghcr.io/cloudnative-pg/postgresql:10.5",
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
					UpdateInterval: 0,
				},
			},
		}
		cluster.Default()

		result := cluster.validateReplicationSlots()
		Expect(result).To(HaveLen(1))
	})

	It("can be enabled on the default PostgreSQL image", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
					UpdateInterval: 0,
				},
			},
		}
		cluster.Default()

		result := cluster.validateReplicationSlots()
		Expect(result).To(BeEmpty())
	})

	It("set replicationSlots by default", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ImageName: versions.DefaultImageName,
			},
		}
		cluster.Default()
		Expect(cluster.Spec.ReplicationSlots).ToNot(BeNil())
		Expect(cluster.Spec.ReplicationSlots.HighAvailability).ToNot(BeNil())
		Expect(cluster.Spec.ReplicationSlots.HighAvailability.Enabled).To(HaveValue(BeTrue()))

		result := cluster.validateReplicationSlots()
		Expect(result).To(BeEmpty())
	})

	It("set replicationSlots.highAvailability by default", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &ReplicationSlotsConfiguration{
					UpdateInterval: 30,
				},
			},
		}
		cluster.Default()
		Expect(cluster.Spec.ReplicationSlots.HighAvailability).ToNot(BeNil())
		Expect(cluster.Spec.ReplicationSlots.HighAvailability.Enabled).To(HaveValue(BeTrue()))

		result := cluster.validateReplicationSlots()
		Expect(result).To(BeEmpty())
	})

	It("allows enabling replication slots on the fly", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(false),
					},
				},
			},
		}
		oldCluster.Default()

		newCluster := oldCluster.DeepCopy()
		newCluster.Spec.ReplicationSlots = &ReplicationSlotsConfiguration{
			HighAvailability: &ReplicationSlotsHAConfiguration{
				Enabled:    ptr.To(true),
				SlotPrefix: "_test_",
			},
		}

		Expect(newCluster.validateReplicationSlotsChange(oldCluster)).To(BeEmpty())
	})

	It("prevents changing the slot prefix while replication slots are enabled", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled:    ptr.To(true),
						SlotPrefix: "_test_",
					},
				},
			},
		}
		oldCluster.Default()

		newCluster := oldCluster.DeepCopy()
		newCluster.Spec.ReplicationSlots.HighAvailability.SlotPrefix = "_toast_"
		Expect(newCluster.validateReplicationSlotsChange(oldCluster)).To(HaveLen(1))
	})

	It("prevents removing the replication slot section when replication slots are enabled", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled:    ptr.To(true),
						SlotPrefix: "_test_",
					},
				},
			},
		}
		oldCluster.Default()

		newCluster := oldCluster.DeepCopy()
		newCluster.Spec.ReplicationSlots = nil
		Expect(newCluster.validateReplicationSlotsChange(oldCluster)).To(HaveLen(1))
	})

	It("allows disabling the replication slots", func() {
		oldCluster := &Cluster{
			Spec: ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled:    ptr.To(true),
						SlotPrefix: "_test_",
					},
				},
			},
		}
		oldCluster.Default()

		newCluster := oldCluster.DeepCopy()
		newCluster.Spec.ReplicationSlots.HighAvailability.Enabled = ptr.To(false)
		Expect(newCluster.validateReplicationSlotsChange(oldCluster)).To(BeEmpty())
	})
})

var _ = Describe("Environment variables validation", func() {
	When("an environment variable is given", func() {
		It("detects if it is valid", func() {
			Expect(isReservedEnvironmentVariable("PGDATA")).To(BeTrue())
		})

		It("detects if it is not valid", func() {
			Expect(isReservedEnvironmentVariable("LC_ALL")).To(BeFalse())
		})
	})

	When("a ClusterSpec is given", func() {
		It("detects if the environment variable list is correct", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					Env: []corev1.EnvVar{
						{
							Name:  "TZ",
							Value: "Europe/Rome",
						},
					},
				},
			}

			Expect(cluster.validateEnv()).To(BeEmpty())
		})

		It("detects if the environment variable list contains a reserved variable", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					Env: []corev1.EnvVar{
						{
							Name:  "TZ",
							Value: "Europe/Rome",
						},
						{
							Name:  "PGDATA",
							Value: "/tmp",
						},
					},
				},
			}

			Expect(cluster.validateEnv()).To(HaveLen(1))
		})
	})
})

var _ = Describe("Storage configuration validation", func() {
	When("a ClusterSpec is given", func() {
		It("produces one error if storage is not set at all", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					StorageConfiguration: StorageConfiguration{},
				},
			}
			Expect(cluster.validateStorageSize()).To(HaveLen(1))
		})

		It("succeeds if storage size is set", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					StorageConfiguration: StorageConfiguration{
						Size: "1G",
					},
				},
			}
			Expect(cluster.validateStorageSize()).To(BeEmpty())
		})

		It("succeeds if storage is not set but a pvc template specifies storage", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					StorageConfiguration: StorageConfiguration{
						PersistentVolumeClaimTemplate: &corev1.PersistentVolumeClaimSpec{
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
							},
						},
					},
				},
			}
			Expect(cluster.validateStorageSize()).To(BeEmpty())
		})
	})
})

var _ = Describe("Role management validation", func() {
	It("should succeed if there is no management stanza", func() {
		cluster := Cluster{
			Spec: ClusterSpec{},
		}
		Expect(cluster.validateManagedRoles()).To(BeEmpty())
	})

	It("should succeed if the role defined is not reserved", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Managed: &ManagedConfiguration{
					Roles: []RoleConfiguration{
						{
							Name: "non-conflicting",
						},
					},
				},
			},
		}
		Expect(cluster.validateManagedRoles()).To(BeEmpty())
	})

	It("should produce an error on invalid connection limit", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Managed: &ManagedConfiguration{
					Roles: []RoleConfiguration{
						{
							Name:            "non-conflicting",
							ConnectionLimit: -3,
						},
					},
				},
			},
		}
		Expect(cluster.validateManagedRoles()).To(HaveLen(1))
	})

	It("should produce an error if the role is reserved", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Managed: &ManagedConfiguration{
					Roles: []RoleConfiguration{
						{
							Name: "postgres",
						},
					},
				},
			},
		}
		Expect(cluster.validateManagedRoles()).To(HaveLen(1))
	})

	It("should produce two errors if the role is reserved and the connection limit is invalid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Managed: &ManagedConfiguration{
					Roles: []RoleConfiguration{
						{
							Name:            "postgres",
							ConnectionLimit: -3,
						},
					},
				},
			},
		}
		Expect(cluster.validateManagedRoles()).To(HaveLen(2))
	})

	It("should produce an error if we define two roles with the same name", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Managed: &ManagedConfiguration{
					Roles: []RoleConfiguration{
						{
							Name:            "my_test",
							ConnectionLimit: -1,
						},
						{
							Name:            "my_test",
							Superuser:       true,
							BypassRLS:       true,
							ConnectionLimit: -1,
						},
					},
				},
			},
		}
		Expect(cluster.validateManagedRoles()).To(HaveLen(1))
	})
	It("should produce an error if we have a password secret AND DisablePassword in a role", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Managed: &ManagedConfiguration{
					Roles: []RoleConfiguration{
						{
							Name:            "my_test",
							Superuser:       true,
							BypassRLS:       true,
							DisablePassword: true,
							PasswordSecret: &LocalObjectReference{
								Name: "myPassword",
							},
							ConnectionLimit: -1,
						},
					},
				},
			},
		}
		Expect(cluster.validateManagedRoles()).To(HaveLen(1))
	})
})

var _ = Describe("Managed Extensions validation", func() {
	It("should succeed if no extension is enabled", func() {
		cluster := Cluster{
			Spec: ClusterSpec{},
		}
		Expect(cluster.validateManagedExtensions()).To(BeEmpty())
	})

	It("should succeed if pg_failover_slots and its prerequisites are enabled", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
				},
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"hot_standby_feedback":                     "on",
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(cluster.validatePgFailoverSlots()).To(BeEmpty())
	})

	It("should produce two errors if pg_failover_slots is enabled and its prerequisites are disabled", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(cluster.validatePgFailoverSlots()).To(HaveLen(2))
	})

	It("should produce an error if pg_failover_slots is enabled and HA slots are disabled", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"hot_standby_feedback":                     "on",
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(cluster.validatePgFailoverSlots()).To(HaveLen(1))
	})

	It("should produce an error if pg_failover_slots is enabled and hot_standby_feedback is disabled", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				ReplicationSlots: &ReplicationSlotsConfiguration{
					HighAvailability: &ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
				},
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(cluster.validatePgFailoverSlots()).To(HaveLen(1))
	})
})

var _ = Describe("Recovery from volume snapshot validation", func() {
	clusterFromRecovery := func(recovery *BootstrapRecovery) *Cluster {
		return &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: recovery,
				},
				WalStorage: &StorageConfiguration{},
			},
		}
	}

	It("should produce an error when defining two recovery sources at the same time", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						Source:          "sourceName",
						Backup:          &BackupSource{},
						VolumeSnapshots: &DataSource{},
					},
				},
			},
		}
		Expect(cluster.validateBootstrapRecoveryDataSource()).To(HaveLen(1))
	})

	It("should produce an error when defining a backupID while recovering using a DataSource", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							BackupID: "20220616T031500",
						},
						VolumeSnapshots: &DataSource{
							Storage: corev1.TypedLocalObjectReference{
								APIGroup: ptr.To(""),
								Kind:     "PersistentVolumeClaim",
								Name:     "pgdata",
							},
						},
					},
				},
			},
		}
		Expect(cluster.validateBootstrapRecoveryDataSource()).To(HaveLen(1))
	})

	It("should produce an error when asking to recovery WALs from a snapshot without having storage for it", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						VolumeSnapshots: &DataSource{
							Storage: corev1.TypedLocalObjectReference{
								APIGroup: ptr.To(""),
								Kind:     "PersistentVolumeClaim",
								Name:     "pgdata",
							},
							WalStorage: &corev1.TypedLocalObjectReference{
								APIGroup: ptr.To(""),
								Kind:     "PersistentVolumeClaim",
								Name:     "pgwal",
							},
						},
					},
				},
			},
		}
		Expect(cluster.validateBootstrapRecoveryDataSource()).To(HaveLen(1))
	})

	It("should not produce an error when the configuration is sound", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						VolumeSnapshots: &DataSource{
							Storage: corev1.TypedLocalObjectReference{
								APIGroup: ptr.To(""),
								Kind:     "PersistentVolumeClaim",
								Name:     "pgdata",
							},
							WalStorage: &corev1.TypedLocalObjectReference{
								APIGroup: ptr.To(""),
								Kind:     "PersistentVolumeClaim",
								Name:     "pgwal",
							},
						},
					},
				},
				WalStorage: &StorageConfiguration{},
			},
		}
		Expect(cluster.validateBootstrapRecoveryDataSource()).To(BeEmpty())
	})

	It("accepts recovery from a VolumeSnapshot", func() {
		cluster := clusterFromRecovery(&BootstrapRecovery{
			VolumeSnapshots: &DataSource{
				Storage: corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(storagesnapshotv1.GroupName),
					Kind:     "VolumeSnapshot",
					Name:     "pgdata",
				},
				WalStorage: &corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(storagesnapshotv1.GroupName),
					Kind:     "VolumeSnapshot",
					Name:     "pgwal",
				},
			},
		})
		Expect(cluster.validateBootstrapRecoveryDataSource()).To(BeEmpty())
	})

	It("accepts recovery from a VolumeSnapshot, while restoring WALs from an object store", func() {
		cluster := clusterFromRecovery(&BootstrapRecovery{
			VolumeSnapshots: &DataSource{
				Storage: corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(storagesnapshotv1.GroupName),
					Kind:     "VolumeSnapshot",
					Name:     "pgdata",
				},
			},

			Source: "pg-cluster",
		})
		Expect(cluster.validateBootstrapRecoveryDataSource()).To(BeEmpty())
	})

	When("using an nil apiGroup", func() {
		It("accepts recovery from a PersistentVolumeClaim", func() {
			cluster := clusterFromRecovery(&BootstrapRecovery{
				VolumeSnapshots: &DataSource{
					Storage: corev1.TypedLocalObjectReference{
						APIGroup: nil,
						Kind:     "PersistentVolumeClaim",
						Name:     "pgdata",
					},
					WalStorage: &corev1.TypedLocalObjectReference{
						APIGroup: nil,
						Kind:     "PersistentVolumeClaim",
						Name:     "pgwal",
					},
				},
			})
			Expect(cluster.validateBootstrapRecoveryDataSource()).To(BeEmpty())
		})
	})

	When("using an empty apiGroup", func() {
		It("accepts recovery from a PersistentVolumeClaim", func() {
			cluster := clusterFromRecovery(&BootstrapRecovery{
				VolumeSnapshots: &DataSource{
					Storage: corev1.TypedLocalObjectReference{
						APIGroup: ptr.To(""),
						Kind:     "PersistentVolumeClaim",
						Name:     "pgdata",
					},
					WalStorage: &corev1.TypedLocalObjectReference{
						APIGroup: ptr.To(""),
						Kind:     "PersistentVolumeClaim",
						Name:     "pgwal",
					},
				},
			})
			Expect(cluster.validateBootstrapRecoveryDataSource()).To(BeEmpty())
		})
	})

	It("prevent recovery from other Objects", func() {
		cluster := clusterFromRecovery(&BootstrapRecovery{
			VolumeSnapshots: &DataSource{
				Storage: corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(""),
					Kind:     "Secret",
					Name:     "pgdata",
				},
				WalStorage: &corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(""),
					Kind:     "ConfigMap",
					Name:     "pgwal",
				},
			},
		})
		Expect(cluster.validateBootstrapRecoveryDataSource()).To(HaveLen(2))
	})
})

var _ = Describe("validateResources", func() {
	var cluster *Cluster

	BeforeEach(func() {
		cluster = &Cluster{
			Spec: ClusterSpec{
				PostgresConfiguration: PostgresConfiguration{
					Parameters: map[string]string{},
				},
				Resources: corev1.ResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{},
					Limits:   map[corev1.ResourceName]resource.Quantity{},
				},
			},
		}
	})

	It("returns an error when the CPU request is greater than CPU limit", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("2")
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("1")

		errors := cluster.validateResources()
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("CPU request is greater than the limit"))
	})

	It("returns an error when the Memory request is greater than Memory limit", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("2Gi")
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")

		errors := cluster.validateResources()
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Memory request is greater than the limit"))
	})

	It("returns two errors when both CPU and Memory requests are greater than their limits", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("2")
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("1")
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("2Gi")
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")

		errors := cluster.validateResources()
		Expect(errors).To(HaveLen(2))
		Expect(errors[0].Detail).To(Equal("CPU request is greater than the limit"))
		Expect(errors[1].Detail).To(Equal("Memory request is greater than the limit"))
	})

	It("returns no errors when both CPU and Memory requests are less than or equal to their limits", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("1")
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1Gi")
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("2Gi")

		errors := cluster.validateResources()
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when CPU request is set but limit is nil", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("1")
		errors := cluster.validateResources()
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when CPU limit is set but request is nil", func() {
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("1")
		errors := cluster.validateResources()
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when Memory request is set but limit is nil", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1Gi")
		errors := cluster.validateResources()
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when Memory limit is set but request is nil", func() {
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")
		errors := cluster.validateResources()
		Expect(errors).To(BeEmpty())
	})

	It("returns an error when memoryRequest is less than shared_buffers in kB", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1Gi")
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "2000000kB"
		errors := cluster.validateResources()
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Memory request is lower than PostgreSQL `shared_buffers` value"))
	})

	It("returns an error when memoryRequest is less than shared_buffers in MB", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1000Mi")
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "2000MB"
		errors := cluster.validateResources()
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Memory request is lower than PostgreSQL `shared_buffers` value"))
	})

	It("returns no errors when memoryRequest is greater than or equal to shared_buffers in GB", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("2Gi")
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "1GB"
		errors := cluster.validateResources()
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when shared_buffers is in a format that can't be parsed", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1Gi")
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "invalid_value"
		errors := cluster.validateResources()
		Expect(errors).To(BeEmpty())
	})
})
