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
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pooler_switchover_pause unit tests", func() {
	var env *testingEnvironment

	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	Describe("reconcileSwitchoverPause", func() {
		It("should skip when feature is disabled", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-1"
				c.Status.TargetPrimary = "pod-2" // switchover in progress
			})

			// Create pooler without pauseDuringSwitchover
			pooler := newFakePooler(env.client, cluster)

			_, err := env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Pooler should not be paused
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
		})

		It("should resume a pooler we paused when the feature is disabled", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-1"
				c.Status.TargetPrimary = "pod-2" // switchover still in progress
			})

			// Pooler we auto-paused (status + annotation, never spec), feature now off.
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-disabled-while-paused",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: pausedDuringSwitchoverActive,
					},
					Labels: map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster:   apiv1.LocalObjectReference{Name: cluster.Name},
					Type:      apiv1.PoolerTypeRW,
					Instances: ptr.To(int32(1)),
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover: ptr.To(false), // feature disabled
						PoolMode:              apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			pooler.Status.PausedForSwitchover = true
			pooler.Status.PausedForSwitchoverTimestamp = pgTime.GetCurrentTimestamp()
			err = env.client.Status().Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			_, err = env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())

			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
			Expect(updatedPooler.Status.PausedForSwitchover).To(BeFalse())
			// The user-owned spec was never touched.
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
		})

		It("should skip poolers without automated integration", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-1"
				c.Status.TargetPrimary = "pod-2"
			})

			// Create a pooler with manual auth (not automated integration)
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "pooler-manual",
					Namespace:   namespace,
					Annotations: map[string]string{},
					Labels:      map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
					Type:    apiv1.PoolerTypeRW,
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover: ptr.To(true),
						AuthQuery:             "SELECT custom_auth($1)", // Custom auth = not automated
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			_, err = env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Pooler should not be paused
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
		})

		It("should skip already manually paused poolers", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-1"
				c.Status.TargetPrimary = "pod-2"
			})

			// Create a pooler that is already paused (manually)
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "pooler-manual-pause",
					Namespace:   namespace,
					Annotations: map[string]string{},
					Labels:      map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster: apiv1.LocalObjectReference{Name: cluster.Name},
					Type:    apiv1.PoolerTypeRW,
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover: ptr.To(true),
						Paused:                ptr.To(true), // Already paused
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			_, err = env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Pooler should not have our annotation (we didn't pause it)
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
			Expect(updatedPooler.Status.PausedForSwitchover).To(BeFalse())
		})

		It("should pause when switchover detected", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-1"
				c.Status.TargetPrimary = "pod-2" // switchover in progress
			})

			// Create pooler with pauseDuringSwitchover enabled
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "pooler-auto-pause",
					Namespace:   namespace,
					Annotations: map[string]string{},
					Labels:      map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster:   apiv1.LocalObjectReference{Name: cluster.Name},
					Type:      apiv1.PoolerTypeRW,
					Instances: ptr.To(int32(1)),
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover: ptr.To(true),
						PoolMode:              apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			requeueAfter, err := env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			// It must requeue at the timeout (default 120s) so the safety force-resume
			// fires even if no further cluster event arrives.
			Expect(requeueAfter).To(Equal(120 * time.Second))

			// Pooler should be marked paused via status + annotation, spec untouched.
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Status.PausedForSwitchover).To(BeTrue())
			Expect(updatedPooler.Status.PausedForSwitchoverTimestamp).ToNot(BeEmpty())
			Expect(updatedPooler.Annotations[utils.PausedDuringSwitchoverAnnotationName]).To(Equal("true"))
			// The user-owned spec is never mutated (no GitOps drift).
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
		})

		It("should resume when switchover completes", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-2"
				c.Status.TargetPrimary = "pod-2" // switchover complete
			})

			// Create a pooler that was auto-paused (status + annotation, never spec)
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-resume-test",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: "true",
					},
					Labels: map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster:   apiv1.LocalObjectReference{Name: cluster.Name},
					Type:      apiv1.PoolerTypeRW,
					Instances: ptr.To(int32(1)),
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover: ptr.To(true),
						PoolMode:              apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			// Set status to reflect auto-pause
			pooler.Status.PausedForSwitchover = true
			pooler.Status.PausedForSwitchoverTimestamp = pgTime.GetCurrentTimestamp()
			err = env.client.Status().Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			requeueAfter, err := env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			// Nothing left to wait for once resumed, so no self-triggered requeue.
			Expect(requeueAfter).To(BeZero())

			// Pooler should be resumed: status cleared, annotation gone, spec untouched.
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(updatedPooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
			Expect(updatedPooler.Status.PausedForSwitchover).To(BeFalse())
			Expect(updatedPooler.Status.PausedForSwitchoverTimestamp).To(BeEmpty())
		})

		It("should not double-pause if already paused for switchover", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-1"
				c.Status.TargetPrimary = "pod-2" // switchover in progress
			})

			// Create a pooler already paused for switchover
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-double-pause",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: "true",
					},
					Labels: map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster:   apiv1.LocalObjectReference{Name: cluster.Name},
					Type:      apiv1.PoolerTypeRW,
					Instances: ptr.To(int32(1)),
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover:        ptr.To(true),
						PauseDuringSwitchoverTimeout: &metav1.Duration{Duration: 5 * time.Minute},
						PoolMode:                     apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			// Set status to show already paused (recent timestamp)
			pooler.Status.PausedForSwitchover = true
			pooler.Status.PausedForSwitchoverTimestamp = pgTime.GetCurrentTimestamp()
			err = env.client.Status().Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			_, err = env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())

			// Pooler should still be paused (no change), spec still untouched.
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Status.PausedForSwitchover).To(BeTrue())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
		})

		It("should force resume after timeout exceeded", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-1"
				c.Status.TargetPrimary = "pod-2" // switchover still in progress
			})

			// Set timestamp to 10 minutes ago
			const rfc3339Micro = "2006-01-02T15:04:05.000000Z07:00"
			oldTimestamp := time.Now().Add(-10 * time.Minute).Format(rfc3339Micro)

			// Create a pooler that was auto-paused a while ago
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-timeout-test",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: "true",
					},
					Labels: map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster:   apiv1.LocalObjectReference{Name: cluster.Name},
					Type:      apiv1.PoolerTypeRW,
					Instances: ptr.To(int32(1)),
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover:        ptr.To(true),
						PauseDuringSwitchoverTimeout: &metav1.Duration{Duration: 5 * time.Minute},
						PoolMode:                     apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			// Set status with old timestamp
			pooler.Status.PausedForSwitchover = true
			pooler.Status.PausedForSwitchoverTimestamp = oldTimestamp
			err = env.client.Status().Update(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			requeueAfter, err := env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())
			// Timed out and gave up: no further self-triggered requeue.
			Expect(requeueAfter).To(BeZero())

			// Pooler should be force-resumed, keeping the timed-out marker so the same
			// switchover is not paused again.
			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(updatedPooler.Annotations).To(HaveKeyWithValue(
				utils.PausedDuringSwitchoverAnnotationName, pausedDuringSwitchoverTimedOut))
			Expect(updatedPooler.Status.PausedForSwitchover).To(BeFalse())
			Expect(updatedPooler.Status.PausedForSwitchoverTimestamp).To(BeEmpty())
		})

		It("should not re-pause a switchover that already timed out", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-1"
				c.Status.TargetPrimary = "pod-2" // switchover still in progress
			})

			// Pooler already force-resumed after timeout: marker set, not paused.
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-timedout-marker",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: pausedDuringSwitchoverTimedOut,
					},
					Labels: map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster:   apiv1.LocalObjectReference{Name: cluster.Name},
					Type:      apiv1.PoolerTypeRW,
					Instances: ptr.To(int32(1)),
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover: ptr.To(true),
						PoolMode:              apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			_, err = env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())

			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
			Expect(updatedPooler.Status.PausedForSwitchover).To(BeFalse())
		})

		It("should clear the timed-out marker once the switchover completes", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-2"
				c.Status.TargetPrimary = "pod-2" // switchover complete
			})

			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-clear-marker",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: pausedDuringSwitchoverTimedOut,
					},
					Labels: map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster:   apiv1.LocalObjectReference{Name: cluster.Name},
					Type:      apiv1.PoolerTypeRW,
					Instances: ptr.To(int32(1)),
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover: ptr.To(true),
						PoolMode:              apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())

			_, err = env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())

			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
		})

		It("should clear a leftover annotation when a partial write never set the status", func() {
			ctx := context.Background()
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace, func(c *apiv1.Cluster) {
				c.Status.CurrentPrimary = "pod-2"
				c.Status.TargetPrimary = "pod-2" // switchover complete
			})

			// Partial write: the annotation patch landed but the status patch did not.
			// Because pause is status-driven the pooler was never actually paused, so on
			// completion we just clear the stray annotation.
			pooler := &apiv1.Pooler{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pooler-partial-write",
					Namespace: namespace,
					Annotations: map[string]string{
						utils.PausedDuringSwitchoverAnnotationName: pausedDuringSwitchoverActive,
					},
					Labels: map[string]string{},
				},
				Spec: apiv1.PoolerSpec{
					Cluster:   apiv1.LocalObjectReference{Name: cluster.Name},
					Type:      apiv1.PoolerTypeRW,
					Instances: ptr.To(int32(1)),
					PgBouncer: &apiv1.PgBouncerSpec{
						PauseDuringSwitchover: ptr.To(true),
						PoolMode:              apiv1.PgBouncerPoolModeSession,
					},
				},
			}
			err := env.client.Create(ctx, pooler)
			Expect(err).ToNot(HaveOccurred())
			// Status intentionally left at its zero value (PausedForSwitchover == false).

			_, err = env.poolerReconciler.reconcileSwitchoverPause(ctx, pooler, cluster)
			Expect(err).ToNot(HaveOccurred())

			updatedPooler := &apiv1.Pooler{}
			err = env.client.Get(ctx, types.NamespacedName{Name: pooler.Name, Namespace: namespace}, updatedPooler)
			Expect(err).ToNot(HaveOccurred())
			Expect(updatedPooler.Annotations).ToNot(HaveKey(utils.PausedDuringSwitchoverAnnotationName))
			Expect(updatedPooler.Spec.PgBouncer.IsPaused()).To(BeFalse())
		})
	})
})
