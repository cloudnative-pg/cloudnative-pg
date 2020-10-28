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

var _ = Describe("", func() {
	It("defaults to creating an application database", func() {
		cluster := Cluster{}
		cluster.Default()
		Expect(cluster.Spec.Bootstrap.InitDB.Database).To(Equal("app"))
		Expect(cluster.Spec.Bootstrap.InitDB.Owner).To(Equal("app"))
	})

	It("default the owner user with the database name", func() {
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
})

var _ = Describe("Storage validation", func() {
	It("complain if the value isn't correct", func() {
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
		Expect(cluster.Spec.ImageName).To(Equal(versions.DefaultImageName))
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
