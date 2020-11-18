package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/versions"

	. "github.com/onsi/ginkgo"
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

	It("doesn't complain if we are using fullRecovery", func() {
		fullRecoveryCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					FullRecovery: &BootstrapFullRecovery{},
				},
			},
		}
		result := fullRecoveryCluster.validateBootstrapMethod()
		Expect(result).To(BeEmpty())
	})

	It("complains where there are two active bootstrap methods", func() {
		invalidCluster := &Cluster{
			Spec: ClusterSpec{
				Bootstrap: &BootstrapConfiguration{
					FullRecovery: &BootstrapFullRecovery{},
					InitDB:       &BootstrapInitDB{},
				},
			},
		}
		result := invalidCluster.validateBootstrapMethod()
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
				SuperuserSecret: &corev1.LocalObjectReference{
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
		Expect(cluster.Spec.ImageName).To(Equal(versions.GetDefaultImageName()))
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

	It("complain when the 'latest' tag is detected", func() {
		cluster := Cluster{
			Spec: ClusterSpec{
				ImageName: "postgres:latest",
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
