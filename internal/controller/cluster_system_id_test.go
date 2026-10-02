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

package controller

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("shouldRefreshSystemID", func() {
	instances := postgres.PostgresqlStatusList{
		Items: []postgres.PostgresqlStatus{{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}}}},
	}

	buildCluster := func(status metav1.ConditionStatus, reason string, fenced string) *apiv1.Cluster {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{ReplicaCluster: &apiv1.ReplicaClusterConfiguration{Enabled: ptr.To(true)}},
		}
		if status != "" {
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:   string(apiv1.ConditionConsistentSystemID),
				Status: status,
				Reason: reason,
			})
		}
		if fenced != "" {
			cluster.Annotations = map[string]string{utils.FencedInstanceAnnotation: fenced}
		}
		return cluster
	}

	DescribeTable("evaluates the cluster and the sampled instances",
		func(ctx SpecContext, cluster *apiv1.Cluster, statuses postgres.PostgresqlStatusList, expected bool) {
			Expect(shouldRefreshSystemID(ctx, cluster, statuses)).To(Equal(expected))
		},
		Entry("instances present and none reported a system ID",
			buildCluster(metav1.ConditionFalse, systemIDNotFoundReason, ""), instances, true),
		Entry("the cluster is not a replica cluster",
			func() *apiv1.Cluster {
				cluster := buildCluster(metav1.ConditionFalse, systemIDNotFoundReason, "")
				cluster.Spec.ReplicaCluster = nil
				return cluster
			}(), instances, false),
		Entry("no instances are present",
			buildCluster(metav1.ConditionFalse, systemIDNotFoundReason, ""), postgres.PostgresqlStatusList{}, false),
		Entry("the system ID is consistent",
			buildCluster(metav1.ConditionTrue, "Unique", ""), instances, false),
		Entry("the system IDs mismatch",
			buildCluster(metav1.ConditionFalse, "Mismatch", ""), instances, false),
		Entry("the condition is not set",
			buildCluster("", "", ""), instances, false),
		Entry("an instance is fenced",
			buildCluster(metav1.ConditionFalse, systemIDNotFoundReason, `["pod-1"]`), instances, false),
		Entry("the fenced instances annotation cannot be parsed",
			buildCluster(metav1.ConditionFalse, systemIDNotFoundReason, `not-json`), instances, false),
		Entry("all the instances are fenced",
			buildCluster(metav1.ConditionFalse, systemIDNotFoundReason, `["*"]`), instances, false),
	)
})

var _ = Describe("withSystemIDRefresh", func() {
	instances := postgres.PostgresqlStatusList{
		Items: []postgres.PostgresqlStatus{{Pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "pod-1"}}}},
	}

	buildCluster := func(status metav1.ConditionStatus, reason string) *apiv1.Cluster {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{ReplicaCluster: &apiv1.ReplicaClusterConfiguration{Enabled: ptr.To(true)}},
		}
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   string(apiv1.ConditionConsistentSystemID),
			Status: status,
			Reason: reason,
		})
		return cluster
	}

	stale := func() *apiv1.Cluster { return buildCluster(metav1.ConditionFalse, systemIDNotFoundReason) }

	DescribeTable("adjusts the result of the reconciliation",
		func(
			ctx SpecContext,
			result ctrl.Result,
			cluster *apiv1.Cluster,
			expected ctrl.Result,
		) {
			Expect(withSystemIDRefresh(ctx, result, cluster, instances)).To(Equal(expected))
		},
		Entry("requests a requeue when the system ID has to be refreshed",
			ctrl.Result{}, stale(), ctrl.Result{RequeueAfter: systemIDRefreshDelay}),
		Entry("keeps a requeue already requested",
			ctrl.Result{RequeueAfter: time.Second}, stale(), ctrl.Result{RequeueAfter: time.Second}),
		Entry("does not requeue when the system ID is consistent",
			ctrl.Result{}, buildCluster(metav1.ConditionTrue, "Unique"), ctrl.Result{}),
	)
})
