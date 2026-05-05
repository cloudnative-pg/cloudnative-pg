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

	"github.com/robfig/cron"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func newScheduledBackupTestClient() client.Client {
	scheme := schemeBuilder.BuildWithAllKnownScheme()
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&apiv1.ScheduledBackup{}, &apiv1.Backup{}).
		Build()
}

var _ = Describe("scheduledbackup createBackup", func() {
	var (
		cli        client.Client
		ns         string
		sb         *apiv1.ScheduledBackup
		sched      cron.Schedule
		recorder   *record.FakeRecorder
		backupTime time.Time
		now        time.Time
	)

	BeforeEach(func(ctx context.Context) {
		cli = newScheduledBackupTestClient()
		ns = newFakeNamespace(cli)

		sb = &apiv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sb-test",
				Namespace: ns,
			},
			Spec: apiv1.ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
				Cluster:  apiv1.LocalObjectReference{Name: "cluster-x"},
			},
		}
		Expect(cli.Create(ctx, sb)).To(Succeed())

		var err error
		sched, err = cron.Parse(sb.Spec.Schedule)
		Expect(err).ToNot(HaveOccurred())

		recorder = record.NewFakeRecorder(10)
		backupTime = time.Date(2026, 4, 17, 22, 35, 42, 0, time.UTC)
		now = time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC)
	})

	It("creates a backup and advances status", func(ctx context.Context) {
		result, err := createBackup(ctx, recorder, cli, sb, backupTime, now, sched, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))

		backupName := sb.BackupName(backupTime)
		var backup apiv1.Backup
		Expect(cli.Get(ctx, types.NamespacedName{Name: backupName, Namespace: ns}, &backup)).To(Succeed())

		var stored apiv1.ScheduledBackup
		Expect(cli.Get(ctx, types.NamespacedName{Name: sb.Name, Namespace: ns}, &stored)).To(Succeed())
		Expect(stored.Status.LastCheckTime).ToNot(BeNil())
		Expect(stored.Status.LastCheckTime.Time).To(BeTemporally("==", now))
		Expect(stored.Status.LastScheduleTime).ToNot(BeNil())
		Expect(stored.Status.LastScheduleTime.Time).To(BeTemporally("==", backupTime))
		Expect(stored.Status.NextScheduleTime).ToNot(BeNil())
	})

	It("requeues without advancing status when Create races with an existing backup", func(ctx context.Context) {
		// In production, the up-front Get in ReconcileScheduledBackup catches a
		// pre-existing Backup before reaching createBackup. This branch fires
		// only when the cache was stale at Get time but the Backup is already
		// in the apiserver — a transient race. We requeue so the next reconcile
		// observes the existing Backup and advances the status from there.
		backupName := sb.BackupName(backupTime)
		existing := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName,
				Namespace: ns,
			},
			Spec: apiv1.BackupSpec{Cluster: sb.Spec.Cluster},
		}
		Expect(cli.Create(ctx, existing)).To(Succeed())

		result, err := createBackup(ctx, recorder, cli, sb, backupTime, now, sched, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Second))

		var stored apiv1.ScheduledBackup
		Expect(cli.Get(ctx, types.NamespacedName{Name: sb.Name, Namespace: ns}, &stored)).To(Succeed())
		Expect(stored.Status.LastCheckTime).To(BeNil())
		Expect(stored.Status.LastScheduleTime).To(BeNil())
	})
})

var _ = Describe("scheduledbackup ReconcileScheduledBackup", func() {
	var (
		cli      client.Client
		ns       string
		sb       *apiv1.ScheduledBackup
		recorder *record.FakeRecorder
	)

	BeforeEach(func(ctx context.Context) {
		cli = newScheduledBackupTestClient()
		ns = newFakeNamespace(cli)

		sb = &apiv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "sb-test",
				Namespace: ns,
			},
			Spec: apiv1.ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
				Cluster:  apiv1.LocalObjectReference{Name: "cluster-x"},
			},
			// Pre-populated status so we skip the first-time-init path. The
			// schedule next-fire from this moment is in the past relative to
			// time.Now(), so the reconciler proceeds to the Get-first/createBackup
			// branch instead of waiting.
			Status: apiv1.ScheduledBackupStatus{
				LastCheckTime: &metav1.Time{Time: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			},
		}
		Expect(cli.Create(ctx, sb)).To(Succeed())
		Expect(cli.Status().Update(ctx, sb)).To(Succeed())

		recorder = record.NewFakeRecorder(10)
	})

	It("creates a Backup and advances status when none exists for the iteration", func(ctx context.Context) {
		originalLastCheck := sb.Status.LastCheckTime.Time

		result, err := ReconcileScheduledBackup(ctx, recorder, cli, sb)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))

		var backups apiv1.BackupList
		Expect(cli.List(ctx, &backups, client.InNamespace(ns))).To(Succeed())
		Expect(backups.Items).To(HaveLen(1))

		var stored apiv1.ScheduledBackup
		Expect(cli.Get(ctx, types.NamespacedName{Name: sb.Name, Namespace: ns}, &stored)).To(Succeed())
		Expect(stored.Status.LastCheckTime).ToNot(BeNil())
		Expect(stored.Status.LastCheckTime.Time).To(BeTemporally(">", originalLastCheck))
	})

	It("adopts an already-existing Backup for the upcoming iteration and advances status (#10562)",
		func(ctx context.Context) {
			// Reproduces the #10562 stuck loop. Compute the deterministic name the
			// reconciler will derive from the scheduled-iteration time and
			// pre-create that Backup. The Get-first observation must adopt it and
			// advance the status; no new Backup must be created.
			originalLastCheck := sb.Status.LastCheckTime.Time
			schedule, err := cron.Parse(sb.Spec.Schedule)
			Expect(err).ToNot(HaveOccurred())
			expectedBackupTime := schedule.Next(originalLastCheck)
			expectedName := sb.BackupName(expectedBackupTime)

			existing := &apiv1.Backup{
				ObjectMeta: metav1.ObjectMeta{
					Name:      expectedName,
					Namespace: ns,
					Labels: map[string]string{
						ParentScheduledBackupLabelName: sb.Name,
					},
				},
				Spec: apiv1.BackupSpec{Cluster: sb.Spec.Cluster},
			}
			Expect(cli.Create(ctx, existing)).To(Succeed())

			result, err := ReconcileScheduledBackup(ctx, recorder, cli, sb)
			Expect(err).ToNot(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))

			// No additional Backup created.
			var backups apiv1.BackupList
			Expect(cli.List(ctx, &backups, client.InNamespace(ns))).To(Succeed())
			Expect(backups.Items).To(HaveLen(1))
			Expect(backups.Items[0].Name).To(Equal(expectedName))

			// Status advanced: LastCheckTime moved past the original, and
			// LastScheduleTime matches the deterministic backupTime of the
			// observed Backup. This is what breaks the loop on the next pass.
			var stored apiv1.ScheduledBackup
			Expect(cli.Get(ctx, types.NamespacedName{Name: sb.Name, Namespace: ns}, &stored)).To(Succeed())
			Expect(stored.Status.LastCheckTime).ToNot(BeNil())
			Expect(stored.Status.LastCheckTime.Time).To(BeTemporally(">", originalLastCheck))
			Expect(stored.Status.LastScheduleTime).ToNot(BeNil())
			Expect(stored.Status.LastScheduleTime.Time).To(BeTemporally("==", expectedBackupTime))
		})
})

var _ = Describe("scheduledbackup ReconcileScheduledBackup immediate", func() {
	It("adopts an existing immediate Backup instead of creating a duplicate", func(ctx context.Context) {
		// Operator-restart corner case: a previous reconcile created the
		// immediate Backup but did not land the status patch, so
		// LastCheckTime is still nil. On retry, time.Now() differs from the
		// previous attempt, so the deterministic name is different. Without
		// observing what's already there, the controller would create a
		// second immediate Backup. This test pins that the existing
		// immediate Backup is adopted instead.
		cli := newScheduledBackupTestClient()
		ns := newFakeNamespace(cli)

		sb := &apiv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "sb-test", Namespace: ns},
			Spec: apiv1.ScheduledBackupSpec{
				Schedule:  "0 0 0 * * *",
				Cluster:   apiv1.LocalObjectReference{Name: "cluster-x"},
				Immediate: ptr.To(true),
			},
		}
		Expect(cli.Create(ctx, sb)).To(Succeed())

		// Pre-create an immediate Backup as if a previous reconcile had
		// created it. The compactISO8601 time intentionally differs from
		// what time.Now() would produce now, to mimic the restart-after-Create
		// scenario.
		existing := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      sb.BackupName(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)),
				Namespace: ns,
				Labels: map[string]string{
					ParentScheduledBackupLabelName: sb.Name,
					utils.ImmediateBackupLabelName: "true",
					utils.ClusterLabelName:         sb.Spec.Cluster.Name,
				},
			},
			Spec: apiv1.BackupSpec{Cluster: sb.Spec.Cluster},
		}
		Expect(cli.Create(ctx, existing)).To(Succeed())

		recorder := record.NewFakeRecorder(10)
		result, err := ReconcileScheduledBackup(ctx, recorder, cli, sb)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))

		// Only the pre-existing Backup remains; the controller did not create a duplicate.
		var backups apiv1.BackupList
		Expect(cli.List(ctx, &backups, client.InNamespace(ns))).To(Succeed())
		Expect(backups.Items).To(HaveLen(1))
		Expect(backups.Items[0].Name).To(Equal(existing.Name))

		var stored apiv1.ScheduledBackup
		Expect(cli.Get(ctx, types.NamespacedName{Name: sb.Name, Namespace: ns}, &stored)).To(Succeed())
		Expect(stored.Status.LastCheckTime).ToNot(BeNil())
	})
})

var _ = Describe("scheduledbackup advanceScheduledBackupStatus", func() {
	It("requeues without error when the status patch hits a Conflict", func(ctx context.Context) {
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli := fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.ScheduledBackup{}, &apiv1.Backup{}).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(
					_ context.Context,
					_ client.Client,
					subResourceName string,
					obj client.Object,
					_ client.Patch,
					_ ...client.SubResourcePatchOption,
				) error {
					if subResourceName == "status" {
						if _, ok := obj.(*apiv1.ScheduledBackup); ok {
							return apierrs.NewConflict(
								schema.GroupResource{Group: apiv1.SchemeGroupVersion.Group, Resource: "scheduledbackups"},
								obj.GetName(),
								nil,
							)
						}
					}
					return nil
				},
			}).
			Build()
		ns := newFakeNamespace(cli)

		sb := &apiv1.ScheduledBackup{
			ObjectMeta: metav1.ObjectMeta{Name: "sb-test", Namespace: ns},
			Spec: apiv1.ScheduledBackupSpec{
				Schedule: "0 0 0 * * *",
				Cluster:  apiv1.LocalObjectReference{Name: "cluster-x"},
			},
		}
		Expect(cli.Create(ctx, sb)).To(Succeed())

		schedule, err := cron.Parse(sb.Spec.Schedule)
		Expect(err).ToNot(HaveOccurred())
		recorder := record.NewFakeRecorder(10)
		now := time.Now()
		backupTime := schedule.Next(now)

		result, err := advanceScheduledBackupStatus(ctx, recorder, cli, sb, backupTime, now, schedule)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(time.Second))
	})
})
