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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudnative-pg/barman-cloud/pkg/api"
	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	pgversion "github.com/cloudnative-pg/machinery/pkg/postgres/version"
	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("bootstrap methods validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain if there isn't a configuration", func() {
		emptyCluster := &apiv1.Cluster{}
		result := v.validateBootstrapMethod(emptyCluster)
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we are using initdb", func() {
		initdbCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
			},
		}
		result := v.validateBootstrapMethod(initdbCluster)
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we are using recovery", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{},
				},
			},
		}
		result := v.validateBootstrapMethod(recoveryCluster)
		Expect(result).To(BeEmpty())
	})

	It("complains where there are two active bootstrap methods", func() {
		invalidCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{},
					InitDB:   &apiv1.BootstrapInitDB{},
				},
			},
		}
		result := v.validateBootstrapMethod(invalidCluster)
		Expect(result).To(HaveLen(1))
	})
})

var _ = Describe("certificates options validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain if there isn't a configuration", func() {
		emptyCluster := &apiv1.Cluster{}
		result := v.validateCerts(emptyCluster)
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if you specify some valid secret names", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Certificates: &apiv1.CertificatesConfiguration{
					ServerCASecret:  "test-server-ca",
					ServerTLSSecret: "test-server-tls",
				},
			},
		}
		result := v.validateCerts(cluster)
		Expect(result).To(BeEmpty())
	})

	It("does complain if you specify the TLS secret and not the CA", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Certificates: &apiv1.CertificatesConfiguration{
					ServerTLSSecret: "test-server-tls",
				},
			},
		}
		result := v.validateCerts(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("does complain if you specify the TLS secret and AltDNSNames is not empty", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Certificates: &apiv1.CertificatesConfiguration{
					ServerCASecret:    "test-server-ca",
					ServerTLSSecret:   "test-server-tls",
					ServerAltDNSNames: []string{"dns-name"},
				},
			},
		}
		result := v.validateCerts(cluster)
		Expect(result).To(HaveLen(1))
	})
})

var _ = Describe("initdb options validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain if there isn't a configuration", func() {
		emptyCluster := &apiv1.Cluster{}
		result := v.validateInitDB(emptyCluster)
		Expect(result).To(BeEmpty())
	})

	It("complains if you specify the database name but not the owner", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
					},
				},
			},
		}

		result := v.validateInitDB(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("complains if you specify the owner but not the database name", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Owner: "app",
					},
				},
			},
		}

		result := v.validateInitDB(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if you specify both database name and owner user", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
					},
				},
			},
		}

		result := v.validateInitDB(cluster)
		Expect(result).To(BeEmpty())
	})

	It("complain if key is missing in the secretRefs", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &apiv1.SQLRefs{
							SecretRefs: []apiv1.SecretKeySelector{
								{
									LocalObjectReference: apiv1.LocalObjectReference{Name: "secret1"},
								},
							},
						},
					},
				},
			},
		}

		result := v.validateInitDB(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("complain if name is missing in the secretRefs", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &apiv1.SQLRefs{
							SecretRefs: []apiv1.SecretKeySelector{
								{
									Key: "key",
								},
							},
						},
					},
				},
			},
		}

		result := v.validateInitDB(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("complain if key is missing in the configMapRefs", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &apiv1.SQLRefs{
							ConfigMapRefs: []apiv1.ConfigMapKeySelector{
								{
									LocalObjectReference: apiv1.LocalObjectReference{Name: "configmap1"},
								},
							},
						},
					},
				},
			},
		}

		result := v.validateInitDB(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("complain if name is missing in the configMapRefs", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &apiv1.SQLRefs{
							ConfigMapRefs: []apiv1.ConfigMapKeySelector{
								{
									Key: "key",
								},
							},
						},
					},
				},
			},
		}

		result := v.validateInitDB(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if configmapRefs and secretRefs are valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						PostInitApplicationSQLRefs: &apiv1.SQLRefs{
							ConfigMapRefs: []apiv1.ConfigMapKeySelector{
								{
									LocalObjectReference: apiv1.LocalObjectReference{Name: "configmap1"},
									Key:                  "key",
								},
								{
									LocalObjectReference: apiv1.LocalObjectReference{Name: "configmap2"},
									Key:                  "key",
								},
							},
							SecretRefs: []apiv1.SecretKeySelector{
								{
									LocalObjectReference: apiv1.LocalObjectReference{Name: "secret1"},
									Key:                  "key",
								},
								{
									LocalObjectReference: apiv1.LocalObjectReference{Name: "secret2"},
									Key:                  "key",
								},
							},
						},
					},
				},
			},
		}

		result := v.validateInitDB(cluster)
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if superuser secret it's empty", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{},
		}

		result := v.validateSuperuserSecret(cluster)

		Expect(result).To(BeEmpty())
	})

	It("complains if superuser secret name it's empty", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				SuperuserSecret: &apiv1.LocalObjectReference{
					Name: "",
				},
			},
		}

		result := v.validateSuperuserSecret(cluster)
		Expect(result).To(HaveLen(1))
	})
})

var _ = Describe("ImagePullPolicy validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complains if the imagePullPolicy isn't valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImagePullPolicy: "wrong",
			},
		}

		result := v.validateImagePullPolicy(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("does not complain if the imagePullPolicy is valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImagePullPolicy: "Always",
			},
		}

		result := v.validateImagePullPolicy(cluster)
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("Image name validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain if the user simply accept the default", func() {
		var cluster apiv1.Cluster
		Expect(v.validateImageName(&cluster)).To(BeEmpty())

		// Let's apply the defaulting webhook, too
		cluster.Default()
		Expect(v.validateImageName(&cluster)).To(BeEmpty())
	})

	It("complains when the 'latest' tag is detected", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:latest",
			},
		}
		Expect(v.validateImageName(cluster)).To(HaveLen(1))
	})

	It("doesn't complain when a alpha tag is used", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:15alpha1",
			},
		}
		Expect(v.validateImageName(cluster)).To(BeEmpty())
	})

	It("doesn't complain when a beta tag is used", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:15beta1",
			},
		}
		Expect(v.validateImageName(cluster)).To(BeEmpty())
	})

	It("doesn't complain when a release candidate tag is used", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:15rc1",
			},
		}
		Expect(v.validateImageName(cluster)).To(BeEmpty())
	})

	It("complains when only the sha is passed", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres@sha256:cff94de382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866",
			},
		}
		Expect(v.validateImageName(cluster)).To(HaveLen(1))
	})

	It("doesn't complain if the tag is valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		Expect(v.validateImageName(cluster)).To(BeEmpty())
	})

	It("doesn't complain if the tag is valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:14.4-1",
			},
		}
		Expect(v.validateImageName(cluster)).To(BeEmpty())
	})

	It("doesn't complain if the tag is valid and has sha", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:10.4@sha256:cff94de382ca538861622bbe84cfe03f44f307a9846a5c5eda672cf4dc692866",
			},
		}
		Expect(v.validateImageName(cluster)).To(BeEmpty())
	})

	It("complain when the tag name is not a PostgreSQL version", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:test_12",
			},
		}
		Expect(v.validateImageName(cluster)).To(HaveLen(1))
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
	Entry("B", "1B", resource.MustParse("1"), false),
	Entry("kB", "1kB", resource.MustParse("1Ki"), false),
	Entry("MB", "1MB", resource.MustParse("1Mi"), false),
	Entry("GB", "1GB", resource.MustParse("1Gi"), false),
	Entry("TB", "1TB", resource.MustParse("1Ti"), false),
	Entry("spaceB", "1 B", resource.MustParse("1"), false),
	Entry("spaceMB", "1 MB", resource.MustParse("1Mi"), false),
	Entry("reject bare", "1", resource.Quantity{}, true),
	Entry("reject kb", "1kb", resource.Quantity{}, true),
	Entry("reject Mb", "1Mb", resource.Quantity{}, true),
	Entry("reject G", "1G", resource.Quantity{}, true),
	Entry("reject random unit", "1random", resource.Quantity{}, true),
	Entry("reject non-numeric", "non-numeric", resource.Quantity{}, true),
)

var _ = Describe("configuration change validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain when the configuration is exactly the same", func() {
		clusterOld := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		clusterNew := clusterOld.DeepCopy()
		Expect(v.validateConfigurationChange(clusterNew, clusterOld)).To(BeEmpty())
	})

	It("doesn't complain when we change a setting which is not fixed", func() {
		clusterOld := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:10.4",
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers": "4G",
					},
				},
			},
		}
		Expect(v.validateConfigurationChange(clusterNew, clusterOld)).To(BeEmpty())
	})

	It("complains when changing postgres major version and settings", func() {
		clusterOld := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:10.4",
			},
		}
		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:10.5",
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers": "4G",
					},
				},
			},
		}
		Expect(v.validateConfigurationChange(clusterNew, clusterOld)).To(HaveLen(1))
	})

	It("produces no error when WAL size settings are correct", func() {
		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "80MB",
						"max_wal_size": "1024",
					},
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(BeEmpty())

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "1500",
						"max_wal_size": "2 GB",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "3Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(BeEmpty())

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "1.5GB",
						"max_wal_size": "2000",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(BeEmpty())

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"max_wal_size": "1GB",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(BeEmpty())

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "100MB",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(BeEmpty())

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{},
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(BeEmpty())
	})

	It("produces one complaint when min_wal_size is bigger than max_wal_size", func() {
		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "1500",
						"max_wal_size": "1GB",
					},
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "2Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(1))

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "2G",
						"max_wal_size": "1GB",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "4Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(1))
	})

	It("produces one complaint when max_wal_size is bigger than WAL storage", func() {
		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"max_wal_size": "2GB",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "1G",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "4Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(1))

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "80MB",
						"max_wal_size": "1500",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "1G",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "4Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(1))
	})

	It("produces two complaints when min_wal_size is bigger than WAL storage and max_wal_size", func() {
		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "3GB",
						"max_wal_size": "1GB",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(2))
	})

	It("complains about invalid value for min_wal_size and max_wal_size", func() {
		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "xxx",
						"max_wal_size": "1GB",
					},
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(1))

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "80",
						"max_wal_size": "1Gb",
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(1))
	})

	It("doesn't compare default values for min_wal_size and max_wal_size with WalStorage", func() {
		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "100Mi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(BeEmpty())

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"min_wal_size": "1.5GB", // default for max_wal_size is 1GB
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(1))

		clusterNew = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"max_wal_size": "70M", // default for min_wal_size is 80M
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "4Gi",
				},
			},
		}
		Expect(v.validateConfiguration(clusterNew)).To(HaveLen(1))
	})

	It("should detect an invalid `shared_buffers` value", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"shared_buffers": "invalid",
					},
				},
			},
		}

		Expect(v.validateConfiguration(cluster)).To(HaveLen(1))
	})

	It("should reject minimal wal_level when backup is configured", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						BarmanCredentials: apiv1.BarmanCredentials{
							AWS: &apiv1.S3Credentials{},
						},
					},
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level":       "minimal",
						"max_wal_senders": "0",
					},
				},
			},
		}
		Expect(cluster.Spec.Backup.IsBarmanBackupConfigured()).To(BeTrue())
		Expect(v.validateConfiguration(cluster)).To(HaveLen(1))
	})

	It("should allow replica wal_level when backup is configured", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						BarmanCredentials: apiv1.BarmanCredentials{
							AWS: &apiv1.S3Credentials{},
						},
					},
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level": "replica",
					},
				},
			},
		}
		Expect(cluster.Spec.Backup.IsBarmanBackupConfigured()).To(BeTrue())
		Expect(v.validateConfiguration(cluster)).To(BeEmpty())
	})

	It("should allow logical wal_level when backup is configured", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{
						BarmanCredentials: apiv1.BarmanCredentials{
							AWS: &apiv1.S3Credentials{},
						},
					},
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level": "logical",
					},
				},
			},
		}
		Expect(cluster.Spec.Backup.IsBarmanBackupConfigured()).To(BeTrue())
		Expect(v.validateConfiguration(cluster)).To(BeEmpty())
	})

	It("should reject minimal wal_level when instances is greater than one", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 2,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level":       "minimal",
						"max_wal_senders": "0",
					},
				},
			},
		}

		Expect(v.validateConfiguration(cluster)).To(HaveLen(1))
	})

	It("should allow replica wal_level when instances is greater than one", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 2,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level": "replica",
					},
				},
			},
		}
		Expect(v.validateConfiguration(cluster)).To(BeEmpty())
	})

	It("should allow logical wal_level when instances is greater than one", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 2,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level": "logical",
					},
				},
			},
		}
		Expect(v.validateConfiguration(cluster)).To(BeEmpty())
	})

	It("should reject an unknown wal_level value", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level": "test",
					},
				},
			},
		}

		errs := v.validateConfiguration(cluster)
		Expect(errs).To(HaveLen(1))
		Expect(errs[0].Detail).To(ContainSubstring("unrecognized `wal_level` value - allowed values"))
	})

	It("should reject minimal if it is a replica cluster", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level":       "minimal",
						"max_wal_senders": "0",
					},
				},
			},
		}
		Expect(cluster.IsReplica()).To(BeTrue())
		Expect(v.validateConfiguration(cluster)).To(HaveLen(1))
	})

	It("should allow minimal wal_level with one instance and without archive mode", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.SkipWalArchiving: "enabled",
				},
			},
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level":       "minimal",
						"max_wal_senders": "0",
					},
				},
			},
		}
		Expect(v.validateConfiguration(cluster)).To(BeEmpty())
	})

	It("should disallow minimal wal_level with one instance, without max_wal_senders being specified", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.SkipWalArchiving: "enabled",
				},
			},
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level": "minimal",
					},
				},
			},
		}
		Expect(v.validateConfiguration(cluster)).To(HaveLen(1))
	})

	It("should disallow changing wal_level to minimal for existing clusters", func() {
		oldCluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.SkipWalArchiving: "enabled",
				},
			},
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"max_wal_senders": "0",
					},
				},
			},
		}
		oldCluster.Default()

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.SkipWalArchiving: "enabled",
				},
			},
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level":       "minimal",
						"max_wal_senders": "0",
					},
				},
			},
		}
		Expect(v.validateWALLevelChange(cluster, oldCluster)).To(HaveLen(1))
	})

	It("should allow retaining wal_level to minimal for existing clusters", func() {
		oldCluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.SkipWalArchiving: "enabled",
				},
			},
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level":       "minimal",
						"max_wal_senders": "0",
					},
				},
			},
		}
		oldCluster.Default()

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.SkipWalArchiving: "enabled",
				},
			},
			Spec: apiv1.ClusterSpec{
				Instances: 1,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"wal_level":       "minimal",
						"max_wal_senders": "0",
						"shared_buffers":  "512MB",
					},
				},
			},
		}
		Expect(v.validateWALLevelChange(cluster, oldCluster)).To(BeEmpty())
	})

	Describe("wal_log_hints", func() {
		It("should reject wal_log_hints set to an invalid value", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances: 1,
					PostgresConfiguration: apiv1.PostgresConfiguration{
						Parameters: map[string]string{
							"wal_log_hints": "foo",
						},
					},
				},
			}
			Expect(v.validateConfiguration(cluster)).To(HaveLen(1))
		})

		It("should allow wal_log_hints set to off for clusters having just one instance", func() {
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						utils.SkipWalArchiving: "enabled",
					},
				},
				Spec: apiv1.ClusterSpec{
					Instances: 1,
					PostgresConfiguration: apiv1.PostgresConfiguration{
						Parameters: map[string]string{
							"wal_log_hints": "off",
						},
					},
				},
			}
			Expect(v.validateConfiguration(cluster)).To(BeEmpty())
		})

		It("should not allow wal_log_hints set to off for clusters having more than one instance", func() {
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						utils.SkipWalArchiving: "enabled",
					},
				},
				Spec: apiv1.ClusterSpec{
					Instances: 3,
					PostgresConfiguration: apiv1.PostgresConfiguration{
						Parameters: map[string]string{
							"wal_log_hints": "off",
						},
					},
				},
			}
			Expect(v.validateConfiguration(cluster)).ToNot(BeEmpty())
		})

		It("should allow wal_log_hints set to on for clusters having just one instance", func() {
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						utils.SkipWalArchiving: "enabled",
					},
				},
				Spec: apiv1.ClusterSpec{
					Instances: 1,
					PostgresConfiguration: apiv1.PostgresConfiguration{
						Parameters: map[string]string{
							"wal_log_hints": "on",
						},
					},
				},
			}
			Expect(v.validateConfiguration(cluster)).To(BeEmpty())
		})

		It("should not allow wal_log_hints set to on for clusters having more than one instance", func() {
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						utils.SkipWalArchiving: "enabled",
					},
				},
				Spec: apiv1.ClusterSpec{
					Instances: 3,
					PostgresConfiguration: apiv1.PostgresConfiguration{
						Parameters: map[string]string{
							"wal_log_hints": "true",
						},
					},
				},
			}
			Expect(v.validateConfiguration(cluster)).To(BeEmpty())
		})
	})
})

var _ = Describe("validate image name change", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	Context("using image name", func() {
		It("doesn't complain with no changes", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(BeEmpty())
		})

		It("complains if it can't upgrade between mayor versions", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:17.0",
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:16.0",
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(HaveLen(1))
		})

		It("doesn't complain if image change is valid", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:17.1",
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:17.0",
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(BeEmpty())
		})
	})
	Context("using image catalog", func() {
		It("complains on major upgrades", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: 15,
					},
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: 16,
					},
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(HaveLen(1))
		})
	})
	Context("changing from imageName to imageCatalogRef", func() {
		It("doesn't complain when the major is the same", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:16.1",
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: 16,
					},
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(BeEmpty())
		})
		It("complains on major upgrades", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:16.1",
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: 17,
					},
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(HaveLen(1))
		})
		It("complains going from default imageName to different major imageCatalogRef", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: 16,
					},
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(HaveLen(1))
		})
		It("doesn't complain going from default imageName to same major imageCatalogRef", func() {
			defaultImageRef := reference.New(versions.DefaultImageName)
			version, err := pgversion.FromTag(defaultImageRef.Tag)
			Expect(err).ToNot(HaveOccurred())
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: int(version.Major()),
					},
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(BeEmpty())
		})
	})

	Context("changing from imageCatalogRef to imageName", func() {
		It("doesn't complain when the major is the same", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: 17,
					},
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:17.1",
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(BeEmpty())
		})
		It("complains on major upgrades", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: 16,
					},
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageName: "postgres:17.1",
				},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(HaveLen(1))
		})
		It("complains going from imageCatalogRef to different major default imageName", func() {
			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: 16,
					},
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(HaveLen(1))
		})
		It("doesn't complain going from imageCatalogRef to same major default imageName", func() {
			imageNameRef := reference.New(versions.DefaultImageName)
			version, err := pgversion.FromTag(imageNameRef.Tag)
			Expect(err).ToNot(HaveOccurred())

			clusterOld := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					ImageCatalogRef: &apiv1.ImageCatalogRef{
						TypedLocalObjectReference: corev1.TypedLocalObjectReference{
							Name: "test",
							Kind: "ImageCatalog",
						},
						Major: int(version.Major()),
					},
				},
			}
			clusterNew := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{},
			}
			Expect(v.validateImageChange(clusterNew, clusterOld)).To(BeEmpty())
		})
	})
})

var _ = Describe("recovery target", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("is mutually exclusive", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
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

		Expect(v.validateRecoveryTarget(cluster)).To(HaveLen(1))
	})

	It("Requires BackupID to perform PITR with TargetName", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
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

		Expect(v.validateRecoveryTarget(cluster)).To(BeEmpty())
	})

	It("Fails when no BackupID is provided to perform PITR with TargetXID", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
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

		Expect(v.validateRecoveryTarget(cluster)).To(HaveLen(1))
	})

	It("TargetTime's format as `YYYY-MM-DD HH24:MI:SS.FF6TZH` is valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
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

		Expect(v.validateRecoveryTarget(cluster)).To(BeEmpty())
	})

	It("TargetTime's format as YYYY-MM-DD HH24:MI:SS.FF6TZH:TZM` is valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
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

		Expect(v.validateRecoveryTarget(cluster)).To(BeEmpty())
	})

	It("TargetTime's format as YYYY-MM-DD HH24:MI:SS.FF6 TZH:TZM` is invalid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
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

		Expect(v.validateRecoveryTarget(cluster)).To(HaveLen(1))
	})

	It("raises errors for invalid LSN", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
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

		Expect(v.validateRecoveryTarget(cluster)).To(HaveLen(1))
	})

	It("valid LSN", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
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

		Expect(v.validateRecoveryTarget(cluster)).To(BeEmpty())
	})

	It("can be specified", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
							TargetTime: "2020-01-01 01:01:00",
						},
					},
				},
			},
		}

		Expect(v.validateRecoveryTarget(cluster)).To(BeEmpty())
	})

	When("recoveryTLI is specified", func() {
		It("allows 'latest'", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Bootstrap: &apiv1.BootstrapConfiguration{
						Recovery: &apiv1.BootstrapRecovery{
							RecoveryTarget: &apiv1.RecoveryTarget{
								TargetTLI: "latest",
							},
						},
					},
				},
			}
			Expect(v.validateRecoveryTarget(cluster)).To(BeEmpty())
		})

		It("allows a positive integer", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Bootstrap: &apiv1.BootstrapConfiguration{
						Recovery: &apiv1.BootstrapRecovery{
							RecoveryTarget: &apiv1.RecoveryTarget{
								TargetTLI: "23",
							},
						},
					},
				},
			}
			Expect(v.validateRecoveryTarget(cluster)).To(BeEmpty())
		})

		It("prevents 0 value", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Bootstrap: &apiv1.BootstrapConfiguration{
						Recovery: &apiv1.BootstrapRecovery{
							RecoveryTarget: &apiv1.RecoveryTarget{
								TargetTLI: "0",
							},
						},
					},
				},
			}
			Expect(v.validateRecoveryTarget(cluster)).To(HaveLen(1))
		})

		It("prevents negative values", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Bootstrap: &apiv1.BootstrapConfiguration{
						Recovery: &apiv1.BootstrapRecovery{
							RecoveryTarget: &apiv1.RecoveryTarget{
								TargetTLI: "-5",
							},
						},
					},
				},
			}
			Expect(v.validateRecoveryTarget(cluster)).To(HaveLen(1))
		})

		It("prevents everything else beside the empty string", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Bootstrap: &apiv1.BootstrapConfiguration{
						Recovery: &apiv1.BootstrapRecovery{
							RecoveryTarget: &apiv1.RecoveryTarget{
								TargetTLI: "I don't remember",
							},
						},
					},
				},
			}
			Expect(v.validateRecoveryTarget(cluster)).To(HaveLen(1))
		})
	})
})

var _ = Describe("primary update strategy", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("allows 'unsupervised'", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PrimaryUpdateStrategy: apiv1.PrimaryUpdateStrategyUnsupervised,
				Instances:             3,
			},
		}
		Expect(v.validatePrimaryUpdateStrategy(cluster)).To(BeEmpty())
	})

	It("allows 'supervised'", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PrimaryUpdateStrategy: apiv1.PrimaryUpdateStrategySupervised,
				Instances:             3,
			},
		}
		Expect(v.validatePrimaryUpdateStrategy(cluster)).To(BeEmpty())
	})

	It("prevents 'supervised' for single-instance clusters", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PrimaryUpdateStrategy: apiv1.PrimaryUpdateStrategySupervised,
				Instances:             1,
			},
		}
		Expect(v.validatePrimaryUpdateStrategy(cluster)).ToNot(BeEmpty())
	})

	It("allows 'unsupervised' for single-instance clusters", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PrimaryUpdateStrategy: apiv1.PrimaryUpdateStrategyUnsupervised,
				Instances:             1,
			},
		}
		Expect(v.validatePrimaryUpdateStrategy(cluster)).To(BeEmpty())
	})

	It("prevents everything else", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PrimaryUpdateStrategy: "maybe",
				Instances:             3,
			},
		}
		Expect(v.validatePrimaryUpdateStrategy(cluster)).ToNot(BeEmpty())
	})
})

var _ = Describe("Number of synchronous replicas", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	Context("new-style configuration", func() {
		It("can't have both new-style configuration and legacy one", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances:       3,
					MinSyncReplicas: 1,
					MaxSyncReplicas: 2,
					PostgresConfiguration: apiv1.PostgresConfiguration{
						Synchronous: &apiv1.SynchronousReplicaConfiguration{
							Number: 2,
						},
					},
				},
			}
			Expect(v.validateConfiguration(cluster)).ToNot(BeEmpty())
		})
	})

	Context("legacy configuration", func() {
		It("should be a positive integer", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances:       3,
					MaxSyncReplicas: -3,
				},
			}
			Expect(v.validateMaxSyncReplicas(cluster)).ToNot(BeEmpty())
		})

		It("should not be equal than the number of replicas", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances:       3,
					MaxSyncReplicas: 3,
				},
			}
			Expect(v.validateMaxSyncReplicas(cluster)).ToNot(BeEmpty())
		})

		It("should not be greater than the number of replicas", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances:       3,
					MaxSyncReplicas: 5,
				},
			}
			Expect(v.validateMaxSyncReplicas(cluster)).ToNot(BeEmpty())
		})

		It("can be zero", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances:       3,
					MaxSyncReplicas: 0,
				},
			}
			Expect(v.validateMaxSyncReplicas(cluster)).To(BeEmpty())
		})

		It("can be lower than the number of replicas", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Instances:       3,
					MaxSyncReplicas: 2,
				},
			}
			Expect(v.validateMaxSyncReplicas(cluster)).To(BeEmpty())
		})
	})
})

var _ = Describe("validateSynchronousReplicaConfiguration", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("returns no error when synchronous configuration is nil", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Synchronous: nil,
				},
			},
		}
		errors := v.validateSynchronousReplicaConfiguration(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("returns an error when number of synchronous replicas is greater than the total instances and standbys", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 2,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Synchronous: &apiv1.SynchronousReplicaConfiguration{
						Number:           5,
						StandbyNamesPost: []string{"standby1"},
						StandbyNamesPre:  []string{"standby2"},
					},
				},
			},
		}
		errors := v.validateSynchronousReplicaConfiguration(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(
			Equal("Invalid synchronous configuration: the number of synchronous replicas must be less than the " +
				"total number of instances and the provided standby names."))
	})

	It("returns an error when number of synchronous replicas is equal to total instances and standbys", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Synchronous: &apiv1.SynchronousReplicaConfiguration{
						Number:           5,
						StandbyNamesPost: []string{"standby1"},
						StandbyNamesPre:  []string{"standby2"},
					},
				},
			},
		}
		errors := v.validateSynchronousReplicaConfiguration(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Invalid synchronous configuration: the number of synchronous replicas " +
			"must be less than the total number of instances and the provided standby names."))
	})

	It("returns no error when number of synchronous replicas is less than total instances and standbys", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Instances: 2,
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Synchronous: &apiv1.SynchronousReplicaConfiguration{
						Number:           2,
						StandbyNamesPost: []string{"standby1"},
						StandbyNamesPre:  []string{"standby2"},
					},
				},
			},
		}
		errors := v.validateSynchronousReplicaConfiguration(cluster)
		Expect(errors).To(BeEmpty())
	})
})

var _ = Describe("storage configuration validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complains if the size is being reduced", func() {
		clusterOld := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "1G",
				},
			},
		}

		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "512M",
				},
			},
		}

		Expect(v.validateStorageChange(clusterNew, clusterOld)).ToNot(BeEmpty())
	})

	It("does not complain if nothing has been changed", func() {
		one := "one"
		clusterOld := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size:         "1G",
					StorageClass: &one,
				},
			},
		}

		clusterNew := clusterOld.DeepCopy()

		Expect(v.validateStorageChange(clusterNew, clusterOld)).To(BeEmpty())
	})

	It("works fine is the size is being enlarged", func() {
		clusterOld := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "8G",
				},
			},
		}

		clusterNew := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10G",
				},
			},
		}

		Expect(v.validateStorageChange(clusterNew, clusterOld)).To(BeEmpty())
	})
})

var _ = Describe("Cluster name validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("should be a valid DNS label", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test.one",
			},
		}
		Expect(v.validateName(cluster)).ToNot(BeEmpty())
	})

	It("should not be too long", func() {
		cluster := &apiv1.Cluster{
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
		Expect(v.validateName(cluster)).ToNot(BeEmpty())
	})

	It("should not raise errors when the name is ok", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "abcdefghi" +
					"abcdefghi" +
					"abcdefghi" +
					"abcdefghi",
			},
		}
		Expect(v.validateName(cluster)).To(BeEmpty())
	})

	It("should return errors when the name is not DNS-1035 compliant", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "4b96d026-a956-47eb-bae8-a99b840805c3",
			},
		}
		Expect(v.validateName(cluster)).NotTo(BeEmpty())
	})

	It("should return errors when the name length is greater than 50", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: strings.Repeat("toomuchlong", 4) + "-" + "after4times",
			},
		}
		Expect(v.validateName(cluster)).NotTo(BeEmpty())
	})

	It("should return errors when having a name with dots", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "wrong.name",
			},
		}
		Expect(v.validateName(cluster)).NotTo(BeEmpty())
	})
})

var _ = Describe("validation of the list of external clusters", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("is correct when it's empty", func() {
		cluster := &apiv1.Cluster{}
		Expect(v.validateExternalClusters(cluster)).To(BeEmpty())
	})

	It("complains when the list of clusters contains duplicates", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
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
		Expect(v.validateExternalClusters(cluster)).ToNot(BeEmpty())
	})

	It("should not raise errors is the cluster name is unique", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
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
		Expect(v.validateExternalClusters(cluster)).To(BeEmpty())
	})
})

var _ = Describe("validation of an external cluster", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("ensure that one of connectionParameters and barmanObjectStore is set", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
					{},
				},
			},
		}
		Expect(v.validateExternalClusters(cluster)).To(Not(BeEmpty()))

		cluster.Spec.ExternalClusters[0].ConnectionParameters = map[string]string{
			"dbname": "postgres",
		}
		cluster.Spec.ExternalClusters[0].BarmanObjectStore = nil
		Expect(v.validateExternalClusters(cluster)).To(BeEmpty())

		cluster.Spec.ExternalClusters[0].ConnectionParameters = nil
		cluster.Spec.ExternalClusters[0].BarmanObjectStore = &apiv1.BarmanObjectStoreConfiguration{}
		Expect(v.validateExternalClusters(cluster)).To(BeEmpty())
	})
})

var _ = Describe("bootstrap base backup validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complains if you specify the database name but not the owner for pg_basebackup", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
						Database: "app",
					},
				},
			},
		}

		result := v.validatePgBaseBackupApplicationDatabase(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("complains if you specify the owner but not the database name for pg_basebackup", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
						Owner: "app",
					},
				},
			},
		}

		result := v.validatePgBaseBackupApplicationDatabase(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if you specify both database name and owner user for pg_basebackup", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
						Database: "app",
						Owner:    "app",
					},
				},
			},
		}

		result := v.validatePgBaseBackupApplicationDatabase(cluster)
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we are not bootstrapping using pg_basebackup", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{},
			},
		}
		result := v.validateBootstrapPgBaseBackupSource(recoveryCluster)
		Expect(result).To(BeEmpty())
	})

	It("complain when the source cluster doesn't exist", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
						Source: "test",
					},
				},
			},
		}
		result := v.validateBootstrapPgBaseBackupSource(recoveryCluster)
		Expect(result).ToNot(BeEmpty())
	})
})

var _ = Describe("bootstrap recovery validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complains if you specify the database name but not the owner for recovery", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Database: "app",
					},
				},
			},
		}

		result := v.validateRecoveryApplicationDatabase(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("complains if you specify the owner but not the database name for recovery", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Owner: "app",
					},
				},
			},
		}

		result := v.validateRecoveryApplicationDatabase(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("doesn't complain if you specify both database name and owner user for recovery", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Database: "app",
						Owner:    "app",
					},
				},
			},
		}

		result := v.validateRecoveryApplicationDatabase(cluster)
		Expect(result).To(BeEmpty())
	})

	Context("does not complain when bootstrap recovery source matches one of the names of external clusters", func() {
		When("using a barman object store configuration", func() {
			recoveryCluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Bootstrap: &apiv1.BootstrapConfiguration{
						Recovery: &apiv1.BootstrapRecovery{
							Source: "test",
						},
					},
					ExternalClusters: []apiv1.ExternalCluster{
						{
							Name:              "test",
							BarmanObjectStore: &api.BarmanObjectStoreConfiguration{},
						},
					},
				},
			}
			errorsList := v.validateBootstrapRecoverySource(recoveryCluster)
			Expect(errorsList).To(BeEmpty())
		})

		When("using a plugin configuration", func() {
			recoveryCluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Bootstrap: &apiv1.BootstrapConfiguration{
						Recovery: &apiv1.BootstrapRecovery{
							Source: "test",
						},
					},
					ExternalClusters: []apiv1.ExternalCluster{
						{
							Name:                "test",
							PluginConfiguration: &apiv1.PluginConfiguration{},
						},
					},
				},
			}
			errorsList := v.validateBootstrapRecoverySource(recoveryCluster)
			Expect(errorsList).To(BeEmpty())
		})
	})

	It("complains when bootstrap recovery source does not match one of the names of external clusters", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Source: "test",
					},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "another-test",
					},
				},
			},
		}
		errorsList := v.validateBootstrapRecoverySource(recoveryCluster)
		Expect(errorsList).ToNot(BeEmpty())
	})

	It("complains when bootstrap recovery source have no BarmanObjectStore nor plugin configuration", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Source: "test",
					},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		errorsList := v.validateBootstrapRecoverySource(recoveryCluster)
		Expect(errorsList).To(HaveLen(1))
	})
})

var _ = Describe("toleration validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain if we provide a proper toleration", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{
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
		result := v.validateTolerations(recoveryCluster)
		Expect(result).To(BeEmpty())
	})

	It("complain when the toleration ", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{
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
		result := v.validateTolerations(recoveryCluster)
		Expect(result).ToNot(BeEmpty())
	})
})

var _ = Describe("validate anti-affinity", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain if we provide an empty affinity section", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{},
			},
		}
		result := v.validateAntiAffinity(cluster)
		Expect(result).To(BeEmpty())
	})
	It("doesn't complain if we provide a proper PodAntiAffinity with anti-affinity enabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: ptr.To(true),
					PodAntiAffinityType:   "required",
				},
			},
		}
		result := v.validateAntiAffinity(cluster)
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we provide a proper PodAntiAffinity with anti-affinity disabled", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: ptr.To(false),
					PodAntiAffinityType:   "required",
				},
			},
		}
		result := v.validateAntiAffinity(recoveryCluster)
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain if we provide a proper PodAntiAffinity with anti-affinity enabled", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: ptr.To(true),
					PodAntiAffinityType:   "preferred",
				},
			},
		}
		result := v.validateAntiAffinity(recoveryCluster)
		Expect(result).To(BeEmpty())
	})
	It("doesn't complain if we provide a proper PodAntiAffinity default with anti-affinity enabled", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: ptr.To(true),
					PodAntiAffinityType:   "",
				},
			},
		}
		result := v.validateAntiAffinity(recoveryCluster)
		Expect(result).To(BeEmpty())
	})

	It("complains if we provide a wrong PodAntiAffinity with anti-affinity disabled", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: ptr.To(false),
					PodAntiAffinityType:   "error",
				},
			},
		}
		result := v.validateAntiAffinity(recoveryCluster)
		Expect(result).NotTo(BeEmpty())
	})

	It("complains if we provide a wrong PodAntiAffinity with anti-affinity enabled", func() {
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Affinity: apiv1.AffinityConfiguration{
					EnablePodAntiAffinity: ptr.To(true),
					PodAntiAffinityType:   "error",
				},
			},
		}
		result := v.validateAntiAffinity(recoveryCluster)
		Expect(result).NotTo(BeEmpty())
	})
})

var _ = Describe("validation of the list of external clusters", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("is correct when it's empty", func() {
		cluster := &apiv1.Cluster{}
		Expect(v.validateExternalClusters(cluster)).To(BeEmpty())
	})

	It("complains when the list of servers contains duplicates", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
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
		Expect(v.validateExternalClusters(cluster)).ToNot(BeEmpty())
	})

	It("should not raise errors is the server name is unique", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ExternalClusters: []apiv1.ExternalCluster{
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
		Expect(v.validateExternalClusters(cluster)).To(BeEmpty())
	})
})

var _ = Describe("bootstrap base backup validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complain when the source cluster doesn't exist", func() {
		bootstrap := apiv1.BootstrapConfiguration{}
		bpb := apiv1.BootstrapPgBaseBackup{Source: "test"}
		bootstrap.PgBaseBackup = &bpb
		recoveryCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{
						Source: "test",
					},
				},
			},
		}
		result := v.validateBootstrapPgBaseBackupSource(recoveryCluster)
		Expect(result).ToNot(BeEmpty())
	})
})

var _ = Describe("unix permissions identifiers change validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complains if the PostgresGID is changed", func() {
		oldCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresGID: apiv1.DefaultPostgresGID,
			},
		}
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresGID: 53,
			},
		}
		Expect(v.validateUnixPermissionIdentifierChange(cluster, oldCluster)).NotTo(BeEmpty())
	})

	It("complains if the PostgresUID is changed", func() {
		oldCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresUID: apiv1.DefaultPostgresUID,
			},
		}
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresGID: 74,
			},
		}
		Expect(v.validateUnixPermissionIdentifierChange(cluster, oldCluster)).NotTo(BeEmpty())
	})

	It("should not complain if the values havn't been changed", func() {
		oldCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresUID: 74,
				PostgresGID: 76,
			},
		}
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresUID: 74,
				PostgresGID: 76,
			},
		}
		Expect(v.validateUnixPermissionIdentifierChange(cluster, oldCluster)).To(BeEmpty())
	})
})

var _ = Describe("promotion token validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complains if the replica token is not formatted in base64", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled:        ptr.To(false),
					Source:         "test",
					PromotionToken: "this-is-a-wrong-token",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}

		result := v.validatePromotionToken(cluster)
		Expect(result).ToNot(BeEmpty())
	})

	It("complains if the replica token is not valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled:        ptr.To(false),
					Source:         "test",
					PromotionToken: base64.StdEncoding.EncodeToString([]byte("{}")),
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}

		result := v.validatePromotionToken(cluster)
		Expect(result).ToNot(BeEmpty())
	})

	It("doesn't complain if the replica token is valid", func() {
		tokenContent := utils.PgControldataTokenContent{
			LatestCheckpointTimelineID:   "3",
			REDOWALFile:                  "this-wal-file",
			DatabaseSystemIdentifier:     "231231212",
			LatestCheckpointREDOLocation: "33322232",
			TimeOfLatestCheckpoint:       "we don't know",
			OperatorVersion:              "version info",
		}
		jsonToken, err := json.Marshal(tokenContent)
		Expect(err).ToNot(HaveOccurred())

		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled:        ptr.To(false),
					Source:         "test",
					PromotionToken: base64.StdEncoding.EncodeToString(jsonToken),
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}

		result := v.validatePromotionToken(cluster)
		Expect(result).To(BeEmpty())
	})

	It("complains if the token is set on a replica cluster (enabled)", func() {
		tokenContent := utils.PgControldataTokenContent{
			LatestCheckpointTimelineID:   "1",
			REDOWALFile:                  "0000000100000001000000A1",
			DatabaseSystemIdentifier:     "231231212",
			LatestCheckpointREDOLocation: "0/1000000",
			TimeOfLatestCheckpoint:       "we don't know",
			OperatorVersion:              "version info",
		}
		jsonToken, err := json.Marshal(tokenContent)
		Expect(err).ToNot(HaveOccurred())

		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled:        ptr.To(true),
					Source:         "test",
					PromotionToken: base64.StdEncoding.EncodeToString(jsonToken),
				},
			},
		}

		result := v.validatePromotionToken(cluster)
		Expect(result).NotTo(BeEmpty())
	})

	It("complains if the token is set on a replica cluster (primary, default name)", func() {
		tokenContent := utils.PgControldataTokenContent{
			LatestCheckpointTimelineID:   "1",
			REDOWALFile:                  "0000000100000001000000A1",
			DatabaseSystemIdentifier:     "231231212",
			LatestCheckpointREDOLocation: "0/1000000",
			TimeOfLatestCheckpoint:       "we don't know",
			OperatorVersion:              "version info",
		}
		jsonToken, err := json.Marshal(tokenContent)
		Expect(err).ToNot(HaveOccurred())

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test2",
			},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary:        "test",
					Source:         "test",
					PromotionToken: base64.StdEncoding.EncodeToString(jsonToken),
				},
			},
		}

		result := v.validatePromotionToken(cluster)
		Expect(result).NotTo(BeEmpty())
	})

	It("complains if the token is set on a replica cluster (primary, self)", func() {
		tokenContent := utils.PgControldataTokenContent{
			LatestCheckpointTimelineID:   "1",
			REDOWALFile:                  "0000000100000001000000A1",
			DatabaseSystemIdentifier:     "231231212",
			LatestCheckpointREDOLocation: "0/1000000",
			TimeOfLatestCheckpoint:       "we don't know",
			OperatorVersion:              "version info",
		}
		jsonToken, err := json.Marshal(tokenContent)
		Expect(err).ToNot(HaveOccurred())

		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary:        "test",
					Self:           "test2",
					Source:         "test",
					PromotionToken: base64.StdEncoding.EncodeToString(jsonToken),
				},
			},
		}

		result := v.validatePromotionToken(cluster)
		Expect(result).NotTo(BeEmpty())
	})

	It("complains it the token is set when minApplyDelay is being used", func() {
		tokenContent := utils.PgControldataTokenContent{
			LatestCheckpointTimelineID:   "1",
			REDOWALFile:                  "0000000100000001000000A1",
			DatabaseSystemIdentifier:     "231231212",
			LatestCheckpointREDOLocation: "0/1000000",
			TimeOfLatestCheckpoint:       "we don't know",
			OperatorVersion:              "version info",
		}
		jsonToken, err := json.Marshal(tokenContent)
		Expect(err).ToNot(HaveOccurred())

		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary:        "test",
					Self:           "test",
					Source:         "test",
					PromotionToken: base64.StdEncoding.EncodeToString(jsonToken),
					MinApplyDelay: &metav1.Duration{
						Duration: 1 * time.Hour,
					},
				},
			},
		}

		result := v.validatePromotionToken(cluster)
		Expect(result).NotTo(BeEmpty())
	})
})

var _ = Describe("replica mode validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complains if the bootstrap method is not specified", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "test",
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		Expect(v.validateReplicaMode(cluster)).ToNot(BeEmpty())
	})

	It("complains if the initdb bootstrap method is used", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		Expect(v.validateReplicaMode(cluster)).ToNot(BeEmpty())
	})

	It("doesn't complain about initdb if we enable the external cluster on an existing cluster", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				ResourceVersion: "existing",
			},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		result := v.validateReplicaMode(cluster)
		Expect(result).To(BeEmpty())
	})

	It("should complain if enabled is set to off during a transition", func() {
		old := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				ResourceVersion: "existing",
			},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
			Status: apiv1.ClusterStatus{
				SwitchReplicaClusterStatus: apiv1.SwitchReplicaClusterStatus{
					InProgress: true,
				},
			},
		}

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				ResourceVersion: "existing",
			},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(false),
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
			Status: apiv1.ClusterStatus{
				SwitchReplicaClusterStatus: apiv1.SwitchReplicaClusterStatus{
					InProgress: true,
				},
			},
		}

		result := v.validateReplicaClusterChange(cluster, old)
		Expect(result).To(HaveLen(1))
		Expect(result[0].Type).To(Equal(field.ErrorTypeForbidden))
		Expect(result[0].Field).To(Equal("spec.replica.enabled"))
	})

	It("is valid when the pg_basebackup bootstrap option is used", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		result := v.validateReplicaMode(cluster)
		Expect(result).To(BeEmpty())
	})

	It("is valid when the restore bootstrap option is used", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		result := v.validateReplicaMode(cluster)
		Expect(result).To(BeEmpty())
	})

	It("complains when the primary field is used with the enabled field", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Primary: "toast",
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{},
				},
				ExternalClusters: []apiv1.ExternalCluster{},
			},
		}
		result := v.validateReplicaMode(cluster)
		Expect(result).ToNot(BeEmpty())
	})

	It("doesn't complain when the enabled field is not specified", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-2",
			},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary: "test",
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		result := v.validateReplicaMode(cluster)
		Expect(result).To(BeEmpty())
	})

	It("doesn't complain when creating a new primary cluster with the replication stanza set", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary: "test",
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}
		result := v.validateReplicaMode(cluster)
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("validate the replica cluster external clusters", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complains when the external cluster doesn't exist (source)", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{},
				},
				ExternalClusters: []apiv1.ExternalCluster{},
			},
		}

		cluster.Spec.Bootstrap.PgBaseBackup = nil
		result := v.validateReplicaClusterExternalClusters(cluster)
		Expect(result).ToNot(BeEmpty())
	})

	It("complains when the external cluster doesn't exist (primary)", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Primary: "test2",
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}

		result := v.validateReplicaClusterExternalClusters(cluster)
		Expect(result).ToNot(BeEmpty())
	})

	It("complains when the external cluster doesn't exist (self)", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Self:    "test2",
					Primary: "test",
					Source:  "test",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					PgBaseBackup: &apiv1.BootstrapPgBaseBackup{},
				},
				ExternalClusters: []apiv1.ExternalCluster{
					{
						Name: "test",
					},
				},
			},
		}

		result := v.validateReplicaClusterExternalClusters(cluster)
		Expect(result).ToNot(BeEmpty())
	})
})

var _ = Describe("Validation changes", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain if given old cluster is nil", func() {
		newCluster := &apiv1.Cluster{}
		err := v.validateClusterChanges(newCluster, nil)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("Backup validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("complain if there's no credentials", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{},
				},
			},
		}
		err := v.validateBackupConfiguration(cluster)
		Expect(err).To(HaveLen(1))
	})
})

var _ = Describe("Backup retention policy validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("doesn't complain if given policy is not provided", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{},
			},
		}
		err := v.validateRetentionPolicy(cluster)
		Expect(err).To(BeEmpty())
	})

	It("doesn't complain if given policy is valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					RetentionPolicy: "90d",
				},
			},
		}
		err := v.validateRetentionPolicy(cluster)
		Expect(err).To(BeEmpty())
	})

	It("complain if a given policy is not valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					RetentionPolicy: "09",
				},
			},
		}
		err := v.validateRetentionPolicy(cluster)
		Expect(err).To(HaveLen(1))
	})
})

var _ = Describe("validation of imports", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("rejects unrecognized import type", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type: "fooBar",
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("rejects microservice import with roles", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MicroserviceSnapshotType,
							Databases: []string{"foo"},
							Roles:     []string{"bar"},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("rejects microservice import without exactly one database", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MicroserviceSnapshotType,
							Databases: []string{"foo", "bar"},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("rejects microservice import with a wildcard on the database name", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MicroserviceSnapshotType,
							Databases: []string{"*foo"},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("accepts microservice import when well specified", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MicroserviceSnapshotType,
							Databases: []string{"foo"},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(BeEmpty())
	})

	It("rejects monolith import with no databases", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MonolithSnapshotType,
							Databases: []string{},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("rejects monolith import with PostImport Application SQL", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:                     apiv1.MonolithSnapshotType,
							Databases:                []string{"foo"},
							PostImportApplicationSQL: []string{"select * from bar"},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("rejects monolith import with wildcards alongside specific values", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MonolithSnapshotType,
							Databases: []string{"bar", "*"},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(HaveLen(1))

		cluster = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MonolithSnapshotType,
							Databases: []string{"foo"},
							Roles:     []string{"baz", "*"},
						},
					},
				},
			},
		}

		result = v.validateImport(cluster)
		Expect(result).To(HaveLen(1))
	})

	It("accepts monolith import with proper values", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MonolithSnapshotType,
							Databases: []string{"foo"},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(BeEmpty())
	})

	It("accepts monolith import with wildcards", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						Database: "app",
						Owner:    "app",
						Import: &apiv1.Import{
							Type:      apiv1.MonolithSnapshotType,
							Databases: []string{"*"},
							Roles:     []string{"*"},
						},
					},
				},
			},
		}

		result := v.validateImport(cluster)
		Expect(result).To(BeEmpty())
	})
})

var _ = Describe("validation of replication slots configuration", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("can be enabled on the default PostgreSQL image", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
					UpdateInterval: 0,
				},
			},
		}
		cluster.Default()

		result := v.validateReplicationSlots(cluster)
		Expect(result).To(BeEmpty())
	})

	It("set replicationSlots by default", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
			},
		}
		cluster.Default()
		Expect(cluster.Spec.ReplicationSlots).ToNot(BeNil())
		Expect(cluster.Spec.ReplicationSlots.HighAvailability).ToNot(BeNil())
		Expect(cluster.Spec.ReplicationSlots.HighAvailability.Enabled).To(HaveValue(BeTrue()))

		result := v.validateReplicationSlots(cluster)
		Expect(result).To(BeEmpty())
	})

	It("set replicationSlots.highAvailability by default", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					UpdateInterval: 30,
				},
			},
		}
		cluster.Default()
		Expect(cluster.Spec.ReplicationSlots.HighAvailability).ToNot(BeNil())
		Expect(cluster.Spec.ReplicationSlots.HighAvailability.Enabled).To(HaveValue(BeTrue()))

		result := v.validateReplicationSlots(cluster)
		Expect(result).To(BeEmpty())
	})

	It("allows enabling replication slots on the fly", func() {
		oldCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(false),
					},
				},
			},
		}
		oldCluster.Default()

		newCluster := oldCluster.DeepCopy()
		newCluster.Spec.ReplicationSlots = &apiv1.ReplicationSlotsConfiguration{
			HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
				Enabled:    ptr.To(true),
				SlotPrefix: "_test_",
			},
		}

		Expect(v.validateReplicationSlotsChange(newCluster, oldCluster)).To(BeEmpty())
	})

	It("prevents changing the slot prefix while replication slots are enabled", func() {
		oldCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled:    ptr.To(true),
						SlotPrefix: "_test_",
					},
				},
			},
		}
		oldCluster.Default()

		newCluster := oldCluster.DeepCopy()
		newCluster.Spec.ReplicationSlots.HighAvailability.SlotPrefix = "_toast_"
		Expect(v.validateReplicationSlotsChange(newCluster, oldCluster)).To(HaveLen(1))
	})

	It("prevents removing the replication slot section when replication slots are enabled", func() {
		oldCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled:    ptr.To(true),
						SlotPrefix: "_test_",
					},
				},
			},
		}
		oldCluster.Default()

		newCluster := oldCluster.DeepCopy()
		newCluster.Spec.ReplicationSlots = nil
		Expect(v.validateReplicationSlotsChange(newCluster, oldCluster)).To(HaveLen(1))
	})

	It("allows disabling the replication slots", func() {
		oldCluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled:    ptr.To(true),
						SlotPrefix: "_test_",
					},
				},
			},
		}
		oldCluster.Default()

		newCluster := oldCluster.DeepCopy()
		newCluster.Spec.ReplicationSlots.HighAvailability.Enabled = ptr.To(false)
		Expect(v.validateReplicationSlotsChange(newCluster, oldCluster)).To(BeEmpty())
	})

	It("should return an error when SynchronizeReplicasConfiguration is not nil and has invalid regex", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					SynchronizeReplicas: &apiv1.SynchronizeReplicasConfiguration{
						ExcludePatterns: []string{"([a-zA-Z]+"},
					},
				},
			},
		}
		errors := v.validateReplicationSlots(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Cannot configure synchronizeReplicas. Invalid regexes were found"))
	})

	It("should not return an error when SynchronizeReplicasConfiguration is not nil and regex is valid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					SynchronizeReplicas: &apiv1.SynchronizeReplicasConfiguration{
						ExcludePatterns: []string{"validpattern"},
					},
				},
			},
		}
		errors := v.validateReplicationSlots(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("should not return an error when SynchronizeReplicasConfiguration is nil", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ImageName: versions.DefaultImageName,
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					SynchronizeReplicas: nil,
				},
			},
		}
		errors := v.validateReplicationSlots(cluster)
		Expect(errors).To(BeEmpty())
	})
})

var _ = Describe("Environment variables validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

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
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Env: []corev1.EnvVar{
						{
							Name:  "TZ",
							Value: "Europe/Rome",
						},
					},
				},
			}

			Expect(v.validateEnv(cluster)).To(BeEmpty())
		})

		It("detects if the environment variable list contains a reserved variable", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
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

			Expect(v.validateEnv(cluster)).To(HaveLen(1))
		})
	})
})

var _ = Describe("Storage configuration validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	When("a ClusterSpec is given", func() {
		It("produces one error if storage is not set at all", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{},
				},
			}
			Expect(v.validateStorageSize(cluster)).To(HaveLen(1))
		})

		It("succeeds if storage size is set", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "1G",
					},
				},
			}
			Expect(v.validateStorageSize(cluster)).To(BeEmpty())
		})

		It("succeeds if storage is not set but a pvc template specifies storage", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						PersistentVolumeClaimTemplate: &corev1.PersistentVolumeClaimSpec{
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{"storage": resource.MustParse("1Gi")},
							},
						},
					},
				},
			}
			Expect(v.validateStorageSize(cluster)).To(BeEmpty())
		})
	})
})

var _ = Describe("Ephemeral volume configuration validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("succeeds if no ephemeral configuration is present", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{},
		}
		Expect(v.validateEphemeralVolumeSource(cluster)).To(BeEmpty())
	})

	It("succeeds if ephemeralVolumeSource is set", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				EphemeralVolumeSource: &corev1.EphemeralVolumeSource{},
			},
		}
		Expect(v.validateEphemeralVolumeSource(cluster)).To(BeEmpty())
	})

	It("succeeds if ephemeralVolumesSizeLimit.temporaryData is set", func() {
		onegi := resource.MustParse("1Gi")
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				EphemeralVolumesSizeLimit: &apiv1.EphemeralVolumesSizeLimitConfiguration{
					TemporaryData: &onegi,
				},
			},
		}
		Expect(v.validateEphemeralVolumeSource(cluster)).To(BeEmpty())
	})

	It("succeeds if ephemeralVolumeSource and ephemeralVolumesSizeLimit.shm are set", func() {
		onegi := resource.MustParse("1Gi")
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				EphemeralVolumeSource: &corev1.EphemeralVolumeSource{},
				EphemeralVolumesSizeLimit: &apiv1.EphemeralVolumesSizeLimitConfiguration{
					Shm: &onegi,
				},
			},
		}
		Expect(v.validateEphemeralVolumeSource(cluster)).To(BeEmpty())
	})

	It("produces one error if conflicting ephemeral storage options are set", func() {
		onegi := resource.MustParse("1Gi")
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				EphemeralVolumeSource: &corev1.EphemeralVolumeSource{},
				EphemeralVolumesSizeLimit: &apiv1.EphemeralVolumesSizeLimitConfiguration{
					TemporaryData: &onegi,
				},
			},
		}
		Expect(v.validateEphemeralVolumeSource(cluster)).To(HaveLen(1))
	})
})

var _ = Describe("Role management validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("should succeed if there is no management stanza", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{},
		}
		Expect(v.validateManagedRoles(cluster)).To(BeEmpty())
	})

	It("should succeed if the role defined is not reserved", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Roles: []apiv1.RoleConfiguration{
						{
							Name: "non-conflicting",
						},
					},
				},
			},
		}
		Expect(v.validateManagedRoles(cluster)).To(BeEmpty())
	})

	It("should produce an error on invalid connection limit", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Roles: []apiv1.RoleConfiguration{
						{
							Name:            "non-conflicting",
							ConnectionLimit: -3,
						},
					},
				},
			},
		}
		Expect(v.validateManagedRoles(cluster)).To(HaveLen(1))
	})

	It("should produce an error if the role is reserved", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Roles: []apiv1.RoleConfiguration{
						{
							Name: "postgres",
						},
					},
				},
			},
		}
		Expect(v.validateManagedRoles(cluster)).To(HaveLen(1))
	})

	It("should produce two errors if the role is reserved and the connection limit is invalid", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Roles: []apiv1.RoleConfiguration{
						{
							Name:            "postgres",
							ConnectionLimit: -3,
						},
					},
				},
			},
		}
		Expect(v.validateManagedRoles(cluster)).To(HaveLen(2))
	})

	It("should produce an error if we define two roles with the same name", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Roles: []apiv1.RoleConfiguration{
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
		Expect(v.validateManagedRoles(cluster)).To(HaveLen(1))
	})
	It("should produce an error if we have a password secret AND DisablePassword in a role", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Roles: []apiv1.RoleConfiguration{
						{
							Name:            "my_test",
							Superuser:       true,
							BypassRLS:       true,
							DisablePassword: true,
							PasswordSecret: &apiv1.LocalObjectReference{
								Name: "myPassword",
							},
							ConnectionLimit: -1,
						},
					},
				},
			},
		}
		Expect(v.validateManagedRoles(cluster)).To(HaveLen(1))
	})
})

var _ = Describe("Managed Extensions validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("should succeed if no extension is enabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{},
		}
		Expect(v.validateManagedExtensions(cluster)).To(BeEmpty())
	})

	It("should fail if hot_standby_feedback is set to an invalid value", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"hot_standby_feedback":                     "foo",
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(v.validatePgFailoverSlots(cluster)).To(HaveLen(2))
	})

	It("should succeed if pg_failover_slots and its prerequisites are enabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"hot_standby_feedback":                     "on",
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(v.validatePgFailoverSlots(cluster)).To(BeEmpty())
	})

	It("should produce two errors if pg_failover_slots is enabled and its prerequisites are disabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(v.validatePgFailoverSlots(cluster)).To(HaveLen(2))
	})

	It("should produce an error if pg_failover_slots is enabled and HA slots are disabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"hot_standby_feedback":                     "yes",
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(v.validatePgFailoverSlots(cluster)).To(HaveLen(1))
	})

	It("should produce an error if pg_failover_slots is enabled and hot_standby_feedback is disabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled: ptr.To(true),
					},
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"pg_failover_slots.synchronize_slot_names": "my_slot",
					},
				},
			},
		}
		Expect(v.validatePgFailoverSlots(cluster)).To(HaveLen(1))
	})
})

var _ = Describe("Recovery from volume snapshot validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	clusterFromRecovery := func(recovery *apiv1.BootstrapRecovery) *apiv1.Cluster {
		return &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: recovery,
				},
				WalStorage: &apiv1.StorageConfiguration{},
			},
		}
	}

	It("should produce an error when defining two recovery sources at the same time", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Source:          "sourceName",
						Backup:          &apiv1.BackupSource{},
						VolumeSnapshots: &apiv1.DataSource{},
					},
				},
			},
		}
		Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(HaveLen(1))
	})

	It("should produce an error when defining a backupID while recovering using a DataSource", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						RecoveryTarget: &apiv1.RecoveryTarget{
							BackupID: "20220616T031500",
						},
						VolumeSnapshots: &apiv1.DataSource{
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
		Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(HaveLen(1))
	})

	It("should produce an error when asking to recovery WALs from a snapshot without having storage for it", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						VolumeSnapshots: &apiv1.DataSource{
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
					},
				},
			},
		}
		Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(HaveLen(1))
	})

	It("should not produce an error when the configuration is sound", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						VolumeSnapshots: &apiv1.DataSource{
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
					},
				},
				WalStorage: &apiv1.StorageConfiguration{},
			},
		}
		Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(BeEmpty())
	})

	It("accepts recovery from a VolumeSnapshot", func() {
		cluster := clusterFromRecovery(&apiv1.BootstrapRecovery{
			VolumeSnapshots: &apiv1.DataSource{
				Storage: corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(storagesnapshotv1.GroupName),
					Kind:     apiv1.VolumeSnapshotKind,
					Name:     "pgdata",
				},
				WalStorage: &corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(storagesnapshotv1.GroupName),
					Kind:     apiv1.VolumeSnapshotKind,
					Name:     "pgwal",
				},
			},
		})
		Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(BeEmpty())
	})

	It("accepts recovery from a VolumeSnapshot, while restoring WALs from an object store", func() {
		cluster := clusterFromRecovery(&apiv1.BootstrapRecovery{
			VolumeSnapshots: &apiv1.DataSource{
				Storage: corev1.TypedLocalObjectReference{
					APIGroup: ptr.To(storagesnapshotv1.GroupName),
					Kind:     apiv1.VolumeSnapshotKind,
					Name:     "pgdata",
				},
			},

			Source: "pg-cluster",
		})
		Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(BeEmpty())
	})

	When("using an nil apiGroup", func() {
		It("accepts recovery from a PersistentVolumeClaim", func() {
			cluster := clusterFromRecovery(&apiv1.BootstrapRecovery{
				VolumeSnapshots: &apiv1.DataSource{
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
			Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(BeEmpty())
		})
	})

	When("using an empty apiGroup", func() {
		It("accepts recovery from a PersistentVolumeClaim", func() {
			cluster := clusterFromRecovery(&apiv1.BootstrapRecovery{
				VolumeSnapshots: &apiv1.DataSource{
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
			Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(BeEmpty())
		})
	})

	It("prevent recovery from other Objects", func() {
		cluster := clusterFromRecovery(&apiv1.BootstrapRecovery{
			VolumeSnapshots: &apiv1.DataSource{
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
		Expect(v.validateBootstrapRecoveryDataSource(cluster)).To(HaveLen(2))
	})
})

var _ = Describe("validateResources", func() {
	var cluster *apiv1.Cluster
	var v *ClusterCustomValidator

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{},
				},
				Resources: corev1.ResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{},
					Limits:   map[corev1.ResourceName]resource.Quantity{},
				},
			},
		}
		v = &ClusterCustomValidator{}
	})

	It("returns an error when the CPU request is greater than CPU limit", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("2")
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("1")

		errors := v.validateResources(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("CPU request is greater than the limit"))
	})

	It("returns an error when the Memory request is greater than Memory limit", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("2Gi")
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")

		errors := v.validateResources(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Memory request is greater than the limit"))
	})

	It("returns no error when the ephemeral storage request is correctly set", func() {
		cluster.Spec.Resources.Requests["ephemeral-storage"] = resource.MustParse("1")
		cluster.Spec.Resources.Limits["ephemeral-storage"] = resource.MustParse("1")

		errors := v.validateResources(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("returns an error when the ephemeral storage request is greater than ephemeral storage limit", func() {
		cluster.Spec.Resources.Requests["ephemeral-storage"] = resource.MustParse("2")
		cluster.Spec.Resources.Limits["ephemeral-storage"] = resource.MustParse("1")

		errors := v.validateResources(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Ephemeral storage request is greater than the limit"))
	})

	It("returns three errors when CPU, Memory, and ephemeral storage requests are greater than limits", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("2")
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("1")
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("2Gi")
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")
		cluster.Spec.Resources.Requests["ephemeral-storage"] = resource.MustParse("2")
		cluster.Spec.Resources.Limits["ephemeral-storage"] = resource.MustParse("1")

		errors := v.validateResources(cluster)
		Expect(errors).To(HaveLen(3))
		Expect(errors[0].Detail).To(Equal("CPU request is greater than the limit"))
		Expect(errors[1].Detail).To(Equal("Memory request is greater than the limit"))
		Expect(errors[2].Detail).To(Equal("Ephemeral storage request is greater than the limit"))
	})

	It("returns two errors when both CPU and Memory requests are greater than their limits", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("2")
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("1")
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("2Gi")
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")

		errors := v.validateResources(cluster)
		Expect(errors).To(HaveLen(2))
		Expect(errors[0].Detail).To(Equal("CPU request is greater than the limit"))
		Expect(errors[1].Detail).To(Equal("Memory request is greater than the limit"))
	})

	It("returns no errors when both CPU and Memory requests are less than or equal to their limits", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("1")
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("2")
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1Gi")
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("2Gi")

		errors := v.validateResources(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when CPU request is set but limit is nil", func() {
		cluster.Spec.Resources.Requests["cpu"] = resource.MustParse("1")
		errors := v.validateResources(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when CPU limit is set but request is nil", func() {
		cluster.Spec.Resources.Limits["cpu"] = resource.MustParse("1")
		errors := v.validateResources(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when Memory request is set but limit is nil", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1Gi")
		errors := v.validateResources(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when Memory limit is set but request is nil", func() {
		cluster.Spec.Resources.Limits["memory"] = resource.MustParse("1Gi")
		errors := v.validateResources(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("returns an error when memoryRequest is less than shared_buffers in kB", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1Gi")
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "2000000kB"
		errors := v.validateResources(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Memory request is lower than PostgreSQL `shared_buffers` value"))
	})

	It("returns an error when memoryRequest is less than shared_buffers in MB", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1000Mi")
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "2000MB"
		errors := v.validateResources(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Detail).To(Equal("Memory request is lower than PostgreSQL `shared_buffers` value"))
	})

	It("returns no errors when memoryRequest is greater than or equal to shared_buffers in GB", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("2Gi")
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "1GB"
		errors := v.validateResources(cluster)
		Expect(errors).To(BeEmpty())
	})

	It("returns no errors when shared_buffers is in a format that can't be parsed", func() {
		cluster.Spec.Resources.Requests["memory"] = resource.MustParse("1Gi")
		cluster.Spec.PostgresConfiguration.Parameters["shared_buffers"] = "invalid_value"
		errors := v.validateResources(cluster)
		Expect(errors).To(BeEmpty())
	})
})

var _ = Describe("Tablespaces validation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	createFakeTemporaryTbsConf := func(name string) apiv1.TablespaceConfiguration {
		return apiv1.TablespaceConfiguration{
			Name: name,
			Storage: apiv1.StorageConfiguration{
				Size: "10Gi",
			},
		}
	}

	It("should succeed if there is no tablespaces section", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
			},
		}
		Expect(v.validate(cluster)).To(BeEmpty())
	})

	It("should succeed if the tablespaces are ok", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					createFakeTemporaryTbsConf("my_tablespace"),
				},
			},
		}
		Expect(v.validate(cluster)).To(BeEmpty())
	})

	It("should produce an error if the tablespace name is too long", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					// each repetition is 14 char long, so 5x14 = 70 char > postgres limit
					createFakeTemporaryTbsConf("my_tablespace1my_tablespace2my_tablespace3my_tablespace4my_tablespace5"),
				},
			},
		}
		Expect(v.validate(cluster)).To(HaveLen(1))
	})

	It("should produce an error if the tablespace name is reserved by Postgres", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					createFakeTemporaryTbsConf("pg_foo"),
				},
			},
		}
		Expect(v.validate(cluster)).To(HaveLen(1))
	})

	It("should produce an error if the tablespace name is not valid", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					// each repetition is 14 char long, so 5x14 = 70 char > postgres limit
					createFakeTemporaryTbsConf("my-^&sdf;"),
				},
			},
		}
		Expect(v.validate(cluster)).To(HaveLen(1))
	})

	It("should produce an error if there are duplicate tablespaces", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					createFakeTemporaryTbsConf("my_tablespace"),
					createFakeTemporaryTbsConf("my_TAblespace"),
					createFakeTemporaryTbsConf("another"),
				},
			},
		}
		Expect(v.validate(cluster)).To(HaveLen(1))
	})

	It("should produce an error if the storage configured for the tablespace is invalid", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					// each repetition is 14 char long, so 5x14 = 70 char > postgres limit
					{
						Name: "my_tablespace1",
						Storage: apiv1.StorageConfiguration{
							Size: "10Gibberish",
						},
					},
				},
			},
		}
		Expect(v.validate(cluster)).To(HaveLen(1))
	})

	It("should produce two errors if two tablespaces have errors", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					// each repetition is 14 char long, so 5x14 = 70 char > postgres limit
					{
						Name: "my_tablespace1",
						Storage: apiv1.StorageConfiguration{
							Size: "10Gibberish",
						},
					},
					// each repetition is 14 char long, so 5x14 = 70 char > postgres limit
					createFakeTemporaryTbsConf("my_tablespace1my_tablespace2my_tablespace3my_tablespace4my_tablespace5"),
				},
			},
		}
		Expect(v.validate(cluster)).To(HaveLen(2))
	})

	It("should produce an error if the tablespaces section is deleted", func() {
		oldCluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					createFakeTemporaryTbsConf("my-tablespace1"),
				},
			},
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
			},
		}
		Expect(v.validateClusterChanges(cluster, oldCluster)).To(HaveLen(1))
	})

	It("should produce an error if a tablespace is deleted", func() {
		oldCluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					createFakeTemporaryTbsConf("my-tablespace1"),
					createFakeTemporaryTbsConf("my-tablespace2"),
				},
			},
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					createFakeTemporaryTbsConf("my-tablespace1"),
				},
			},
		}
		Expect(v.validateClusterChanges(cluster, oldCluster)).To(HaveLen(1))
	})

	It("should produce an error if a tablespace is reduced in size", func() {
		oldCluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					createFakeTemporaryTbsConf("my-tablespace1"),
				},
			},
		}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					{
						Name: "my-tablespace1",
						Storage: apiv1.StorageConfiguration{
							Size: "9Gi",
						},
					},
				},
			},
		}
		Expect(v.validateClusterChanges(cluster, oldCluster)).To(HaveLen(1))
	})

	It("should not complain when the backup section refers to a tbs that is defined", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					{
						Name: "my-tablespace1",
						Storage: apiv1.StorageConfiguration{
							Size: "9Gi",
						},
					},
				},
				Backup: &apiv1.BackupConfiguration{
					VolumeSnapshot: &apiv1.VolumeSnapshotConfiguration{
						TablespaceClassName: map[string]string{
							"my-tablespace1": "random",
						},
					},
				},
			},
		}
		Expect(v.validateTablespaceBackupSnapshot(cluster)).To(BeEmpty())
	})

	It("should complain when the backup section refers to a tbs that is not defined", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster1",
			},
			Spec: apiv1.ClusterSpec{
				Instances: 3,
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "10Gi",
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					{
						Name: "my-tablespace1",
						Storage: apiv1.StorageConfiguration{
							Size: "9Gi",
						},
					},
				},
				Backup: &apiv1.BackupConfiguration{
					VolumeSnapshot: &apiv1.VolumeSnapshotConfiguration{
						TablespaceClassName: map[string]string{
							"not-present": "random",
						},
					},
				},
			},
		}
		Expect(v.validateTablespaceBackupSnapshot(cluster)).To(HaveLen(1))
	})
})

var _ = Describe("Validate hibernation", func() {
	var v *ClusterCustomValidator
	BeforeEach(func() {
		v = &ClusterCustomValidator{}
	})

	It("should succeed if hibernation is set to 'on'", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.HibernationAnnotationName: string(utils.HibernationAnnotationValueOn),
				},
			},
		}
		Expect(v.validateHibernationAnnotation(cluster)).To(BeEmpty())
	})

	It("should succeed if hibernation is set to 'off'", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.HibernationAnnotationName: string(utils.HibernationAnnotationValueOff),
				},
			},
		}
		Expect(v.validateHibernationAnnotation(cluster)).To(BeEmpty())
	})

	It("should fail if hibernation is set to an invalid value", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.HibernationAnnotationName: "",
				},
			},
		}
		Expect(v.validateHibernationAnnotation(cluster)).To(HaveLen(1))
	})
})

var _ = Describe("validateManagedServices", func() {
	var cluster *apiv1.Cluster
	var v *ClusterCustomValidator

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Services: &apiv1.ManagedServices{
						Additional: []apiv1.ManagedService{},
					},
				},
			},
		}
		v = &ClusterCustomValidator{}
	})

	Context("when Managed or Services is nil", func() {
		It("should return no errors", func() {
			cluster.Spec.Managed = nil
			Expect(v.validateManagedServices(cluster)).To(BeNil())

			cluster.Spec.Managed = &apiv1.ManagedConfiguration{}
			cluster.Spec.Managed.Services = nil
			Expect(v.validateManagedServices(cluster)).To(BeNil())
		})
	})

	Context("when there are no duplicate names", func() {
		It("should return no errors", func() {
			cluster.Spec.Managed.Services.Additional = []apiv1.ManagedService{
				{
					ServiceTemplate: apiv1.ServiceTemplateSpec{
						ObjectMeta: apiv1.Metadata{Name: "service1"},
					},
				},
				{
					ServiceTemplate: apiv1.ServiceTemplateSpec{
						ObjectMeta: apiv1.Metadata{Name: "service2"},
					},
				},
			}
			Expect(v.validateManagedServices(cluster)).To(BeNil())
		})
	})

	Context("when there are duplicate names", func() {
		It("should return an error", func() {
			cluster.Spec.Managed.Services.Additional = []apiv1.ManagedService{
				{
					ServiceTemplate: apiv1.ServiceTemplateSpec{
						ObjectMeta: apiv1.Metadata{Name: "service1"},
					},
				},
				{
					ServiceTemplate: apiv1.ServiceTemplateSpec{
						ObjectMeta: apiv1.Metadata{Name: "service1"},
					},
				},
			}
			errs := v.validateManagedServices(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Type).To(Equal(field.ErrorTypeInvalid))
			Expect(errs[0].Field).To(Equal("spec.managed.services.additional"))
			Expect(errs[0].Detail).To(ContainSubstring("contains services with the same .metadata.name"))
		})
	})

	Context("when service template validation fails", func() {
		It("should return an error", func() {
			cluster.Spec.Managed.Services.Additional = []apiv1.ManagedService{
				{
					ServiceTemplate: apiv1.ServiceTemplateSpec{
						ObjectMeta: apiv1.Metadata{Name: ""},
					},
				},
			}
			errs := v.validateManagedServices(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Type).To(Equal(field.ErrorTypeInvalid))
			Expect(errs[0].Field).To(Equal("spec.managed.services.additional[0]"))
		})

		It("should not allow reserved service names", func() {
			assertError := func(name string, index int, err *field.Error) {
				expectedDetail := fmt.Sprintf("the service name: '%s' is reserved for operator use", name)
				Expect(err.Type).To(Equal(field.ErrorTypeInvalid))
				Expect(err.Field).To(Equal(fmt.Sprintf("spec.managed.services.additional[%d]", index)))
				Expect(err.Detail).To(Equal(expectedDetail))
			}
			cluster.Spec.Managed.Services.Additional = []apiv1.ManagedService{
				{ServiceTemplate: apiv1.ServiceTemplateSpec{ObjectMeta: apiv1.Metadata{Name: cluster.GetServiceReadWriteName()}}},
				{ServiceTemplate: apiv1.ServiceTemplateSpec{ObjectMeta: apiv1.Metadata{Name: cluster.GetServiceReadName()}}},
				{ServiceTemplate: apiv1.ServiceTemplateSpec{ObjectMeta: apiv1.Metadata{Name: cluster.GetServiceReadOnlyName()}}},
				{ServiceTemplate: apiv1.ServiceTemplateSpec{ObjectMeta: apiv1.Metadata{Name: cluster.GetServiceAnyName()}}},
			}
			errs := v.validateManagedServices(cluster)
			Expect(errs).To(HaveLen(4))
			assertError("test-rw", 0, errs[0])
			assertError("test-r", 1, errs[1])
			assertError("test-ro", 2, errs[2])
			assertError("test-any", 3, errs[3])
		})
	})

	Context("disabledDefault service validation", func() {
		It("should allow the disablement of ro and r service", func() {
			cluster.Spec.Managed.Services.DisabledDefaultServices = []apiv1.ServiceSelectorType{
				apiv1.ServiceSelectorTypeR,
				apiv1.ServiceSelectorTypeRO,
			}
			errs := v.validateManagedServices(cluster)
			Expect(errs).To(BeEmpty())
		})

		It("should not allow the disablement of rw service", func() {
			cluster.Spec.Managed.Services.DisabledDefaultServices = []apiv1.ServiceSelectorType{
				apiv1.ServiceSelectorTypeRW,
			}
			errs := v.validateManagedServices(cluster)
			Expect(errs).To(HaveLen(1))
			Expect(errs[0].Type).To(Equal(field.ErrorTypeInvalid))
			Expect(errs[0].Field).To(Equal("spec.managed.services.disabledDefaultServices"))
		})
	})
})

var _ = Describe("ServiceTemplate Validation", func() {
	var (
		path         *field.Path
		serviceSpecs apiv1.ServiceTemplateSpec
	)

	BeforeEach(func() {
		path = field.NewPath("spec")
	})

	Describe("validateServiceTemplate", func() {
		Context("when name is required", func() {
			It("should return an error if the name is empty", func() {
				serviceSpecs = apiv1.ServiceTemplateSpec{
					ObjectMeta: apiv1.Metadata{Name: ""},
				}

				errs := validateServiceTemplate(path, true, serviceSpecs)
				Expect(errs).To(HaveLen(1))
				Expect(errs[0].Error()).To(ContainSubstring("name is required"))
			})

			It("should not return an error if the name is present", func() {
				serviceSpecs = apiv1.ServiceTemplateSpec{
					ObjectMeta: apiv1.Metadata{Name: "valid-name"},
				}

				errs := validateServiceTemplate(path, true, serviceSpecs)
				Expect(errs).To(BeEmpty())
			})
		})

		Context("when name is not allowed", func() {
			It("should return an error if the name is present", func() {
				serviceSpecs = apiv1.ServiceTemplateSpec{
					ObjectMeta: apiv1.Metadata{Name: "invalid-name"},
				}

				errs := validateServiceTemplate(path, false, serviceSpecs)
				Expect(errs).To(HaveLen(1))
				Expect(errs[0].Error()).To(ContainSubstring("name is not allowed"))
			})

			It("should not return an error if the name is empty", func() {
				serviceSpecs = apiv1.ServiceTemplateSpec{
					ObjectMeta: apiv1.Metadata{Name: ""},
				}

				errs := validateServiceTemplate(path, false, serviceSpecs)
				Expect(errs).To(BeEmpty())
			})
		})

		Context("when selector is present", func() {
			It("should return an error if the selector is present", func() {
				serviceSpecs = apiv1.ServiceTemplateSpec{
					ObjectMeta: apiv1.Metadata{Name: "valid-name"},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{"app": "test"},
					},
				}

				errs := validateServiceTemplate(path, true, serviceSpecs)
				Expect(errs).To(HaveLen(1))
				Expect(errs[0].Error()).To(ContainSubstring("selector field is managed by the operator"))
			})

			It("should not return an error if the selector is absent", func() {
				serviceSpecs = apiv1.ServiceTemplateSpec{
					ObjectMeta: apiv1.Metadata{Name: "valid-name"},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{},
					},
				}

				errs := validateServiceTemplate(path, true, serviceSpecs)
				Expect(errs).To(BeEmpty())
			})
		})
	})
})

var _ = Describe("validatePodPatchAnnotation", func() {
	var v *ClusterCustomValidator

	It("returns nil if the annotation is not present", func() {
		cluster := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
		Expect(v.validatePodPatchAnnotation(cluster)).To(BeNil())
	})

	It("returns an error if decoding the JSON patch fails to decode", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.PodPatchAnnotationName: "invalid-json-patch",
				},
			},
		}

		errors := v.validatePodPatchAnnotation(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Type).To(Equal(field.ErrorTypeInvalid))
		Expect(errors[0].Field).To(Equal("metadata.annotations." + utils.PodPatchAnnotationName))
		Expect(errors[0].Detail).To(ContainSubstring("error decoding JSON patch"))
	})

	It("returns an error if decoding the JSON patch fails to apply", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.PodPatchAnnotationName: `[{"op": "replace", "path": "/spec/podInvalidSection", "value": "test"}]`,
				},
			},
		}

		errors := v.validatePodPatchAnnotation(cluster)
		Expect(errors).To(HaveLen(1))
		Expect(errors[0].Type).To(Equal(field.ErrorTypeInvalid))
		Expect(errors[0].Field).To(Equal("metadata.annotations." + utils.PodPatchAnnotationName))
		Expect(errors[0].Detail).To(ContainSubstring("jsonpatch doesn't apply cleanly to the pod"))
	})

	It("returns nil if the JSON patch is decoded successfully", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					utils.PodPatchAnnotationName: `[{"op": "replace", "path": "/metadata/name", "value": "test"}]`,
				},
			},
		}

		Expect(v.validatePodPatchAnnotation(cluster)).To(BeNil())
	})
})
