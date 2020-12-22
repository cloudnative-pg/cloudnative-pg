/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/versions"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extract the used image name", func() {
	cluster := v1alpha1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterName",
			Namespace: "default",
		},
	}
	pod := PodWithExistingStorage(cluster, nil, 1)

	It("extract the default image name", func() {
		Expect(GetPostgreSQLImageName(*pod)).To(Equal(versions.GetDefaultImageName()))
	})
})
