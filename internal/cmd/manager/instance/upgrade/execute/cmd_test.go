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

package execute

import (
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("setupExtensionEnvironment", func() {
	It("errors when PGDataImageInfo is missing", func() {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				TargetPGDataImageInfo: &apiv1.ImageInfo{},
			},
		}
		err := setupExtensionEnvironment(cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("PGDataImageInfo"))
	})

	It("errors when TargetPGDataImageInfo is missing", func() {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{},
			},
		}
		err := setupExtensionEnvironment(cluster)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("TargetPGDataImageInfo"))
	})

	It("succeeds with both image-info statuses present and no extensions", func() {
		cluster := &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				PGDataImageInfo:       &apiv1.ImageInfo{},
				TargetPGDataImageInfo: &apiv1.ImageInfo{},
			},
		}
		Expect(setupExtensionEnvironment(cluster)).To(Succeed())
	})
})

var _ = Describe("recordPendingExtensionUpdates", func() {
	const (
		clusterName = "cluster-example"
		namespace   = "default"
		podName     = "cluster-example-1"
	)

	var (
		cluster   *apiv1.Cluster
		fakeC     client.Client
		pgDataDir string
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
		}
		fakeC = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			Build()
		pgDataDir = GinkgoT().TempDir()
	})

	getCondition := func(ctx SpecContext) *metav1.Condition {
		var updated apiv1.Cluster
		Expect(fakeC.Get(ctx, client.ObjectKeyFromObject(cluster), &updated)).To(Succeed())
		return meta.FindStatusCondition(
			updated.Status.Conditions,
			string(apiv1.ConditionMajorUpgradeExtensionUpdatesPending),
		)
	}

	It("is a no-op when update_extensions.sql is absent", func(ctx SpecContext) {
		Expect(recordPendingExtensionUpdates(ctx, fakeC, cluster, podName, pgDataDir)).To(Succeed())
		Expect(getCondition(ctx)).To(BeNil())
	})

	It("sets the pending-updates condition when the script is present", func(ctx SpecContext) {
		scriptPath := filepath.Join(pgDataDir, "update_extensions.sql")
		Expect(os.WriteFile(scriptPath, []byte("ALTER EXTENSION pg_stat_statements UPDATE;"), 0o600)).
			To(Succeed())

		Expect(recordPendingExtensionUpdates(ctx, fakeC, cluster, podName, pgDataDir)).To(Succeed())

		c := getCondition(ctx)
		Expect(c).ToNot(BeNil())
		Expect(c.Status).To(Equal(metav1.ConditionTrue))
		Expect(c.Reason).To(Equal(string(apiv1.ConditionReasonExtensionUpdatesPending)))
		Expect(c.Message).To(ContainSubstring(scriptPath))
		Expect(c.Message).To(ContainSubstring(podName))
		Expect(c.Message).To(ContainSubstring("kubectl exec"))
		Expect(c.Message).To(ContainSubstring("every database"))
	})

	It("returns an error when the script path cannot be checked", func(ctx SpecContext) {
		// A non-existent parent directory makes os.Stat return an error
		// distinct from ErrNotExist (the file system propagates a parent
		// permission/lookup failure path), but since fileutils.FileExists
		// abstracts that, we instead simulate it by passing a path that
		// contains a NUL byte: Go's syscall layer rejects it before the
		// file is even consulted, surfacing a real error.
		err := recordPendingExtensionUpdates(ctx, fakeC, cluster, podName, "/dev/null/\x00invalid")
		Expect(err).To(HaveOccurred())
		Expect(getCondition(ctx)).To(BeNil())
	})
})
