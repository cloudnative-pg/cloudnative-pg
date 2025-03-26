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

package v1

import (
	"k8s.io/utils/ptr"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cluster default configuration", func() {
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

var _ = Describe("setDefaultPlugins", func() {
	It("adds pre-defined plugins if not already present", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Plugins: []PluginConfiguration{
					{Name: "existing-plugin", Enabled: ptr.To(true)},
				},
			},
		}
		config := &configuration.Data{
			IncludePlugins: "predefined-plugin1,predefined-plugin2",
		}

		cluster.setDefaultPlugins(config)

		Expect(cluster.Spec.Plugins).To(
			ContainElement(PluginConfiguration{Name: "existing-plugin", Enabled: ptr.To(true)}))
		Expect(cluster.Spec.Plugins).To(
			ContainElement(PluginConfiguration{Name: "predefined-plugin1", Enabled: ptr.To(true)}))
		Expect(cluster.Spec.Plugins).To(
			ContainElement(PluginConfiguration{Name: "predefined-plugin2", Enabled: ptr.To(true)}))
	})

	It("does not add pre-defined plugins if already present", func() {
		cluster := &Cluster{
			Spec: ClusterSpec{
				Plugins: []PluginConfiguration{
					{Name: "predefined-plugin1", Enabled: ptr.To(false)},
				},
			},
		}
		config := &configuration.Data{
			IncludePlugins: "predefined-plugin1,predefined-plugin2",
		}

		cluster.setDefaultPlugins(config)

		Expect(cluster.Spec.Plugins).To(HaveLen(2))
		Expect(cluster.Spec.Plugins).To(
			ContainElement(PluginConfiguration{Name: "predefined-plugin1", Enabled: ptr.To(false)}))
		Expect(cluster.Spec.Plugins).To(
			ContainElement(PluginConfiguration{Name: "predefined-plugin2", Enabled: ptr.To(true)}))
	})

	It("handles empty plugin list gracefully", func() {
		cluster := &Cluster{}
		config := &configuration.Data{
			IncludePlugins: "predefined-plugin1",
		}

		cluster.setDefaultPlugins(config)

		Expect(cluster.Spec.Plugins).To(HaveLen(1))
		Expect(cluster.Spec.Plugins).To(
			ContainElement(PluginConfiguration{Name: "predefined-plugin1", Enabled: ptr.To(true)}))
	})
})
