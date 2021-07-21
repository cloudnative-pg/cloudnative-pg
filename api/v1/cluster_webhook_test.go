/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
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

	It("complains when we changed a fixed setting", func() {
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
						"data_directory": "/var/pgdata/here",
					},
				},
			},
		}
		Expect(len(clusterNew.validateConfigurationChange(&clusterOld))).To(Equal(1))
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
				ImageName: "postgres:11.0",
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
							TargetTime:      "2020-01-01 01:01",
							TargetImmediate: nil,
							Exclusive:       nil,
						},
					},
				},
			},
		}

		Expect(len(cluster.validateRecoveryTarget())).To(Equal(1))
	})

	It("can be specified", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					Recovery: &BootstrapRecovery{
						RecoveryTarget: &RecoveryTarget{
							TargetTime: "2020-01-01 01:01",
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

		It("allows 'current'", func() {
			cluster := Cluster{
				Spec: ClusterSpec{
					Bootstrap: &BootstrapConfiguration{
						Recovery: &BootstrapRecovery{
							RecoveryTarget: &RecoveryTarget{
								TargetTLI: "current",
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

var _ = Describe("storage size validation", func() {
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

		Expect(clusterNew.validateStorageSizeChange(&clusterOld)).ToNot(BeEmpty())
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

		Expect(clusterNew.validateStorageSizeChange(&clusterOld)).To(BeEmpty())
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

var _ = Describe("validation of the list of external servers", func() {
	It("is correct when it's empty", func() {
		cluster := Cluster{}
		Expect(cluster.validateExternalServers()).To(BeEmpty())
	})

	It("complains when the list of servers contains duplicates", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{
						Name: "one",
					},
					{
						Name: "one",
					},
				},
			},
		}
		Expect(cluster.validateExternalServers()).ToNot(BeEmpty())
	})

	It("should not raise errors is the server name is unique", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{
						Name: "one",
					},
					{
						Name: "two",
					},
				},
			},
		}
		Expect(cluster.validateExternalServers()).To(BeEmpty())
	})
})

var _ = Describe("bootstrap base backup validation", func() {
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

var _ = Describe("validation of the list of external servers", func() {
	It("is correct when it's empty", func() {
		cluster := Cluster{}
		Expect(cluster.validateExternalServers()).To(BeEmpty())
	})

	It("complains when the list of servers contains duplicates", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{
						Name: "one",
					},
					{
						Name: "one",
					},
				},
			},
		}
		Expect(cluster.validateExternalServers()).ToNot(BeEmpty())
	})

	It("should not raise errors is the server name is unique", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ExternalClusters: []ExternalCluster{
					{
						Name: "one",
					},
					{
						Name: "two",
					},
				},
			},
		}
		Expect(cluster.validateExternalServers()).To(BeEmpty())
	})
})

var _ = Describe("bootstrap base backup validation", func() {
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
		bootstrap := BootstrapConfiguration{}
		bpb := BootstrapPgBaseBackup{"test"}
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

var _ = Describe("replica mode validation", func() {
	It("complain if pg_basebackup bootstrap option is not used", func() {
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

		cluster.Spec.Bootstrap.PgBaseBackup = nil
		result = cluster.validateReplicaMode()
		Expect(result).ToNot(BeEmpty())
	})

	It("complains when the external server doesn't exist", func() {
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
