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

package autoresize

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Reconciler Helpers", func() {
	Context("IsAutoResizeEnabled", func() {
		It("should return true if data storage resize is enabled", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Resize: &apiv1.ResizeConfiguration{Enabled: true},
					},
				},
			}
			Expect(IsAutoResizeEnabled(cluster)).To(BeTrue())
		})

		It("should return true if WAL storage resize is enabled", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					WalStorage: &apiv1.StorageConfiguration{
						Resize: &apiv1.ResizeConfiguration{Enabled: true},
					},
				},
			}
			Expect(IsAutoResizeEnabled(cluster)).To(BeTrue())
		})

		It("should return true if any tablespace resize is enabled", func() {
			cluster := &apiv1.Cluster{
				Spec: apiv1.ClusterSpec{
					Tablespaces: []apiv1.TablespaceConfiguration{
						{
							Name: "t1",
							Storage: apiv1.StorageConfiguration{
								Resize: &apiv1.ResizeConfiguration{Enabled: true},
							},
						},
					},
				},
			}
			Expect(IsAutoResizeEnabled(cluster)).To(BeTrue())
		})

		It("should return false if nothing is enabled", func() {
			cluster := &apiv1.Cluster{}
			Expect(IsAutoResizeEnabled(cluster)).To(BeFalse())
		})
	})

	Context("getResizeConfigForPVC", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Resize: &apiv1.ResizeConfiguration{Enabled: true},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Resize: &apiv1.ResizeConfiguration{Enabled: false},
				},
				Tablespaces: []apiv1.TablespaceConfiguration{
					{
						Name: "t1",
						Storage: apiv1.StorageConfiguration{
							Resize: &apiv1.ResizeConfiguration{Enabled: true},
						},
					},
				},
			},
		}

		It("should return data resize config", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgData)},
				},
			}
			config := getResizeConfigForPVC(cluster, pvc)
			Expect(config).ToNot(BeNil())
			Expect(config.Enabled).To(BeTrue())
		})

		It("should return WAL resize config", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgWal)},
				},
			}
			config := getResizeConfigForPVC(cluster, pvc)
			Expect(config).ToNot(BeNil())
			Expect(config.Enabled).To(BeFalse())
		})

		It("should return tablespace resize config", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.PvcRoleLabelName:        string(utils.PVCRolePgTablespace),
						utils.TablespaceNameLabelName: "t1",
					},
				},
			}
			config := getResizeConfigForPVC(cluster, pvc)
			Expect(config).ToNot(BeNil())
			Expect(config.Enabled).To(BeTrue())
		})

		It("should return nil for unknown role", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{utils.PvcRoleLabelName: "unknown"},
				},
			}
			Expect(getResizeConfigForPVC(cluster, pvc)).To(BeNil())
		})
	})

	Context("getVolumeStatsForPVC", func() {
		diskStatus := &postgres.DiskStatus{
			DataVolume: &postgres.VolumeStatus{PercentUsed: 50},
			WALVolume:  &postgres.VolumeStatus{PercentUsed: 60},
			Tablespaces: map[string]*postgres.VolumeStatus{
				"t1": {PercentUsed: 70},
			},
		}

		It("should return data volume stats", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgData)},
				},
			}
			stats := getVolumeStatsForPVC(diskStatus, string(utils.PVCRolePgData), pvc)
			Expect(stats).ToNot(BeNil())
			Expect(stats.PercentUsed).To(Equal(float64(50)))
		})

		It("should return WAL volume stats", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgWal)},
				},
			}
			stats := getVolumeStatsForPVC(diskStatus, string(utils.PVCRolePgWal), pvc)
			Expect(stats).ToNot(BeNil())
			Expect(stats.PercentUsed).To(Equal(float64(60)))
		})

		It("should return tablespace volume stats", func() {
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						utils.PvcRoleLabelName:        string(utils.PVCRolePgTablespace),
						utils.TablespaceNameLabelName: "t1",
					},
				},
			}
			stats := getVolumeStatsForPVC(diskStatus, string(utils.PVCRolePgTablespace), pvc)
			Expect(stats).ToNot(BeNil())
			Expect(stats.PercentUsed).To(Equal(float64(70)))
		})

		It("should return nil if diskStatus is missing volume", func() {
			emptyStatus := &postgres.DiskStatus{}
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{utils.PvcRoleLabelName: string(utils.PVCRolePgData)},
				},
			}
			Expect(getVolumeStatsForPVC(emptyStatus, string(utils.PVCRolePgData), pvc)).To(BeNil())
		})
	})
})

var _ = Describe("Event History Pruning", func() {
	It("should prune old events and respect hard cap", func() {
		cluster := &apiv1.Cluster{}
		now := time.Now()

		// Add 60 events: 20 old, 40 new
		events := make([]apiv1.AutoResizeEvent, 60)
		for i := 0; i < 20; i++ {
			events[i] = apiv1.AutoResizeEvent{
				Timestamp: metav1.NewTime(now.Add(-30 * time.Hour)),
				PVCName:   "old",
			}
		}
		for i := 20; i < 60; i++ {
			events[i] = apiv1.AutoResizeEvent{
				Timestamp: metav1.NewTime(now.Add(-1 * time.Hour)),
				PVCName:   "new",
			}
		}
		cluster.Status.AutoResizeEvents = events

		newEvent := apiv1.AutoResizeEvent{
			Timestamp: metav1.NewTime(now),
			PVCName:   "latest",
		}

		appendResizeEvent(cluster, newEvent)

		// Should have 40 'new' + 1 'latest' = 41 events (since 20 'old' were pruned)
		Expect(cluster.Status.AutoResizeEvents).To(HaveLen(41))
		for _, e := range cluster.Status.AutoResizeEvents {
			Expect(e.PVCName).ToNot(Equal("old"))
		}
	})

	It("should respect the hard cap of 50 events", func() {
		cluster := &apiv1.Cluster{}
		now := time.Now()

		// Add 55 fresh events
		events := make([]apiv1.AutoResizeEvent, 55)
		for i := 0; i < 55; i++ {
			events[i] = apiv1.AutoResizeEvent{
				Timestamp: metav1.NewTime(now.Add(time.Duration(-i) * time.Minute)),
				PVCName:   "fresh",
			}
		}
		cluster.Status.AutoResizeEvents = events

		newEvent := apiv1.AutoResizeEvent{
			Timestamp: metav1.NewTime(now),
			PVCName:   "latest",
		}

		appendResizeEvent(cluster, newEvent)

		Expect(cluster.Status.AutoResizeEvents).To(HaveLen(50))
		Expect(cluster.Status.AutoResizeEvents[49].PVCName).To(Equal("latest"))
	})

	It("should filter out events with zero timestamps", func() {
		cluster := &apiv1.Cluster{}
		cluster.Status.AutoResizeEvents = []apiv1.AutoResizeEvent{
			{PVCName: "zero"}, // zero timestamp
		}

		newEvent := apiv1.AutoResizeEvent{
			Timestamp: metav1.Now(),
			PVCName:   "latest",
		}

		appendResizeEvent(cluster, newEvent)

		Expect(cluster.Status.AutoResizeEvents).To(HaveLen(1))
		Expect(cluster.Status.AutoResizeEvents[0].PVCName).To(Equal("latest"))
	})
})

var _ = Describe("reconcilePVC", func() {
	var (
		ctx      context.Context
		scheme   *runtime.Scheme
		recorder *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(apiv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		recorder = record.NewFakeRecorder(10)
	})

	It("should successfully resize a PVC when threshold is exceeded", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Resize: &apiv1.ResizeConfiguration{
						Enabled: true,
						Triggers: &apiv1.ResizeTriggers{
							UsageThreshold: ptr.To(80),
						},
					},
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size: "2Gi",
				},
			},
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Labels: map[string]string{
					utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
					utils.InstanceNameLabelName: "test-pod",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()

		diskInfoByPod := map[string]*InstanceDiskInfo{
			"test-pod": {
				DiskStatus: &postgres.DiskStatus{
					DataVolume: &postgres.VolumeStatus{
						PercentUsed:    85,
						AvailableBytes: 1 * 1024 * 1024 * 1024,
					},
				},
			},
		}

		resized, err := reconcilePVC(ctx, c, recorder, cluster, diskInfoByPod, pvc)
		Expect(err).ToNot(HaveOccurred())
		Expect(resized).To(BeTrue())

		// Verify PVC was patched
		var updatedPVC corev1.PersistentVolumeClaim
		Expect(c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "test-pvc"}, &updatedPVC)).To(Succeed())
		newSize := updatedPVC.Spec.Resources.Requests[corev1.ResourceStorage]
		Expect(newSize.String()).To(Equal("12Gi")) // 10Gi + 20% = 12Gi

		// Verify Event was recorded
		Expect(recorder.Events).To(Receive(ContainSubstring("Expanded volume test-pvc from 10Gi to 12Gi")))

		// Verify Cluster status was mutated
		Expect(cluster.Status.AutoResizeEvents).To(HaveLen(1))
		Expect(cluster.Status.AutoResizeEvents[0].PVCName).To(Equal("test-pvc"))
		Expect(cluster.Status.AutoResizeEvents[0].Result).To(Equal(apiv1.ResizeResultSuccess))
	})

	It("should block resize if rate limit is exceeded", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Resize: &apiv1.ResizeConfiguration{
						Enabled: true,
					},
				},
			},
			Status: apiv1.ClusterStatus{
				AutoResizeEvents: []apiv1.AutoResizeEvent{
					{
						PVCName:   "test-pvc",
						Timestamp: metav1.NewTime(time.Now().Add(-1 * time.Hour)),
					},
					{
						PVCName:   "test-pvc",
						Timestamp: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
					},
					{
						PVCName:   "test-pvc",
						Timestamp: metav1.NewTime(time.Now().Add(-3 * time.Hour)),
					},
				},
			},
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Labels: map[string]string{
					utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
					utils.InstanceNameLabelName: "test-pod",
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()

		diskInfoByPod := map[string]*InstanceDiskInfo{
			"test-pod": {
				DiskStatus: &postgres.DiskStatus{
					DataVolume: &postgres.VolumeStatus{PercentUsed: 90},
				},
			},
		}

		resized, err := reconcilePVC(ctx, c, recorder, cluster, diskInfoByPod, pvc)
		Expect(err).ToNot(HaveOccurred())
		Expect(resized).To(BeFalse())
		Expect(recorder.Events).To(Receive(ContainSubstring("Rate limit exceeded")))
	})

	It("should return error on invalid expansion limit", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Resize: &apiv1.ResizeConfiguration{
						Enabled: true,
						Expansion: &apiv1.ExpansionPolicy{
							Limit: "invalid",
						},
					},
				},
			},
		}

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pvc",
				Namespace: "default",
				Labels: map[string]string{
					utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
					utils.InstanceNameLabelName: "test-pod",
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(pvc).Build()

		diskInfoByPod := map[string]*InstanceDiskInfo{
			"test-pod": {
				DiskStatus: &postgres.DiskStatus{
					DataVolume: &postgres.VolumeStatus{PercentUsed: 90},
				},
			},
		}

		resized, err := reconcilePVC(ctx, c, recorder, cluster, diskInfoByPod, pvc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid expansion limit"))
		Expect(resized).To(BeFalse())
	})
})

var _ = Describe("Reconcile", func() {
	var (
		ctx      context.Context
		scheme   *runtime.Scheme
		recorder *record.FakeRecorder
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = runtime.NewScheme()
		Expect(apiv1.AddToScheme(scheme)).To(Succeed())
		Expect(corev1.AddToScheme(scheme)).To(Succeed())
		recorder = record.NewFakeRecorder(10)
	})

	It("should return empty result when auto-resize is disabled", func() {
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Resize: &apiv1.ResizeConfiguration{Enabled: false},
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(scheme).Build()

		result, err := Reconcile(ctx, c, recorder, cluster, nil, nil)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.IsZero()).To(BeTrue())
	})

	It("should successfully reconcile multiple PVCs", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Resize: &apiv1.ResizeConfiguration{
						Enabled: true,
						Triggers: &apiv1.ResizeTriggers{
							UsageThreshold: ptr.To(80),
						},
						Strategy: &apiv1.ResizeStrategy{
							WALSafetyPolicy: &apiv1.WALSafetyPolicy{
								AcknowledgeWALRisk: true,
							},
						},
					},
				},
			},
		}

		pvc1 := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-1",
				Namespace: "default",
				Labels: map[string]string{
					utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
					utils.InstanceNameLabelName: "pod-1",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&pvc1).Build()

		diskInfoByPod := map[string]*InstanceDiskInfo{
			"pod-1": {
				DiskStatus: &postgres.DiskStatus{
					DataVolume: &postgres.VolumeStatus{
						PercentUsed:    85,
						AvailableBytes: 1 * 1024 * 1024 * 1024,
					},
				},
				WALHealthStatus: &postgres.WALHealthStatus{
					ArchiveHealthy: true,
				},
			},
		}

		pvcs := []corev1.PersistentVolumeClaim{pvc1}

		result, err := Reconcile(ctx, c, recorder, cluster, diskInfoByPod, pvcs)
		Expect(err).ToNot(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDelay))

		// Verify PVC was patched
		var updatedPVC corev1.PersistentVolumeClaim
		Expect(c.Get(ctx, client.ObjectKey{Namespace: "default", Name: "pvc-1"}, &updatedPVC)).To(Succeed())
		newSize := updatedPVC.Spec.Resources.Requests[corev1.ResourceStorage]
		Expect(newSize.String()).To(Equal("12Gi"))
	})

	It("should return error when a PVC reconciliation fails", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Resize: &apiv1.ResizeConfiguration{
						Enabled: true,
						Expansion: &apiv1.ExpansionPolicy{
							Limit: "invalid",
						},
					},
				},
			},
		}

		pvc1 := corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pvc-1",
				Namespace: "default",
				Labels: map[string]string{
					utils.PvcRoleLabelName:      string(utils.PVCRolePgData),
					utils.InstanceNameLabelName: "pod-1",
				},
			},
		}

		c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&pvc1).Build()

		diskInfoByPod := map[string]*InstanceDiskInfo{
			"pod-1": {
				DiskStatus: &postgres.DiskStatus{
					DataVolume: &postgres.VolumeStatus{PercentUsed: 90},
				},
			},
		}

		pvcs := []corev1.PersistentVolumeClaim{pvc1}

		result, err := Reconcile(ctx, c, recorder, cluster, diskInfoByPod, pvcs)
		Expect(err).To(HaveOccurred())
		Expect(result.RequeueAfter).To(Equal(RequeueDelay))
	})
})
