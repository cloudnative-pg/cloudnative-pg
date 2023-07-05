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

package logs

import (
	v1 "k8s.io/api/core/v1"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakeClient "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test the command", func() {
	clusterName := "test-cluster"
	namespace := "default"
	var cluster *apiv1.Cluster
	pod := &v1.Pod{
		ObjectMeta: v12.ObjectMeta{
			Namespace: namespace,
			Name:      clusterName + "-1",
		},
	}
	cluster = &apiv1.Cluster{
		ObjectMeta: v12.ObjectMeta{
			Namespace: namespace,
			Name:      clusterName,
			Labels: map[string]string{
				utils.ClusterLabelName: clusterName,
			},
		},
		Spec: apiv1.ClusterSpec{},
	}

	plugin.Namespace = namespace
	plugin.ClientInterface = fakeClient.NewSimpleClientset(pod)
	plugin.Client = fake.NewClientBuilder().
		WithScheme(scheme.BuildWithAllKnownScheme()).
		WithObjects(cluster).
		Build()
	It("should get the command help", func() {
		cmd := clusterCmd()
		err := cmd.Execute()
		Expect(err).To(HaveOccurred())
	})

	It("should not fail, with cluster name as argument", func() {
		cmd := clusterCmd()
		cmd.SetArgs([]string{clusterName})
		err := cmd.Execute()
		Expect(err).ToNot(HaveOccurred())
	})

	It("could follow the logs", func() {
		cmd := clusterCmd()
		cmd.SetArgs([]string{clusterName, "-f"})
		err := cmd.Execute()
		Expect(err).ToNot(HaveOccurred())
	})
})
