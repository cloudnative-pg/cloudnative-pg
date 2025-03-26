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

package logs

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterName + "-1",
		},
	}
	cluster = &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterName,
			Labels: map[string]string{
				utils.ClusterLabelName: clusterName,
			},
		},
		Spec: apiv1.ClusterSpec{},
	}

	plugin.Namespace = namespace
	plugin.ClientInterface = fakeClient.NewClientset(pod)
	plugin.Client = fake.NewClientBuilder().
		WithScheme(scheme.BuildWithAllKnownScheme()).
		WithObjects(cluster).
		Build()

	It("should not fail, with cluster name as argument", func() {
		cmd := clusterCmd()
		cmd.SetArgs([]string{clusterName})
		PauseOutputInterception()
		err := cmd.Execute()
		ResumeOutputInterception()
		Expect(err).ToNot(HaveOccurred())
	})

	It("could follow the logs", func() {
		cmd := clusterCmd()
		cmd.SetArgs([]string{clusterName, "-f"})
		PauseOutputInterception()
		err := cmd.Execute()
		ResumeOutputInterception()
		Expect(err).ToNot(HaveOccurred())
	})
})
