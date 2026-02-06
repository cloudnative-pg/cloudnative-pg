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

package volumesnapshot

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getSnapshotName", func() {
	It("should return only the backup name when the role is PVCRolePgData", func() {
		name := persistentvolumeclaim.NewPgDataCalculator().GetSnapshotName("backup123")
		Expect(name).To(Equal("backup123"))
	})

	It("should append '-wal' to the backup name when the role is PVCRolePgWal", func() {
		name := persistentvolumeclaim.NewPgWalCalculator().GetSnapshotName("backup123")
		Expect(name).To(Equal("backup123-wal"))
	})
})

var _ = Describe("Volumesnapshot reconciler", func() {
	const (
		namespace   = "test-namespace"
		clusterName = "clusterName"
		backupName  = "theBackup"
	)
	var (
		cluster   *apiv1.Cluster
		targetPod *corev1.Pod
		pvcs      []corev1.PersistentVolumeClaim
		backup    *apiv1.Backup
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:   namespace,
				Name:        clusterName,
				Labels:      map[string]string{},
				Annotations: map[string]string{},
			},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Backup: &apiv1.BackupConfiguration{
					VolumeSnapshot: &apiv1.VolumeSnapshotConfiguration{
						ClassName: "csi-hostpath-snapclass",
						Online:    ptr.To(false),
					},
				},
			},
		}
		targetPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      clusterName + "-2",
			},
		}
		pvcs = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-2",
					Namespace: namespace,
					Labels: map[string]string{
						utils.PvcRoleLabelName: string(utils.PVCRolePgData),
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName + "-2-wal",
					Namespace: namespace,
					Labels: map[string]string{
						utils.PvcRoleLabelName: string(utils.PVCRolePgWal),
					},
				},
			},
		}
		startedAt := metav1.Now()
		stoppedAt := metav1.NewTime(time.Now().Add(time.Hour))

		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      backupName,
			},
			Status: apiv1.BackupStatus{
				StartedAt:    ptr.To(startedAt),
				StoppedAt:    ptr.To(stoppedAt),
				MajorVersion: 18,
			},
		}
	})

	It("should fence the target pod when there are no volumesnapshots", func(ctx SpecContext) {
		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(backup, cluster, targetPod).
			Build()

		fakeRecorder := record.NewFakeRecorder(3)

		executor := NewReconcilerBuilder(mockClient, fakeRecorder).
			Build()

		result, err := executor.Reconcile(ctx, cluster, backup, targetPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		var latestCluster apiv1.Cluster
		err = mockClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, &latestCluster)
		Expect(err).ToNot(HaveOccurred())

		data, err := utils.GetFencedInstances(latestCluster.Annotations)
		Expect(err).ToNot(HaveOccurred())
		Expect(data.Len()).To(Equal(1))
		Expect(data.Has(targetPod.Name)).To(BeTrue())

		var snapshotList volumesnapshotv1.VolumeSnapshotList
		err = mockClient.List(ctx, &snapshotList)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshotList.Items).NotTo(BeEmpty())
	})

	It("should not fence the target pod when there are existing volumesnapshots", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   namespace,
						Name:        backup.Name,
						Annotations: map[string]string{},
						Labels: map[string]string{
							utils.BackupNameLabelName: backup.Name,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:   namespace,
						Name:        backup.Name + "-wal",
						Annotations: map[string]string{},
						Labels: map[string]string{
							utils.BackupNameLabelName: backup.Name,
						},
					},
				},
			},
		}

		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, targetPod).
			WithStatusSubresource(backup).
			WithLists(&snapshots).
			Build()
		fakeRecorder := record.NewFakeRecorder(3)

		executor := NewReconcilerBuilder(mockClient, fakeRecorder).
			Build()

		result, err := executor.Reconcile(ctx, cluster, backup, targetPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		// we should have found snapshots that are not ready, and so we'd return to
		// wait for them to be ready
		Expect(result).ToNot(BeNil())

		var latestCluster apiv1.Cluster
		err = mockClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, &latestCluster)
		Expect(err).ToNot(HaveOccurred())

		data, err := utils.GetFencedInstances(latestCluster.Annotations)
		Expect(err).ToNot(HaveOccurred())
		Expect(data.Len()).To(Equal(0))
	})

	It("should unfence the target pod when the snapshots have been provisioned", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      backup.Name,
						Labels: map[string]string{
							utils.BackupNameLabelName: backup.Name,
						},
						Annotations: map[string]string{
							"avoid": "nil",
						},
					},
					Status: &volumesnapshotv1.VolumeSnapshotStatus{
						ReadyToUse:                     ptr.To(false),
						Error:                          nil,
						BoundVolumeSnapshotContentName: ptr.To(fmt.Sprintf("%s-content", backup.Name)),
						CreationTime:                   ptr.To(metav1.Now()),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      backup.Name + "-wal",
						Labels: map[string]string{
							utils.BackupNameLabelName: backup.Name,
						},
						Annotations: map[string]string{
							"avoid": "nil",
						},
					},
					Status: &volumesnapshotv1.VolumeSnapshotStatus{
						ReadyToUse:                     ptr.To(false),
						Error:                          nil,
						BoundVolumeSnapshotContentName: ptr.To(fmt.Sprintf("%s-wal-content", backup.Name)),
						CreationTime:                   ptr.To(metav1.Now()),
					},
				},
			},
		}

		cluster.Annotations[utils.FencedInstanceAnnotation] = fmt.Sprintf(`["%s"]`, targetPod.Name)

		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, targetPod, backup).
			WithStatusSubresource(backup).
			WithLists(&snapshots).
			Build()
		fakeRecorder := record.NewFakeRecorder(3)

		executor := NewReconcilerBuilder(mockClient, fakeRecorder).
			Build()

		result, err := executor.Reconcile(ctx, cluster, backup, targetPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		// we should have found snapshots that have been privisioned, so we need to
		// wait until they are ready in a next reconciliation loop
		Expect(result).ToNot(BeNil())

		var latestCluster apiv1.Cluster
		err = mockClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, &latestCluster)
		Expect(err).ToNot(HaveOccurred())

		data, err := utils.GetFencedInstances(latestCluster.Annotations)
		Expect(err).ToNot(HaveOccurred())
		Expect(data.Len()).To(Equal(0))

		var latestBackup apiv1.Backup
		err = mockClient.Get(ctx, types.NamespacedName{Name: backupName, Namespace: namespace}, &latestBackup)
		Expect(err).ToNot(HaveOccurred())

		Expect(latestBackup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseFinalizing))
	})

	It("should mark the backup as completed when the snapshots are ready", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      backup.Name,
						Labels: map[string]string{
							utils.BackupNameLabelName: backup.Name,
						},
						Annotations: map[string]string{
							"avoid": "nil",
						},
					},
					Status: &volumesnapshotv1.VolumeSnapshotStatus{
						BoundVolumeSnapshotContentName: ptr.To(fmt.Sprintf("%s-content", backup.Name)),
						ReadyToUse:                     ptr.To(true),
						Error:                          nil,
						CreationTime:                   ptr.To(metav1.Now()),
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Namespace: namespace,
						Name:      backup.Name + "-wal",
						Labels: map[string]string{
							utils.BackupNameLabelName: backup.Name,
						},
						Annotations: map[string]string{
							"avoid": "nil",
						},
					},
					Status: &volumesnapshotv1.VolumeSnapshotStatus{
						BoundVolumeSnapshotContentName: ptr.To(fmt.Sprintf("%s-wal-content", backup.Name)),
						ReadyToUse:                     ptr.To(true),
						Error:                          nil,
						CreationTime:                   ptr.To(metav1.Now()),
					},
				},
			},
		}

		cluster.Annotations[utils.FencedInstanceAnnotation] = fmt.Sprintf(`["%s"]`, targetPod.Name)

		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster, targetPod, backup).
			WithStatusSubresource(backup).
			WithLists(&snapshots).
			Build()
		fakeRecorder := record.NewFakeRecorder(3)

		executor := NewReconcilerBuilder(mockClient, fakeRecorder).
			Build()

		result, err := executor.Reconcile(ctx, cluster, backup, targetPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		// we should have found snapshots that are ready, and so the result
		// should be nil
		Expect(result).To(BeNil())

		var latestCluster apiv1.Cluster
		err = mockClient.Get(ctx, types.NamespacedName{Name: clusterName, Namespace: namespace}, &latestCluster)
		Expect(err).ToNot(HaveOccurred())

		data, err := utils.GetFencedInstances(latestCluster.Annotations)
		Expect(err).ToNot(HaveOccurred())
		Expect(data.Len()).To(Equal(0))
	})

	It("should properly enrich the backup with labels", func(ctx SpecContext) {
		mockClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(backup, cluster, targetPod).
			Build()

		fakeRecorder := record.NewFakeRecorder(3)

		executor := NewReconcilerBuilder(mockClient, fakeRecorder).
			Build()

		result, err := executor.Reconcile(ctx, cluster, backup, targetPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).ToNot(BeNil())

		var snapshotList volumesnapshotv1.VolumeSnapshotList
		err = mockClient.List(ctx, &snapshotList)
		Expect(err).ToNot(HaveOccurred())

		for _, snapshot := range snapshotList.Items {
			// Expected common labels
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.KubernetesAppManagedByLabelName, utils.ManagerName))
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.KubernetesAppLabelName, utils.AppName))
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.KubernetesAppInstanceLabelName, cluster.Name))
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.KubernetesAppVersionLabelName, "18"))
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.KubernetesAppComponentLabelName, utils.DatabaseComponentName))
			// Backup specific labels
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.BackupNameLabelName, backup.Name))
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.BackupDateLabelName, time.Now().Format("20060102")))
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.BackupMonthLabelName, time.Now().Format("200601")))
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.BackupYearLabelName, strconv.Itoa(time.Now().Year())))
			Expect(snapshot.Labels).To(HaveKeyWithValue(utils.MajorVersionLabelName, "18"))
		}
	})
})

var _ = Describe("transferLabelsToAnnotations", func() {
	const (
		exampleValueOne = "value1"
		exampleValueTwo = "value2"
	)

	var (
		labels      map[string]string
		annotations map[string]string
	)

	BeforeEach(func() {
		labels = make(map[string]string)
		annotations = make(map[string]string)
	})

	It("should not panic if labels or annotations are nil", func() {
		Expect(func() { transferLabelsToAnnotations(nil, annotations) }).ToNot(Panic())
		Expect(func() { transferLabelsToAnnotations(labels, nil) }).ToNot(Panic())
		Expect(func() { transferLabelsToAnnotations(nil, nil) }).ToNot(Panic())
	})

	It("should transfer specified labels to annotations", func() {
		labels[utils.ClusterInstanceRoleLabelName] = exampleValueOne
		labels[utils.InstanceNameLabelName] = exampleValueTwo
		//nolint:staticcheck
		labels[utils.ClusterRoleLabelName] = "value3"
		labels["extraLabel"] = "value4" // This should not be transferred

		transferLabelsToAnnotations(labels, annotations)

		Expect(annotations[utils.ClusterInstanceRoleLabelName]).To(Equal(exampleValueOne))
		Expect(annotations[utils.InstanceNameLabelName]).To(Equal(exampleValueTwo))
		//nolint:staticcheck
		Expect(annotations[utils.ClusterRoleLabelName]).To(Equal("value3"))
		Expect(annotations).ToNot(HaveKey("extraLabel"))

		Expect(labels).ToNot(HaveKey(utils.ClusterInstanceRoleLabelName))
		Expect(labels).ToNot(HaveKey(utils.InstanceNameLabelName))
		Expect(labels).ToNot(HaveKey("role"))
		Expect(labels["extraLabel"]).To(Equal("value4"))
	})

	It("should not modify annotations if label is not present", func() {
		labels[utils.ClusterInstanceRoleLabelName] = exampleValueOne
		//nolint:staticcheck
		labels[utils.ClusterRoleLabelName] = "value3"

		transferLabelsToAnnotations(labels, annotations)

		Expect(annotations[utils.ClusterInstanceRoleLabelName]).To(Equal(exampleValueOne))
		Expect(annotations).ToNot(HaveKey(utils.InstanceNameLabelName))
		//nolint:staticcheck
		Expect(annotations[utils.ClusterRoleLabelName]).To(Equal("value3"))
	})

	It("should leave annotations unchanged if no matching labels are found", func() {
		labels["nonMatchingLabel1"] = exampleValueOne
		labels["nonMatchingLabel2"] = exampleValueTwo

		transferLabelsToAnnotations(labels, annotations)

		Expect(annotations).To(BeEmpty())
		Expect(labels).To(HaveKeyWithValue("nonMatchingLabel1", exampleValueOne))
		Expect(labels).To(HaveKeyWithValue("nonMatchingLabel2", exampleValueTwo))
	})
})

var _ = Describe("annotateSnapshotsWithBackupData", func() {
	var (
		fakeClient   k8client.Client
		snapshots    volumesnapshotv1.VolumeSnapshotList
		backupStatus *apiv1.BackupStatus
		startedAt    metav1.Time
		stoppedAt    metav1.Time
	)

	BeforeEach(func() {
		snapshots = volumesnapshotv1.VolumeSnapshotList{
			Items: slice{
				{ObjectMeta: metav1.ObjectMeta{Name: "snapshot-1", Annotations: map[string]string{"avoid": "nil"}}},
				{ObjectMeta: metav1.ObjectMeta{Name: "snapshot-2", Annotations: map[string]string{"avoid": "nil"}}},
			},
		}
		startedAt = metav1.Now()
		stoppedAt = metav1.NewTime(time.Now().Add(time.Hour))
		backupStatus = &apiv1.BackupStatus{
			StartedAt: ptr.To(startedAt),
			StoppedAt: ptr.To(stoppedAt),
		}
		fakeClient = fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithLists(&snapshots).Build()
	})

	It("should update all snapshots with backup time annotations", func(ctx context.Context) {
		err := annotateSnapshotsWithBackupData(ctx, fakeClient, snapshots.Items, backupStatus)
		Expect(err).ToNot(HaveOccurred())

		for _, snapshot := range snapshots.Items {
			Expect(snapshot.Annotations[utils.BackupStartTimeAnnotationName]).To(BeEquivalentTo(startedAt.Format(time.RFC3339)))
			Expect(snapshot.Annotations[utils.BackupEndTimeAnnotationName]).To(BeEquivalentTo(stoppedAt.Format(time.RFC3339)))
		}
	})
})

var _ = Describe("addDeadlineStatus", func() {
	var (
		ctx    context.Context
		backup *apiv1.Backup
		cli    k8client.Client
	)

	BeforeEach(func() {
		ctx = context.TODO()
		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      "test-backup",
			},
			Status: apiv1.BackupStatus{
				PluginMetadata: make(map[string]string),
			},
		}
		cli = fake.NewClientBuilder().WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(backup).
			WithStatusSubresource(&apiv1.Backup{}).
			Build()
	})

	It("should add deadline status if not present", func() {
		err := addDeadlineStatus(ctx, cli, backup)
		Expect(err).ToNot(HaveOccurred())

		var updatedBackup apiv1.Backup
		err = cli.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: backup.Namespace}, &updatedBackup)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedBackup.Status.PluginMetadata).To(HaveKey(pluginName))
		Expect(updatedBackup.Status.PluginMetadata[pluginName]).ToNot(BeEmpty())
		Expect(updatedBackup.Status.PluginMetadata[pluginName]).To(MatchRegexp(`{"volumeSnapshotFirstFailure":\d+}`))
	})

	It("should not modify deadline status if already present", func() {
		backup.Status.PluginMetadata[pluginName] = `{"volumeSnapshotFirstFailure": 1234567890}`
		err := cli.Status().Update(ctx, backup)
		Expect(err).ToNot(HaveOccurred())

		err = addDeadlineStatus(ctx, cli, backup)
		Expect(err).ToNot(HaveOccurred())

		var updatedBackup apiv1.Backup
		err = cli.Get(ctx, types.NamespacedName{Name: backup.Name, Namespace: backup.Namespace}, &updatedBackup)
		Expect(err).ToNot(HaveOccurred())
		Expect(updatedBackup.Status.PluginMetadata[pluginName]).To(Equal(`{"volumeSnapshotFirstFailure": 1234567890}`))
	})
})

var _ = Describe("isDeadlineExceeded", func() {
	var backup *apiv1.Backup

	BeforeEach(func() {
		backup = &apiv1.Backup{
			Status: apiv1.BackupStatus{
				PluginMetadata: make(map[string]string),
			},
		}
	})

	It("should return an error if plugin metadata is empty", func() {
		_, err := isDeadlineExceeded(backup)
		Expect(err).To(HaveOccurred())
	})

	It("should return error if unmarshalling fails", func() {
		backup.Status.PluginMetadata[pluginName] = "invalid-json"
		exceeded, err := isDeadlineExceeded(backup)
		Expect(err).To(HaveOccurred())
		Expect(exceeded).To(BeFalse())
	})

	It("should return error if no volumeSnapshotFirstFailure found in plugin metadata", func() {
		backup.Status.PluginMetadata[pluginName] = `{}`
		exceeded, err := isDeadlineExceeded(backup)
		Expect(err).To(HaveOccurred())
		Expect(exceeded).To(BeFalse())
	})

	It("should return false if deadline has not exceeded", func() {
		data := metadata{VolumeSnapshotFirstDetectedFailure: time.Now().Unix()}
		rawData, _ := json.Marshal(data)
		backup.Status.PluginMetadata[pluginName] = string(rawData)
		backup.Annotations = map[string]string{utils.BackupVolumeSnapshotDeadlineAnnotationName: "10"}

		exceeded, err := isDeadlineExceeded(backup)
		Expect(err).ToNot(HaveOccurred())
		Expect(exceeded).To(BeFalse())
	})

	It("should return true if deadline has exceeded", func() {
		data := metadata{VolumeSnapshotFirstDetectedFailure: time.Now().Add(-20 * time.Minute).Unix()}
		rawData, _ := json.Marshal(data)
		backup.Status.PluginMetadata[pluginName] = string(rawData)
		backup.Annotations = map[string]string{utils.BackupVolumeSnapshotDeadlineAnnotationName: "10"}

		exceeded, err := isDeadlineExceeded(backup)
		Expect(err).ToNot(HaveOccurred())
		Expect(exceeded).To(BeTrue())
	})
})

var _ = Describe("unmarshalMetadata", func() {
	It("should unmarshal valid metadata correctly", func() {
		rawData := `{"volumeSnapshotFirstFailure": 1234567890}`
		data, err := unmarshalMetadata(rawData)
		Expect(err).ToNot(HaveOccurred())
		Expect(data.VolumeSnapshotFirstDetectedFailure).To(Equal(int64(1234567890)))
	})

	It("should return an error if rawData is invalid JSON", func() {
		rawData := `invalid-json`
		data, err := unmarshalMetadata(rawData)
		Expect(err).To(HaveOccurred())
		Expect(data).To(BeNil())
	})

	It("should return an error if volumeSnapshotFirstFailure is missing", func() {
		rawData := `{}`
		data, err := unmarshalMetadata(rawData)
		Expect(err).To(HaveOccurred())
		Expect(data).To(BeNil())
	})

	It("should return an error if volumeSnapshotFirstFailure is zero", func() {
		rawData := `{"volumeSnapshotFirstFailure": 0}`
		data, err := unmarshalMetadata(rawData)
		Expect(err).To(HaveOccurred())
		Expect(data).To(BeNil())
	})
})

type mockClient struct {
	k8client.Client
	createError error
}

func (m *mockClient) Create(ctx context.Context, obj k8client.Object, opts ...k8client.CreateOption) error {
	if m.createError != nil {
		return m.createError
	}
	return m.Client.Create(ctx, obj, opts...)
}

var _ = Describe("createSnapshot with race condition", func() {
	var (
		ctx       context.Context
		backup    *apiv1.Backup
		cluster   *apiv1.Cluster
		targetPod *corev1.Pod
		pvc       *corev1.PersistentVolumeClaim
	)

	BeforeEach(func() {
		ctx = context.TODO()
		backup = &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      "test-backup",
			},
			Status: apiv1.BackupStatus{
				StartedAt: ptr.To(metav1.Now()),
			},
		}
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      "test-cluster",
			},
			Spec: apiv1.ClusterSpec{
				Backup: &apiv1.BackupConfiguration{
					VolumeSnapshot: &apiv1.VolumeSnapshotConfiguration{
						ClassName: "csi-hostpath-snapclass",
					},
				},
			},
		}
		targetPod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      "test-pod",
			},
		}
		pvc = &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      "test-pvc",
				Labels: map[string]string{
					utils.PvcRoleLabelName: string(utils.PVCRolePgData),
				},
			},
		}
	})

	It("should succeed if Create fails but Get returns existing snapshot with UID", func() {
		// Prepare existing snapshot
		snapName := persistentvolumeclaim.NewPgDataCalculator().GetSnapshotName(backup.Name)
		existingSnapshot := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      snapName,
				UID:       types.UID("existing-uid"),
			},
			Spec: volumesnapshotv1.VolumeSnapshotSpec{
				Source: volumesnapshotv1.VolumeSnapshotSource{
					PersistentVolumeClaimName: &pvc.Name,
				},
			},
		}

		// Fake client with existing snapshot
		baseClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(existingSnapshot).
			Build()

		// Wraps with mock to inject Create error
		mClient := &mockClient{
			Client:      baseClient,
			createError: fmt.Errorf("simulated conflict error"),
		}

		reconciler := NewReconcilerBuilder(mClient, record.NewFakeRecorder(3)).Build()

		// We use createSnapshot directly to test the unexpected error handling logic there
		err := reconciler.createSnapshot(ctx, cluster, backup, targetPod, pvc)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail if Create fails and Get fails (snapshot does not exist)", func() {
		// Fake client WITHOUT existing snapshot
		baseClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			Build()

		// Wraps with mock to inject Create error
		mClient := &mockClient{
			Client:      baseClient,
			createError: fmt.Errorf("simulated conflict error"),
		}

		reconciler := NewReconcilerBuilder(mClient, record.NewFakeRecorder(3)).Build()

		err := reconciler.createSnapshot(ctx, cluster, backup, targetPod, pvc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated conflict error"))
	})

	It("should fail if Create fails and Get returns snapshot without UID", func() {
		// Prepare existing snapshot WITHOUT UID (simulating not yet persisted or some weird state)
		snapName := persistentvolumeclaim.NewPgDataCalculator().GetSnapshotName(backup.Name)
		existingSnapshot := &volumesnapshotv1.VolumeSnapshot{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-namespace",
				Name:      snapName,
				// No UID
			},
		}

		baseClient := fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(existingSnapshot).
			Build()

		mClient := &mockClient{
			Client:      baseClient,
			createError: fmt.Errorf("simulated conflict error"),
		}

		reconciler := NewReconcilerBuilder(mClient, record.NewFakeRecorder(3)).Build()

		err := reconciler.createSnapshot(ctx, cluster, backup, targetPod, pvc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("snapshot exists but has no UID"))
	})
})
