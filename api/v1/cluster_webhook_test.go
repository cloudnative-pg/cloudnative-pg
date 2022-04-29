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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
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
		Expect(len(result)).To(Equal(1))
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
		Expect(len(result)).To(Equal(1))
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
		Expect(len(result)).To(Equal(1))
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
		Expect(len(result)).To(Equal(1))
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
		Expect(len(result)).To(Equal(1))
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
		Expect(len(result)).To(Equal(1))
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

	It("defaults to creating an application database if recovery is used", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{},
				},
			},
		}
		cluster.Default()
		Expect(cluster.ShouldRecoveryCreateApplicationDatabase()).Should(BeFalse())
		Expect(cluster.Spec.Bootstrap.Recovery.Database).Should(BeEmpty())
		Expect(cluster.Spec.Bootstrap.Recovery.Owner).Should(BeEmpty())
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

	It("defaults not to create an application database if pg_basebackup is used", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					PgBaseBackup: &BootstrapPgBaseBackup{},
				},
			},
		}
		cluster.Default()
		Expect(cluster.ShouldPgBaseBackupCreateApplicationDatabase()).Should(BeFalse())
		Expect(cluster.Spec.Bootstrap.PgBaseBackup.Database).Should(BeEmpty())
		Expect(cluster.Spec.Bootstrap.PgBaseBackup.Owner).Should(BeEmpty())
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
		Expect(len(cluster.Spec.PostgresConfiguration.Parameters)).To(BeNumerically(">", 0))
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
		Expect(len(result)).To(Equal(1))
	})
	It("does not complain if the imagePullPolicy is valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImagePullPolicy: "Always",
			},
		}

		result := cluster.validateImagePullPolicy()
		Expect(len(result)).To(Equal(0))
	})
})

var _ = Describe("Storage validation", func() {
	It("complains if the value isn't correct", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size: "X",
				},
			},
		}

		result := cluster.validateStorageConfiguration()
		Expect(len(result)).To(Equal(1))
	})

	It("doesn't complain if value is correct", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size: "1Gi",
				},
			},
		}

		result := cluster.validateStorageConfiguration()
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
		Expect(len(cluster.validateImageName())).To(Equal(1))
	})

	It("complains when only the sha is passed", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres@sha256:cff94de382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866",
			},
		}
		Expect(len(cluster.validateImageName())).To(Equal(1))
	})

	It("doesn't complain if the tag is valid", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.4",
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
		Expect(len(cluster.validateImageName())).To(Equal(1))
	})
})

var _ = Describe("configuration change validation", func() {
	It("doesn't complain when the configuration is exactly the same", func() {
		clusterOld := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		clusterNew := clusterOld
		Expect(len(clusterNew.validateConfigurationChange(&clusterOld))).To(Equal(0))
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
		Expect(len(clusterNew.validateConfigurationChange(&clusterOld))).To(Equal(0))
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
		Expect(len(clusterNew.validateConfigurationChange(&clusterOld))).To(Equal(1))
	})
})

var _ = Describe("validate image name change", func() {
	It("doesn't complain with no changes", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{},
		}
		Expect(len(clusterNew.validateImageChange(""))).To(Equal(0))
	})

	It("complains if versions are wrong", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:12.0",
			},
		}
		Expect(len(clusterNew.validateImageChange("12:1"))).To(Equal(1))
	})

	It("complains if can't upgrade between mayor versions", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:11.0",
			},
		}
		Expect(len(clusterNew.validateImageChange("postgres:12.0"))).To(Equal(1))
	})

	It("doesn't complain if image change it's valid", func() {
		clusterNew := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:12.0",
			},
		}
		Expect(len(clusterNew.validateImageChange("postgres:12.1"))).To(Equal(0))
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
							TargetXID:       "3",
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

		Expect(len(cluster.validateRecoveryTarget())).To(Equal(1))
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

		Expect(len(cluster.validateRecoveryTarget())).To(Equal(0))
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

		Expect(len(cluster.validateRecoveryTarget())).To(Equal(0))
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

		Expect(len(cluster.validateRecoveryTarget())).To(Equal(1))
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

		Expect(len(cluster.validateRecoveryTarget())).To(Equal(1))
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

		Expect(len(cluster.validateRecoveryTarget())).To(Equal(0))
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

		Expect(len(cluster.validateRecoveryTarget())).To(Equal(0))
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
			Expect(len(cluster.validateRecoveryTarget())).To(Equal(0))
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
			Expect(len(cluster.validateRecoveryTarget())).To(Equal(0))
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
			Expect(len(cluster.validateRecoveryTarget())).To(Equal(1))
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
			Expect(len(cluster.validateRecoveryTarget())).To(Equal(1))
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
			Expect(len(cluster.validateRecoveryTarget())).To(Equal(1))
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
	It("complains if the storage size is not parsable", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size: "10 apples",
				},
			},
		}
		Expect(cluster.validateStorageSize()).ToNot(BeEmpty())
	})

	It("works fine if the size is good", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				StorageConfiguration: StorageConfiguration{
					Size: "10G",
				},
			},
		}
		Expect(cluster.validateStorageSize()).To(BeEmpty())
	})

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

		result := cluster.validatePgBaseBackup()
		Expect(len(result)).To(Equal(1))
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

		result := cluster.validatePgBaseBackup()
		Expect(len(result)).To(Equal(1))
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

		result := cluster.validatePgBaseBackup()
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

		result := cluster.validateRecovery()
		Expect(len(result)).To(Equal(1))
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

		result := cluster.validateRecovery()
		Expect(len(result)).To(Equal(1))
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

		result := cluster.validateRecovery()
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
					Tolerations: []v1.Toleration{
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
					Tolerations: []v1.Toleration{
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
		Expect(len(err)).To(Equal(1))
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
		Expect(len(err)).To(Equal(2))
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

var _ = Describe("Recovery and Backup Target", func() {
	cluster := &Cluster{
		Spec: ClusterSpec{
			Bootstrap: &BootstrapConfiguration{
				Recovery: &BootstrapRecovery{
					Source: "one",
				},
			},
			Backup: &BackupConfiguration{
				BarmanObjectStore: &BarmanObjectStoreConfiguration{
					S3Credentials: &S3Credentials{
						AccessKeyIDReference: &SecretKeySelector{
							LocalObjectReference: LocalObjectReference{
								Name: "s3-creds",
							},
							Key: "access_key",
						},
						SecretAccessKeyReference: &SecretKeySelector{
							LocalObjectReference: LocalObjectReference{
								Name: "s3-creds",
							},
							Key: "secret_key",
						},
					},
					DestinationPath: "/destination/path",
				},
			},
			ExternalClusters: []ExternalCluster{
				{
					Name: "one",
					BarmanObjectStore: &BarmanObjectStoreConfiguration{
						S3Credentials: &S3Credentials{
							AccessKeyIDReference: &SecretKeySelector{
								LocalObjectReference: LocalObjectReference{
									Name: "s3-creds",
								},
								Key: "access_key",
							},
							SecretAccessKeyReference: &SecretKeySelector{
								LocalObjectReference: LocalObjectReference{
									Name: "s3-creds",
								},
								Key: "secret_key",
							},
						},
						DestinationPath: "/destination/path",
					},
				},
			},
		},
	}

	It("complains if external cluster and target backup are equal", func() {
		result := cluster.validateRecoveryAndBackupTarget()
		Expect(result).NotTo(BeEmpty())
	})

	It("does not complain if target backup is different from external backup in path", func() {
		cluster.Spec.Backup.BarmanObjectStore.DestinationPath = "/destination/new/path"
		result := cluster.validateRecoveryAndBackupTarget()
		Expect(result).To(BeEmpty())
	})
})
