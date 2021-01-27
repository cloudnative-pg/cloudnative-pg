/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Configmap creation", func() {
	cluster := apiv1.Cluster{
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
