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
	"path"

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

var _ = Describe("Get the logs", Ordered, func() {
	namespace := "default"
	clusterName := "test-cluster"
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterName + "-1",
		},
	}
	client := fakeClient.NewClientset(pod)
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      clusterName,
			Labels: map[string]string{
				utils.ClusterLabelName: clusterName,
			},
		},
		Spec: apiv1.ClusterSpec{},
	}
	var cl clusterLogs
	plugin.Client = fake.NewClientBuilder().
		WithScheme(scheme.BuildWithAllKnownScheme()).
		WithObjects(cluster).
		Build()

	BeforeEach(func(ctx SpecContext) {
		cl = clusterLogs{
			ctx:         ctx,
			clusterName: clusterName,
			namespace:   namespace,
			follow:      true,
			timestamp:   true,
			tailLines:   -1,
			client:      client,
		}
	})

	It("should get a proper cluster", func() {
		cluster, err := getCluster(cl)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster).ToNot(BeNil())
	})

	It("should get the proper stream cluster log", func() {
		logsStream := getStreamClusterLogs(cluster, cl)
		Expect(logsStream).ToNot(BeNil())
		Expect(logsStream.Options.Follow).To(BeTrue())
		Expect(logsStream.Options.Timestamps).To(BeTrue())
		Expect(logsStream.Options.SinceTime).ToNot(BeNil())
		Expect(logsStream.Options.TailLines).To(BeNil())
	})

	It("should get the proper tail lines", func() {
		cl.tailLines = 5
		logsStream := getStreamClusterLogs(cluster, cl)
		Expect(logsStream).ToNot(BeNil())
		Expect(logsStream.Options.Follow).To(BeTrue())
		Expect(logsStream.Options.Timestamps).To(BeTrue())
		Expect(logsStream.Options.SinceTime).ToNot(BeNil())
		Expect(logsStream.Options.TailLines).ToNot(BeNil())
		Expect(*logsStream.Options.TailLines).To(BeEquivalentTo(5))
	})

	It("should get the proper stream for logs", func() {
		PauseOutputInterception()
		err := followCluster(cl)
		ResumeOutputInterception()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should save the logs to file", func() {
		tempDir := GinkgoT().TempDir()
		cl.outputFile = path.Join(tempDir, "test-file.logs")
		PauseOutputInterception()
		err := saveClusterLogs(cl)
		ResumeOutputInterception()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail if can't write a file", func() {
		tempDir := GinkgoT().TempDir()
		cl.outputFile = path.Join(tempDir, "this-does-not-exist/test-file.log")
		err := saveClusterLogs(cl)
		Expect(err).To(HaveOccurred())
	})

	It("should fail when cluster doesn't exists", func() {
		cl.clusterName += "-fail"
		err := followCluster(cl)
		Expect(err).To(HaveOccurred())

		err = saveClusterLogs(cl)
		Expect(err).To(HaveOccurred())
	})
})
