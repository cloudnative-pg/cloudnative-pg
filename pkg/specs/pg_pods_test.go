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

package specs

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"

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
	pod, _ := PodWithExistingStorage(cluster, 1)

	It("extract the default image name", func() {
		Expect(GetPostgresImageName(*pod)).To(Equal(configuration.Current.PostgresImageName))
	})

	It("extract the init container image name", func() {
		Expect(GetBootstrapControllerImageName(*pod)).To(Equal(configuration.Current.OperatorImageName))
	})
})
