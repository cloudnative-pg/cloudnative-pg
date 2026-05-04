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
	"fmt"
	"time"

	pgTime "github.com/cloudnative-pg/machinery/pkg/postgres/time"
	"github.com/robfig/cron"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		cli = fake.NewClientBuilder().
			WithScheme(scheme).
			WithStatusSubresource(&apiv1.ScheduledBackup{}, &apiv1.Backup{}).
			Build()
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

	It("creates a backup and advances status when no backup exists", func(ctx context.Context) {
		result, err := createBackup(ctx, recorder, cli, sb, backupTime, now, sched, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))

		backupName := fmt.Sprintf("%s-%s", sb.Name, pgTime.ToCompactISO8601(backupTime))
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

	It("does not loop when a backup with the deterministic name already exists", func(ctx context.Context) {
		// Reproduces issue #10562: a previous iteration persisted the Backup but
		// its scheduled-backup status patch never landed (lost response or transient
		// error), so LastCheckTime is still at its pre-creation value. The next
		// reconcile recomputes the same deterministic backup name, so apiserver
		// returns AlreadyExists. The controller must treat that as a no-op success
		// and advance LastCheckTime, otherwise reconciliation loops forever.
		backupName := fmt.Sprintf("%s-%s", sb.Name, pgTime.ToCompactISO8601(backupTime))
		existing := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backupName,
				Namespace: ns,
			},
			Spec: apiv1.BackupSpec{
				Cluster: sb.Spec.Cluster,
			},
		}
		Expect(cli.Create(ctx, existing)).To(Succeed())

		result, err := createBackup(ctx, recorder, cli, sb, backupTime, now, sched, false)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(BeNumerically(">", time.Duration(0)))

		var stored apiv1.ScheduledBackup
		Expect(cli.Get(ctx, types.NamespacedName{Name: sb.Name, Namespace: ns}, &stored)).To(Succeed())
		Expect(stored.Status.LastCheckTime).ToNot(BeNil())
		Expect(stored.Status.LastCheckTime.Time).To(BeTemporally("==", now))
		Expect(stored.Status.LastScheduleTime).ToNot(BeNil())
		Expect(stored.Status.LastScheduleTime.Time).To(BeTemporally("==", backupTime))
	})
})
