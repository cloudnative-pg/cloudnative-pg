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

package webserver

import (
	"context"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("PluginBackupCommand.Start", func() {
	const namespace = "test"

	var (
		cluster *apiv1.Cluster
		backup  *apiv1.Backup
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: namespace},
		}
		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{Name: "test-backup", Namespace: namespace},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{Name: "test-cluster"},
				Method:  apiv1.BackupMethodPlugin,
				PluginConfiguration: &apiv1.BackupPluginConfiguration{
					Name: "test-plugin",
				},
			},
			Status: apiv1.BackupStatus{
				Phase: apiv1.BackupPhasePending,
			},
		}
	})

	It("patches the backup as running before returning", func() {
		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, backup).
			WithStatusSubresource(cluster, backup).
			Build()

		cmd := NewPluginBackupCommand(cluster, backup, cli, &record.FakeRecorder{})

		err := cmd.Start(context.Background())
		Expect(err).ToNot(HaveOccurred())

		var got apiv1.Backup
		Expect(cli.Get(context.Background(),
			client.ObjectKey{Namespace: namespace, Name: "test-backup"}, &got)).To(Succeed())
		Expect(string(got.Status.Phase)).To(Equal(string(apiv1.BackupPhaseRunning)))
	})

	It("returns an error and starts no work when the running-phase patch fails", func() {
		patchErr := errors.New("injected patch failure")
		cli := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, backup).
			WithStatusSubresource(cluster, backup).
			WithInterceptorFuncs(interceptor.Funcs{
				SubResourcePatch: func(
					_ context.Context, _ client.Client, _ string, _ client.Object,
					_ client.Patch, _ ...client.SubResourcePatchOption,
				) error {
					return patchErr
				},
			}).
			Build()

		cmd := NewPluginBackupCommand(cluster, backup, cli, &record.FakeRecorder{})

		err := cmd.Start(context.Background())
		Expect(err).To(HaveOccurred())

		// No goroutine was ever spawned to do plugin work: the stored object
		// still has the phase it started with, not "running" and not "failed"
		// (which is what invokeStart would have set had it run).
		var got apiv1.Backup
		Expect(cli.Get(context.Background(),
			client.ObjectKey{Namespace: namespace, Name: "test-backup"}, &got)).To(Succeed())
		Expect(string(got.Status.Phase)).To(Equal(string(apiv1.BackupPhasePending)))
	})
})
