/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"strings"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configmap creation", func() {
	cluster := v1alpha1.Cluster{
		ObjectMeta: v1.ObjectMeta{
			Namespace: "thisnamespace",
			Name:      "thisname",
		},
	}
	It("create a configmap with the same name and namespace of the cluster", func() {
		configMap, err := CreatePostgresConfigMap(&cluster)
		Expect(err).To(BeNil())
		Expect(configMap.Name).To(Equal(cluster.Name))
		Expect(configMap.Namespace).To(Equal(cluster.Namespace))
	})
})

var _ = Describe("PostgreSQL configuration creation", func() {
	settings := map[string]string{
		"shared_buffers": "1024MB",
	}

	It("apply the default settings", func() {
		config := CreatePostgresqlConfiguration(100000, settings)
		Expect(len(config)).To(BeNumerically(">", 1))
		Expect(config["hot_standby"]).To(Equal("true"))
	})

	It("enforce the mandatory values", func() {
		testing := map[string]string{
			"hot_standby": "off",
		}
		config := CreatePostgresqlConfiguration(100000, testing)
		Expect(config["hot_standby"]).To(Equal("true"))
	})

	It("generate a config file", func() {
		confFile := CreatePostgresqlConfFile(CreatePostgresqlConfiguration(100000, settings))
		Expect(confFile).To(Not(BeEmpty()))
		Expect(len(strings.Split(confFile, "\n"))).To(BeNumerically(">", 2))
	})

	When("version is 10", func() {
		It("will use appropriate settings", func() {
			config := CreatePostgresqlConfiguration(100000, settings)
			Expect(config["wal_keep_segments"]).To(Equal("32"))
		})
	})

	When("version is 13", func() {
		It("will use appropriate settings", func() {
			config := CreatePostgresqlConfiguration(130000, settings)
			Expect(config["wal_keep_size"]).To(Equal("512MB"))
		})
	})
})
