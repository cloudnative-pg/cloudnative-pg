/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package postgres

import (
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PostgreSQL configuration creation", func() {
	settings := map[string]string{
		"shared_buffers": "1024MB",
	}

	It("apply the default settings", func() {
		info := ConfigurationInfo{
			Settings:           CnpConfigurationSettings,
			MajorVersion:       100000,
			UserSettings:       settings,
			IncludingMandatory: true,
			Replicas:           nil,
			SyncReplicas:       0,
		}
		config := CreatePostgresqlConfiguration(info)
		Expect(len(config)).To(BeNumerically(">", 1))
		Expect(config["hot_standby"]).To(Equal("true"))
	})

	It("enforce the mandatory values", func() {
		info := ConfigurationInfo{
			Settings:     CnpConfigurationSettings,
			MajorVersion: 100000,
			UserSettings: map[string]string{
				"hot_standby": "off",
			},
			IncludingMandatory: true,
			Replicas:           nil,
			SyncReplicas:       0,
		}
		config := CreatePostgresqlConfiguration(info)
		Expect(config["hot_standby"]).To(Equal("true"))
	})

	It("generate a config file", func() {
		info := ConfigurationInfo{
			Settings:           CnpConfigurationSettings,
			MajorVersion:       100000,
			UserSettings:       settings,
			IncludingMandatory: true,
			Replicas:           nil,
			SyncReplicas:       0,
		}
		confFile, sha256 := CreatePostgresqlConfFile(CreatePostgresqlConfiguration(info))
		Expect(sha256).NotTo(BeEmpty())
		Expect(confFile).To(Not(BeEmpty()))
		Expect(len(strings.Split(confFile, "\n"))).To(BeNumerically(">", 2))
	})

	It("is sorted by parameter name", func() {
		settings := map[string]string{
			"shared_buffers":  "128KB",
			"log_destination": "stderr",
		}
		confFile, sha256 := CreatePostgresqlConfFile(settings)
		Expect(sha256).NotTo(BeEmpty())
		Expect(confFile).To(ContainSubstring("log_destination = 'stderr'\nshared_buffers = '128KB'\n"))
	})

	When("version is 10", func() {
		It("will use appropriate settings", func() {
			info := ConfigurationInfo{
				Settings:           CnpConfigurationSettings,
				MajorVersion:       100000,
				UserSettings:       settings,
				IncludingMandatory: true,
				Replicas:           nil,
				SyncReplicas:       0,
			}
			config := CreatePostgresqlConfiguration(info)
			Expect(config["wal_keep_segments"]).To(Equal("32"))
		})
	})

	When("version is 13", func() {
		It("will use appropriate settings", func() {
			info := ConfigurationInfo{
				Settings:           CnpConfigurationSettings,
				MajorVersion:       130000,
				UserSettings:       settings,
				IncludingMandatory: true,
				Replicas:           nil,
				SyncReplicas:       0,
			}
			config := CreatePostgresqlConfiguration(info)
			Expect(config["wal_keep_size"]).To(Equal("512MB"))
		})
	})

	When("we are using synchronous replication", func() {
		It("generate the correct value for the synchronous_standby_names parameter", func() {
			info := ConfigurationInfo{
				Settings:           CnpConfigurationSettings,
				MajorVersion:       130000,
				UserSettings:       settings,
				IncludingMandatory: true,
				Replicas: []string{
					"one",
					"two",
					"three",
				},
				SyncReplicas: 2,
			}
			config := CreatePostgresqlConfiguration(info)
			Expect(config["synchronous_standby_names"]).To(Equal("ANY 2 (\"one\",\"two\",\"three\")"))
		})
	})
})

var _ = Describe("pg_hba.conf generation", func() {
	specRules := []string{
		"one",
		"two",
		"three",
	}
	It("insert the spec configuration between an header and a footer", func() {
		hbaContent := CreateHBARules(specRules)
		Expect(hbaContent).To(ContainSubstring("two\n"))
	})
})
