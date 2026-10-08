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
	"sync"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// laggingBackupClient mimics an informer cache that has not yet observed the
// latest writes: while a snapshot is set, reads of that Backup return the
// snapshot. Writes always reach the underlying store.
type laggingBackupClient struct {
	client.Client

	mu       sync.Mutex
	snapshot *apiv1.Backup
}

func (l *laggingBackupClient) freeze(ctx context.Context, key client.ObjectKey) {
	var current apiv1.Backup
	Expect(l.Client.Get(ctx, key, &current)).To(Succeed())

	l.mu.Lock()
	defer l.mu.Unlock()
	l.snapshot = current.DeepCopy()
}

func (l *laggingBackupClient) sync() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.snapshot = nil
}

func (l *laggingBackupClient) Get(
	ctx context.Context,
	key client.ObjectKey,
	obj client.Object,
	opts ...client.GetOption,
) error {
	l.mu.Lock()
	snapshot := l.snapshot
	l.mu.Unlock()

	if backup, ok := obj.(*apiv1.Backup); ok && snapshot != nil && snapshot.Name == key.Name {
		snapshot.DeepCopyInto(backup)
		return nil
	}
	return l.Client.Get(ctx, key, obj, opts...)
}

var _ = Describe("backup_controller starting a backup", func() {
	const (
		clusterName = "cluster-example"
		podName     = "cluster-example-1"
		backupName  = "backup-example"
		image       = "ghcr.io/cloudnative-pg/postgresql:18.1"
	)

	var (
		store    client.WithWatch
		lagging  *laggingBackupClient
		backup   *apiv1.Backup
		key      client.ObjectKey
		starts   []string
		reconcil *BackupReconciler
	)

	reconcileOnce := func(ctx context.Context) {
		_, err := reconcil.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).ToNot(HaveOccurred())
	}

	expectStartedOnce := func(ctx context.Context) {
		Expect(starts).To(HaveLen(1))

		var stored apiv1.Backup
		Expect(lagging.Client.Get(ctx, key, &stored)).To(Succeed())
		Expect(string(stored.Status.Phase)).To(Equal(apiv1.BackupPhaseStarted))
		Expect(stored.Status.MajorVersion).To(Equal(18))
	}

	BeforeEach(func(ctx context.Context) {
		starts = nil
		scheme := schemeBuilder.BuildWithAllKnownScheme()
		store = fake.NewClientBuilder().WithScheme(scheme).
			WithStatusSubresource(&apiv1.Cluster{}, &apiv1.Backup{}).
			WithIndex(&corev1.Pod{}, podOwnerKey, func(rawObj client.Object) []string {
				if owner, ok := IsOwnedByCluster(rawObj.(*corev1.Pod)); ok {
					return []string{owner}
				}
				return nil
			}).
			WithIndex(&apiv1.Backup{}, ".spec.cluster.name", func(rawObj client.Object) []string {
				return []string{rawObj.(*apiv1.Backup).Spec.Cluster.Name}
			}).
			WithIndex(&apiv1.Backup{}, backupPhase, func(rawObj client.Object) []string {
				return []string{string(rawObj.(*apiv1.Backup).Status.Phase)}
			}).
			Build()
		lagging = &laggingBackupClient{Client: store}

		namespace := newFakeNamespace(store)

		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: namespace},
			Spec: apiv1.ClusterSpec{
				ImageName: image,
				Backup: &apiv1.BackupConfiguration{
					BarmanObjectStore: &apiv1.BarmanObjectStoreConfiguration{DestinationPath: "s3://bucket"},
				},
			},
			Status: apiv1.ClusterStatus{
				TargetPrimary:   podName,
				Image:           image,
				InstancesStatus: map[apiv1.PodStatus][]string{apiv1.PodHealthy: {podName}},
			},
		}
		Expect(store.Create(ctx, cluster)).To(Succeed())
		Expect(store.Status().Update(ctx, cluster)).To(Succeed())

		Expect(store.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.GetServerCASecretName(),
				Namespace: namespace,
			},
			Data: map[string][]byte{certs.CACertKey: []byte("ca")},
		})).To(Succeed())

		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: apiv1.SchemeGroupVersion.String(),
					Kind:       apiv1.ClusterKind,
					Name:       clusterName,
					Controller: ptr.To(true),
				}},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: specs.PostgresContainerName, Image: image}},
			},
		}
		Expect(store.Create(ctx, pod)).To(Succeed())
		pod.Status = corev1.PodStatus{
			Phase:      corev1.PodRunning,
			Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:        specs.PostgresContainerName,
				ContainerID: "containerd://abc",
				Ready:       true,
			}},
		}
		Expect(store.Status().Update(ctx, pod)).To(Succeed())

		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{Name: backupName, Namespace: namespace},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{Name: clusterName},
				Method:  apiv1.BackupMethodBarmanObjectStore,
				Target:  apiv1.BackupTargetPrimary,
			},
		}
		Expect(store.Create(ctx, backup)).To(Succeed())
		key = client.ObjectKeyFromObject(backup)

		reconcil = &BackupReconciler{
			Client:               lagging,
			Scheme:               scheme,
			Recorder:             record.NewFakeRecorder(120),
			instanceStatusClient: &fakeInstanceStatusClient{sessionID: "session"},
			execInstanceBackup: func(_ context.Context, _ *corev1.Pod, name string) (string, string, error) {
				starts = append(starts, name)
				return "", "", nil
			},
		}
	})

	It("starts the backup once when the cache has observed the first start", func(ctx context.Context) {
		reconcileOnce(ctx)
		lagging.sync()
		reconcileOnce(ctx)

		expectStartedOnce(ctx)
	})

	It("starts the backup once when the cache lags behind the first start", func(ctx context.Context) {
		// The second reconciliation is already queued by an earlier event and
		// runs before the cache observes the status written by the first one.
		lagging.freeze(ctx, key)
		reconcileOnce(ctx)
		reconcileOnce(ctx)

		expectStartedOnce(ctx)
	})

	It("retries instead of failing the backup when the start patch fails", func(ctx context.Context) {
		failOnce := true
		lagging.Client = interceptor.NewClient(store, interceptor.Funcs{
			SubResourcePatch: func(
				ctx context.Context,
				cli client.Client,
				subResourceName string,
				obj client.Object,
				patch client.Patch,
				opts ...client.SubResourcePatchOption,
			) error {
				if _, isBackup := obj.(*apiv1.Backup); isBackup && failOnce {
					failOnce = false
					return apierrs.NewTimeoutError("timeout", 1)
				}
				return cli.SubResource(subResourceName).Patch(ctx, obj, patch, opts...)
			},
		})

		_, err := reconcil.Reconcile(ctx, ctrl.Request{NamespacedName: key})
		Expect(err).To(HaveOccurred())
		Expect(starts).To(BeEmpty())

		var stored apiv1.Backup
		Expect(lagging.Client.Get(ctx, key, &stored)).To(Succeed())
		Expect(string(stored.Status.Phase)).ToNot(Equal(apiv1.BackupPhaseFailed))

		reconcileOnce(ctx)
		expectStartedOnce(ctx)
	})
})
