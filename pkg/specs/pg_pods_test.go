/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Extract the used image name", func() {
	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clusterName",
			Namespace: "default",
		},
	}
	pod := PodWithExistingStorage(cluster, 1)

	It("extract the default image name", func() {
		Expect(GetPostgresImageName(*pod)).To(Equal(configuration.Current.PostgresImageName))
	})

	It("extract the init container image name", func() {
		Expect(GetBootstrapControllerImageName(*pod)).To(Equal(configuration.Current.OperatorImageName))
	})
})
