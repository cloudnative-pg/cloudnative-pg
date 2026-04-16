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

package fence

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("fencingOn", func() {
	const (
		clusterName = "cluster-example"
		namespace   = "test-ns"
	)

	BeforeEach(func() {
		plugin.Namespace = namespace
	})

	It("should fence a known instance", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-example-1", "cluster-example-2"},
			},
		}
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		Expect(fencingOn(ctx, clusterName, "cluster-example-1")).To(Succeed())

		var updated apiv1.Cluster
		Expect(plugin.Client.Get(ctx,
			types.NamespacedName{Name: clusterName, Namespace: namespace},
			&updated,
		)).To(Succeed())
		Expect(updated.IsInstanceFenced("cluster-example-1")).To(BeTrue())
	})

	It("should reject an unknown instance", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-example-1"},
			},
		}
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		err := fencingOn(ctx, clusterName, "cluster-example-99")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not a known instance"))
	})

	It("should allow fencing all instances", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-example-1"},
			},
		}
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		Expect(fencingOn(ctx, clusterName, utils.FenceAllInstances)).To(Succeed())

		var updated apiv1.Cluster
		Expect(plugin.Client.Get(ctx,
			types.NamespacedName{Name: clusterName, Namespace: namespace},
			&updated,
		)).To(Succeed())
		Expect(updated.IsInstanceFenced("cluster-example-1")).To(BeTrue())
	})

	It("should return an error when the cluster does not exist", func(ctx SpecContext) {
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			Build()

		err := fencingOn(ctx, "nonexistent", "nonexistent-1")
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("fencingOff", func() {
	const (
		clusterName = "cluster-example"
		namespace   = "test-ns"
	)

	jsonMarshal := func(l ...string) string {
		s, err := json.Marshal(l)
		Expect(err).NotTo(HaveOccurred())
		return string(s)
	}

	BeforeEach(func() {
		plugin.Namespace = namespace
	})

	It("should unfence a known fenced instance", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				Annotations: map[string]string{
					utils.FencedInstanceAnnotation: jsonMarshal("cluster-example-1"),
				},
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-example-1", "cluster-example-2"},
			},
		}
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		Expect(fencingOff(ctx, clusterName, "cluster-example-1")).To(Succeed())

		var updated apiv1.Cluster
		Expect(plugin.Client.Get(ctx,
			types.NamespacedName{Name: clusterName, Namespace: namespace},
			&updated,
		)).To(Succeed())
		Expect(updated.IsInstanceFenced("cluster-example-1")).To(BeFalse())
	})

	It("should reject an unknown instance that is not fenced", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-example-1"},
			},
		}
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		err := fencingOff(ctx, clusterName, "cluster-example-99")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not a known instance"))
	})

	It("should allow unfencing an instance not in InstanceNames but currently fenced", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				Annotations: map[string]string{
					utils.FencedInstanceAnnotation: jsonMarshal("cluster-example-3"),
				},
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-example-1", "cluster-example-2"},
			},
		}
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		Expect(fencingOff(ctx, clusterName, "cluster-example-3")).To(Succeed())

		var updated apiv1.Cluster
		Expect(plugin.Client.Get(ctx,
			types.NamespacedName{Name: clusterName, Namespace: namespace},
			&updated,
		)).To(Succeed())
		Expect(updated.IsInstanceFenced("cluster-example-3")).To(BeFalse())
	})

	It("should allow unfencing all instances", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
				Annotations: map[string]string{
					utils.FencedInstanceAnnotation: jsonMarshal("*"),
				},
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: []string{"cluster-example-1"},
			},
		}
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()

		Expect(fencingOff(ctx, clusterName, utils.FenceAllInstances)).To(Succeed())

		var updated apiv1.Cluster
		Expect(plugin.Client.Get(ctx,
			types.NamespacedName{Name: clusterName, Namespace: namespace},
			&updated,
		)).To(Succeed())
		Expect(updated.IsInstanceFenced("cluster-example-1")).To(BeFalse())
	})
})
