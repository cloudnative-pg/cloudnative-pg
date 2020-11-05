/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package postgres

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"strings"
)

var _ = Describe("PostgreSQL configuration creation", func() {
	settings := map[string]string{
		"shared_buffers": "1024MB",
	}

	It("apply the default settings", func() {
		config := CreatePostgresqlConfiguration(CnpConfigurationSettings, 100000, settings, true)
		Expect(len(config)).To(BeNumerically(">", 1))
		Expect(config["hot_standby"]).To(Equal("true"))
	})

	It("enforce the mandatory values", func() {
		testing := map[string]string{
			"hot_standby": "off",
		}
		config := CreatePostgresqlConfiguration(CnpConfigurationSettings, 100000, testing, true)
		Expect(config["hot_standby"]).To(Equal("true"))
	})

	It("generate a config file", func() {
		confFile := CreatePostgresqlConfFile(CreatePostgresqlConfiguration(CnpConfigurationSettings, 100000, settings, true))
		Expect(confFile).To(Not(BeEmpty()))
		Expect(len(strings.Split(confFile, "\n"))).To(BeNumerically(">", 2))
	})

	It("is sorted by parameter name", func() {
		settings := map[string]string{
			"shared_buffers":  "128KB",
			"log_destination": "stderr",
		}
		confFile := CreatePostgresqlConfFile(settings)
		Expect(confFile).To(Equal("log_destination = 'stderr'\nshared_buffers = '128KB'\n"))
	})

	When("version is 10", func() {
		It("will use appropriate settings", func() {
			config := CreatePostgresqlConfiguration(CnpConfigurationSettings, 100000, settings, true)
			Expect(config["wal_keep_segments"]).To(Equal("32"))
		})
	})

	When("version is 13", func() {
		It("will use appropriate settings", func() {
			config := CreatePostgresqlConfiguration(CnpConfigurationSettings, 130000, settings, true)
			Expect(config["wal_keep_size"]).To(Equal("512MB"))
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
