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

package postgres

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL configuration creation", func() {
	settings := map[string]string{
		"shared_buffers": "1024MB",
	}

	It("apply the default settings", func() {
		info := ConfigurationInfo{
			Settings:              CnpgConfigurationSettings,
			MajorVersion:          100000,
			UserSettings:          settings,
			IncludingMandatory:    true,
			SyncReplicasElectable: nil,
			SyncReplicas:          0,
		}
		config := CreatePostgresqlConfiguration(info)
		Expect(len(config.configs)).To(BeNumerically(">", 1))
		Expect(config.GetConfig("hot_standby")).To(Equal("true"))
	})

	It("enforce the mandatory values", func() {
		info := ConfigurationInfo{
			Settings:     CnpgConfigurationSettings,
			MajorVersion: 100000,
			UserSettings: map[string]string{
				"hot_standby": "off",
			},
			IncludingMandatory:    true,
			SyncReplicasElectable: nil,
			SyncReplicas:          0,
		}
		config := CreatePostgresqlConfiguration(info)
		Expect(config.GetConfig("hot_standby")).To(Equal("true"))
	})

	It("generate a config file", func() {
		info := ConfigurationInfo{
			Settings:              CnpgConfigurationSettings,
			MajorVersion:          100000,
			UserSettings:          settings,
			IncludingMandatory:    true,
			SyncReplicasElectable: nil,
			SyncReplicas:          0,
		}
		conf := CreatePostgresqlConfiguration(info)
		confFile, sha256 := CreatePostgresqlConfFile(conf)
		Expect(sha256).NotTo(BeEmpty())
		Expect(confFile).To(Not(BeEmpty()))
		Expect(len(strings.Split(confFile, "\n"))).To(BeNumerically(">", 2))
	})

	It("is sorted by parameter name", func() {
		settings := map[string]string{
			"shared_buffers":  "128KB",
			"log_destination": "stderr",
		}
		confFile, sha256 := CreatePostgresqlConfFile(&PgConfiguration{settings})
		Expect(sha256).NotTo(BeEmpty())
		Expect(confFile).To(ContainSubstring("log_destination = 'stderr'\nshared_buffers = '128KB'\n"))
	})

	When("version is 10", func() {
		It("will use appropriate settings", func() {
			info := ConfigurationInfo{
				Settings:              CnpgConfigurationSettings,
				MajorVersion:          100000,
				UserSettings:          settings,
				IncludingMandatory:    true,
				SyncReplicasElectable: nil,
				SyncReplicas:          0,
			}
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("wal_keep_segments")).To(Equal("32"))
		})
	})

	When("version is 13", func() {
		It("will use appropriate settings", func() {
			info := ConfigurationInfo{
				Settings:              CnpgConfigurationSettings,
				MajorVersion:          130000,
				UserSettings:          settings,
				IncludingMandatory:    true,
				SyncReplicasElectable: nil,
				SyncReplicas:          0,
			}
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("wal_keep_size")).To(Equal("512MB"))
			Expect(config.GetConfig("wal_level")).To(Equal("logical"))
			Expect(config.GetConfig("shared_memory_type")).To(Equal("mmap"))
		})
	})

	When("replica cluster is being configured", func() {
		It("will set archive_mode to always", func() {
			info := ConfigurationInfo{
				Settings:              CnpgConfigurationSettings,
				MajorVersion:          130000,
				UserSettings:          settings,
				IncludingMandatory:    true,
				SyncReplicasElectable: nil,
				SyncReplicas:          0,
				IsReplicaCluster:      true,
			}
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("archive_mode")).To(Equal("always"))
		})
	})

	When("a primary cluster is configured", func() {
		It("will set archive_mode to on", func() {
			info := ConfigurationInfo{
				Settings:              CnpgConfigurationSettings,
				MajorVersion:          130000,
				UserSettings:          settings,
				IncludingMandatory:    true,
				SyncReplicasElectable: nil,
				SyncReplicas:          0,
				IsReplicaCluster:      false,
			}
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("archive_mode")).To(Equal("on"))
		})
	})

	It("adds shared_preload_library correctly", func() {
		info := ConfigurationInfo{
			Settings:                         CnpgConfigurationSettings,
			MajorVersion:                     130000,
			IncludingMandatory:               true,
			SyncReplicas:                     0,
			IncludingSharedPreloadLibraries:  true,
			AdditionalSharedPreloadLibraries: []string{"some_library", "another_library", ""},
		}
		config := CreatePostgresqlConfiguration(info)
		libraries := strings.Split(config.GetConfig(SharedPreloadLibraries), ",")
		Expect(len(info.AdditionalSharedPreloadLibraries)).To(BeNumerically(">", len(libraries)))
		Expect(libraries).Should(SatisfyAll(
			ContainElements("some_library", "another_library"), Not(ContainElement(""))))
	})

	When("we are using synchronous replication", func() {
		It("generate the correct value for the synchronous_standby_names parameter", func() {
			info := ConfigurationInfo{
				Settings:           CnpgConfigurationSettings,
				MajorVersion:       130000,
				UserSettings:       settings,
				IncludingMandatory: true,
				SyncReplicasElectable: []string{
					"one",
					"two",
					"three",
				},
				SyncReplicas: 2,
			}
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("synchronous_standby_names")).
				To(Equal("ANY 2 (\"one\",\"two\",\"three\")"))
		})
	})

	It("checks if PreserveFixedSettingsFromUser works properly", func() {
		info := ConfigurationInfo{
			Settings:     CnpgConfigurationSettings,
			MajorVersion: 100000,
			UserSettings: map[string]string{
				"ssl":                  "off",
				"recovery_target_name": "test",
			},
			SyncReplicasElectable: nil,
			SyncReplicas:          0,
		}
		By("making sure it enforces fixed parameters if false", func() {
			info.PreserveFixedSettingsFromUser = false
			info.IncludingMandatory = false
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("ssl")).To(Equal(""))
			Expect(config.GetConfig("recovery_target_name")).To(Equal(""))
		})

		By("making sure it doesn't enforce fixed parameters if true", func() {
			info.PreserveFixedSettingsFromUser = true
			info.IncludingMandatory = false
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("ssl")).To(Equal("off"))
			Expect(config.GetConfig("recovery_target_name")).To(Equal("test"))
		})
		By("making sure it enforces fixed parameters if IncludingMandatory is true too", func() {
			info.PreserveFixedSettingsFromUser = true
			info.IncludingMandatory = true
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("ssl")).To(Equal("on"))
			Expect(config.GetConfig("recovery_target_name")).To(Equal(""))
		})
		By("making sure it enforces fixed parameters if IncludingMandatory is true, "+
			"but PreserveFixedSettingsFromUser is false ", func() {
			info.PreserveFixedSettingsFromUser = false
			info.IncludingMandatory = true
			config := CreatePostgresqlConfiguration(info)
			Expect(config.GetConfig("ssl")).To(Equal("on"))
			Expect(config.GetConfig("recovery_target_name")).To(Equal(""))
		})
	})

	Context("allow_alter_system", func() {
		When("PostgreSQL >= 17", func() {
			It("can properly set allow_alter_system to on", func() {
				info := ConfigurationInfo{
					IsAlterSystemEnabled: true,
					MajorVersion:         170000,
					IncludingMandatory:   true,
				}
				config := CreatePostgresqlConfiguration(info)
				Expect(config.GetConfig("allow_alter_system")).To(Equal("on"))
			})

			It("can properly set allow_alter_system to off", func() {
				info := ConfigurationInfo{
					IsAlterSystemEnabled: false,
					MajorVersion:         180000,
					IncludingMandatory:   true,
				}
				config := CreatePostgresqlConfiguration(info)
				Expect(config.GetConfig("allow_alter_system")).To(Equal("off"))
			})
		})

		When("PostgreSQL <17", func() {
			It("should not set allow_alter_system", func() {
				info := ConfigurationInfo{
					IsAlterSystemEnabled: false,
					MajorVersion:         140000,
					IncludingMandatory:   true,
				}
				config := CreatePostgresqlConfiguration(info)
				value, ok := config.configs["allow_alter_system"]
				Expect(ok).To(BeFalse())
				Expect(value).To(BeEmpty())
			})
			It("should not set allow_alter_system", func() {
				info := ConfigurationInfo{
					IsAlterSystemEnabled: true,
					MajorVersion:         140000,
					IncludingMandatory:   true,
				}
				config := CreatePostgresqlConfiguration(info)
				value, ok := config.configs["allow_alter_system"]
				Expect(ok).To(BeFalse())
				Expect(value).To(BeEmpty())
			})
		})
	})
})

var _ = Describe("pg_hba.conf generation", func() {
	specRules := []string{
		"one",
		"two",
		"three",
	}

	It("insert the spec configuration between an header and a footer when the version can not be parsed", func() {
		Expect(CreateHBARules(specRules, "md5", "")).To(
			ContainSubstring("\ntwo\n"))
	})

	It("really use the passed default authentication method", func() {
		Expect(CreateHBARules(specRules, "this-one", "")).To(
			ContainSubstring("\nhost all all all this-one\n"))
	})

	It("really uses the ldapConfigString", func() {
		Expect(CreateHBARules(specRules, "defaultAuthenticationMethod", "ldapConfigString")).To(
			ContainSubstring("\nldapConfigString\n"))
	})
})

var _ = Describe("pg_ident.conf generation", func() {
	specRules := []string{
		"test someone else",
	}

	It("contains the default map when no mappings are added", func() {
		Expect(CreateIdentRules(make([]string, 0), "someone")).To(
			ContainSubstring("\nlocal someone postgres\n"))
	})

	It("contains the default map and additional mappings when added", func() {
		rules, _ := CreateIdentRules(specRules, "someone")
		Expect(rules).To(ContainSubstring("\nlocal someone postgres\n"))
		Expect(rules).To(ContainSubstring("\ntest someone else\n"))
	})
})

var _ = Describe("pgaudit", func() {
	var pgaudit *ManagedExtension
	BeforeEach(func() {
		pgaudit = nil
		for i, ext := range ManagedExtensions {
			if ext.Name == "pgaudit" {
				pgaudit = &ManagedExtensions[i]
				break
			}
		}
		Expect(pgaudit).ToNot(BeNil())
	})

	It("is enabled", func() {
		userConfigsWithPgAudit := make(map[string]string, 1)
		userConfigsWithPgAudit["pgaudit.xxx"] = "test"
		Expect(pgaudit.IsUsed(userConfigsWithPgAudit)).To(BeTrue())
	})

	It("is not enabled", func() {
		userConfigsWithNoPgAudit := make(map[string]string, 1)
		Expect(pgaudit.IsUsed(userConfigsWithNoPgAudit)).To(BeFalse())
	})

	It("adds pgaudit to shared_preload_library", func() {
		info := ConfigurationInfo{
			Settings:                        CnpgConfigurationSettings,
			MajorVersion:                    130000,
			UserSettings:                    map[string]string{"pgaudit.something": "something"},
			IncludingSharedPreloadLibraries: true,
			IncludingMandatory:              true,
			SyncReplicas:                    0,
		}
		config := CreatePostgresqlConfiguration(info)
		Expect(config.GetConfig(SharedPreloadLibraries)).To(Equal("pgaudit"))
		info.AdditionalSharedPreloadLibraries = []string{"other_library"}
		config2 := CreatePostgresqlConfiguration(info)
		libraries := strings.Split(config2.GetConfig(SharedPreloadLibraries), ",")
		Expect(libraries).ToNot(ContainElement(""))
		Expect(libraries).To(ContainElements("pgaudit", "other_library"))
	})

	It("adds pg_stat_statements to shared_preload_library", func() {
		info := ConfigurationInfo{
			Settings:                        CnpgConfigurationSettings,
			MajorVersion:                    130000,
			UserSettings:                    map[string]string{"pg_stat_statements.something": "something"},
			IncludingMandatory:              true,
			IncludingSharedPreloadLibraries: true,
			SyncReplicas:                    0,
		}
		config := CreatePostgresqlConfiguration(info)
		Expect(config.GetConfig(SharedPreloadLibraries)).To(Equal("pg_stat_statements"))
		info.AdditionalSharedPreloadLibraries = []string{"other_library"}
		config2 := CreatePostgresqlConfiguration(info)
		libraries := strings.Split(config2.GetConfig(SharedPreloadLibraries), ",")
		Expect(libraries).ToNot(ContainElement(""))
		Expect(libraries).To(ContainElements("pg_stat_statements", "other_library"))
	})

	It("adds pg_stat_statements and pgaudit to shared_preload_library", func() {
		info := ConfigurationInfo{
			Settings:     CnpgConfigurationSettings,
			MajorVersion: 130000,
			UserSettings: map[string]string{
				"pg_stat_statements.something": "something",
				"pgaudit.somethingelse":        "somethingelse",
			},
			IncludingMandatory:              true,
			IncludingSharedPreloadLibraries: true,
			SyncReplicas:                    0,
		}
		config := CreatePostgresqlConfiguration(info)
		libraries := strings.Split(config.GetConfig(SharedPreloadLibraries), ",")
		Expect(libraries).To(HaveLen(2))
		Expect(libraries).ToNot(ContainElement(""))
		Expect(libraries).To(ContainElements("pg_stat_statements", "pgaudit"))
	})
})

var _ = Describe("pg_failover_slots", func() {
	It("adds pg_failover_slots to shared_preload_library", func() {
		info := ConfigurationInfo{
			Settings:                        CnpgConfigurationSettings,
			MajorVersion:                    130000,
			UserSettings:                    map[string]string{"pg_failover_slots.something": "something"},
			IncludingMandatory:              true,
			IncludingSharedPreloadLibraries: true,
			SyncReplicas:                    0,
		}
		config := CreatePostgresqlConfiguration(info)
		libraries := strings.Split(config.GetConfig(SharedPreloadLibraries), ",")
		Expect(libraries).To(HaveLen(1))
		Expect(libraries).ToNot(ContainElement(""))
		Expect(libraries).To(ContainElements("pg_failover_slots"))
	})
})
