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

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	k8scheme "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensureClusterIsNotFenced", func() {
	var (
		mockCli k8client.Client
		cluster *apiv1.Cluster
	)

	getCluster := func(ctx context.Context, clusterKey k8client.ObjectKey) (*apiv1.Cluster, error) {
		remoteCluster := &apiv1.Cluster{}
		err := mockCli.Get(ctx, clusterKey, remoteCluster)
		return remoteCluster, err
	}

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
				Annotations: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
		}
		mockCli = fake.NewClientBuilder().
			WithScheme(k8scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()
	})

	Context("when no instances are fenced", func() {
		It("should not modify the object", func(ctx SpecContext) {
			origCluster, err := getCluster(ctx, k8client.ObjectKeyFromObject(cluster))
			Expect(err).ToNot(HaveOccurred())

			err = ensureClusterIsNotFenced(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())

			remoteCluster, err := getCluster(ctx, k8client.ObjectKeyFromObject(cluster))
			Expect(err).ToNot(HaveOccurred())
			Expect(remoteCluster.ObjectMeta).To(Equal(origCluster.ObjectMeta))
		})
	})

	Context("when fenced instances exist", func() {
		BeforeEach(func() {
			modified, err := utils.AddFencedInstance(utils.FenceAllInstances, &cluster.ObjectMeta)
			Expect(err).ToNot(HaveOccurred())
			Expect(modified).To(BeTrue())
			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster).
				Build()
		})

		It("should patch the cluster and remove fenced instances", func(ctx SpecContext) {
			origCluster, err := getCluster(ctx, k8client.ObjectKeyFromObject(cluster))
			Expect(err).ToNot(HaveOccurred())
			Expect(origCluster.Annotations).To(HaveKey(utils.FencedInstanceAnnotation))

			err = ensureClusterIsNotFenced(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())

			remoteCluster, err := getCluster(ctx, k8client.ObjectKeyFromObject(cluster))
			Expect(err).ToNot(HaveOccurred())

			Expect(remoteCluster.ObjectMeta).ToNot(Equal(origCluster.ObjectMeta))
			Expect(remoteCluster.Annotations).ToNot(HaveKey(utils.FencedInstanceAnnotation))
		})
	})
})

var _ = Describe("restoreClusterStatus", func() {
	var (
		mockCli k8client.Client
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}
		mockCli = fake.NewClientBuilder().
			WithScheme(k8scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			Build()
	})

	Context("when restoring cluster status", func() {
		It("should patch the cluster with the updated status", func(ctx SpecContext) {
			latestNodeSerial := 10
			targetPrimaryNodeSerial := 3

			err := restoreClusterStatus(ctx, mockCli, cluster, latestNodeSerial, targetPrimaryNodeSerial)
			Expect(err).ToNot(HaveOccurred())

			modifiedCluster := &apiv1.Cluster{}
			err = mockCli.Get(ctx, k8client.ObjectKeyFromObject(cluster), modifiedCluster)
			Expect(err).ToNot(HaveOccurred())

			Expect(modifiedCluster.Status.LatestGeneratedNode).To(Equal(latestNodeSerial))
			Expect(modifiedCluster.Status.TargetPrimary).To(
				Equal(specs.GetInstanceName(cluster.Name, targetPrimaryNodeSerial)))
		})
	})
})

var _ = Describe("getOrphanPVCs", func() {
	var (
		mockCli  k8client.Client
		cluster  *apiv1.Cluster
		goodPvcs []corev1.PersistentVolumeClaim
		badPvcs  []corev1.PersistentVolumeClaim
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}

		goodPvcs = []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-1",
					Namespace: "default",
					Annotations: map[string]string{
						utils.ClusterSerialAnnotationName: "1",
					},
					Labels: map[string]string{
						utils.ClusterLabelName:             cluster.Name,
						utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelReplica,
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-2",
					Namespace: "default",
					Annotations: map[string]string{
						utils.ClusterSerialAnnotationName: "2",
					},
					Labels: map[string]string{
						utils.ClusterLabelName:             cluster.Name,
						utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelPrimary,
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-3",
					Namespace: "default",
					Annotations: map[string]string{
						utils.ClusterSerialAnnotationName: "3",
					},
					Labels: map[string]string{
						utils.ClusterLabelName:             cluster.Name,
						utils.ClusterInstanceRoleLabelName: specs.ClusterRoleLabelReplica,
					},
				},
			},
		}

		badPvcs = []corev1.PersistentVolumeClaim{
			// does not have the serial annotation needs to be discarded
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-4",
					Namespace: "default",
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
					},
				},
			},
			// this one should be ignored given that it has owner references
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-55",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       "some-controller",
							Kind:       "any-kind",
							UID:        "3241",
							APIVersion: "v1",
						},
					},
					Annotations: map[string]string{
						utils.ClusterSerialAnnotationName: "55",
					},
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
					},
				},
			},
			// not relevant for us
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "random-1",
					Namespace: "default",
					Annotations: map[string]string{
						utils.ClusterSerialAnnotationName: "1",
					},
					Labels: map[string]string{
						utils.ClusterLabelName: "random",
					},
				},
			},
		}

		pvcList := &corev1.PersistentVolumeClaimList{
			Items: append(goodPvcs, badPvcs...),
		}

		mockCli = fake.NewClientBuilder().
			WithScheme(k8scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			WithStatusSubresource(cluster).
			WithLists(pvcList).
			Build()
	})

	It("should fetch only the pvcs that belong to the cluster and without an owner", func(ctx SpecContext) {
		remotePvcs, err := getOrphanPVCs(ctx, mockCli, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(remotePvcs).To(HaveLen(len(goodPvcs)))

		names := make([]string, len(remotePvcs))
		for idx := range remotePvcs {
			names[idx] = remotePvcs[idx].Name
		}

		for _, goodPvc := range goodPvcs {
			Expect(names).To(ContainElement(goodPvc.Name))
		}
	})

	It("should correctly calculate node serials from pvcs", func() {
		high, primary, err := getNodeSerialsFromPVCs(goodPvcs)
		Expect(err).ToNot(HaveOccurred())
		Expect(high).To(Equal(3))
		Expect(primary).To(Equal(2))
	})

	It("should correctly restore the orphan pvcs", func(ctx SpecContext) {
		err := restoreOrphanPVCs(ctx, mockCli, cluster, goodPvcs)
		Expect(err).ToNot(HaveOccurred())

		for _, pvc := range goodPvcs {
			Expect(pvc.OwnerReferences).ToNot(BeEmpty())
			Expect(pvc.Annotations[utils.PVCStatusAnnotationName]).To(Equal(persistentvolumeclaim.StatusReady))
		}
	})
})

var _ = Describe("ensureOrphanServicesAreNotPresent", func() {
	var (
		mockCli k8client.Client
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Services: &apiv1.ManagedServices{
						Additional: []apiv1.ManagedService{
							{
								SelectorType:   apiv1.ServiceSelectorTypeRW,
								UpdateStrategy: apiv1.ServiceUpdateStrategyPatch,
								ServiceTemplate: apiv1.ServiceTemplateSpec{
									ObjectMeta: apiv1.Metadata{
										Name: "test-rw-service",
									},
								},
							},
						},
					},
				},
			},
		}
		mockCli = fake.NewClientBuilder().
			WithScheme(k8scheme.BuildWithAllKnownScheme()).
			WithObjects(cluster).
			Build()
	})

	Context("when no orphan services are present", func() {
		It("should not return an error", func(ctx SpecContext) {
			err := ensureOrphanServicesAreNotPresent(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("when orphan services are present", func() {
		BeforeEach(func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.GetServiceReadWriteName(),
					Namespace: cluster.Namespace,
				},
			}
			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, svc).
				Build()
		})

		It("should delete the orphan services", func(ctx SpecContext) {
			err := ensureOrphanServicesAreNotPresent(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())

			var svc corev1.Service
			err = mockCli.Get(ctx,
				k8client.ObjectKey{Name: cluster.GetServiceReadWriteName(), Namespace: cluster.Namespace},
				&svc,
			)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		})

		Context("when orphan read services are present", func() {
			BeforeEach(func() {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cluster.GetServiceReadName(),
						Namespace: cluster.Namespace,
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, svc).
					Build()
			})

			It("should delete the orphan read services", func(ctx SpecContext) {
				err := ensureOrphanServicesAreNotPresent(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())

				var svc corev1.Service
				err = mockCli.Get(ctx,
					k8client.ObjectKey{Name: cluster.GetServiceReadName(), Namespace: cluster.Namespace},
					&svc,
				)
				Expect(apierrs.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("when orphan read-only services are present", func() {
			BeforeEach(func() {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cluster.GetServiceReadOnlyName(),
						Namespace: cluster.Namespace,
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, svc).
					Build()
			})

			It("should delete the orphan read-only services", func(ctx SpecContext) {
				err := ensureOrphanServicesAreNotPresent(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())

				var svc corev1.Service
				err = mockCli.Get(ctx,
					k8client.ObjectKey{Name: cluster.GetServiceReadOnlyName(), Namespace: cluster.Namespace},
					&svc,
				)
				Expect(apierrs.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("when orphan additional services are present", func() {
			BeforeEach(func() {
				svc := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rw-service",
						Namespace: cluster.Namespace,
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, svc).
					Build()
			})

			It("should delete the orphan additional services", func(ctx SpecContext) {
				err := ensureOrphanServicesAreNotPresent(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())

				var svc corev1.Service
				err = mockCli.Get(ctx,
					k8client.ObjectKey{Name: "test-rw-service", Namespace: cluster.Namespace},
					&svc,
				)
				Expect(apierrs.IsNotFound(err)).To(BeTrue())
			})
		})
	})

	Context("when services have owner references", func() {
		BeforeEach(func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.GetServiceReadWriteName(),
					Namespace: cluster.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							Name:       "some-controller",
							Kind:       "any-kind",
							UID:        "3241",
							APIVersion: "v1",
						},
					},
				},
			}
			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, svc).
				Build()
		})

		It("should return an error", func(ctx SpecContext) {
			err := ensureOrphanServicesAreNotPresent(ctx, mockCli, cluster)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("service has owner references and it is not orphan"))
		})
	})

	Context("when services are owned by the cluster", func() {
		BeforeEach(func() {
			svc := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cluster.GetServiceReadWriteName(),
					Namespace: cluster.Namespace,
				},
			}
			cluster.TypeMeta = metav1.TypeMeta{Kind: apiv1.ClusterKind, APIVersion: apiv1.SchemeGroupVersion.String()}
			cluster.SetInheritedDataAndOwnership(&svc.ObjectMeta)
			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, svc).
				Build()
		})

		It("should not return an error", func(ctx SpecContext) {
			err := ensureOrphanServicesAreNotPresent(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})

var _ = Describe("ensureInitContainersAreCompleted", func() {
	var (
		mockCli k8client.Client
		cluster *apiv1.Cluster
		pod     *corev1.Pod
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}
	})

	Context("when no pods with init containers exist", func() {
		BeforeEach(func() {
			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster).
				Build()
		})

		It("should return nil without error", func(ctx SpecContext) {
			res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeNil())
		})
	})

	Context("when a pod with init containers exists", func() {
		BeforeEach(func() {
			pod = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
						utils.PodRoleLabelName: string(utils.PodRoleInstance),
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "restore-init",
						},
					},
				},
			}
			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, pod).
				Build()
		})

		Context("when init containers have not started", func() {
			It("should requeue with 5 second delay", func(ctx SpecContext) {
				res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).ToNot(BeNil())
				Expect(res.RequeueAfter).To(Equal(5 * time.Second))
			})
		})

		Context("when init containers are still running", func() {
			BeforeEach(func() {
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
					{
						Name: "restore-init",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{},
						},
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, pod).
					Build()
			})

			It("should requeue with 5 second delay", func(ctx SpecContext) {
				res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).ToNot(BeNil())
				Expect(res.RequeueAfter).To(Equal(5 * time.Second))
			})
		})

		Context("when init containers completed successfully", func() {
			BeforeEach(func() {
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
					{
						Name: "restore-init",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 0,
								Reason:   "Completed",
							},
						},
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, pod).
					Build()
			})

			It("should return nil without requeue", func(ctx SpecContext) {
				res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).To(BeNil())
			})
		})

		Context("when init containers failed", func() {
			BeforeEach(func() {
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
					{
						Name: "restore-init",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 1,
								Reason:   "Error",
								Message:  "restore failed",
							},
						},
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, pod).
					Build()
			})

			It("should requeue with 10 second delay", func(ctx SpecContext) {
				res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).ToNot(BeNil())
				Expect(res.RequeueAfter).To(Equal(10 * time.Second))
			})
		})

		Context("when multiple non-sidecar init containers exist", func() {
			BeforeEach(func() {
				pod.Spec.InitContainers = []corev1.Container{
					{
						Name: "init-1",
					},
					{
						Name: "init-2",
					},
				}
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
					{
						Name: "init-1",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 0,
								Reason:   "Completed",
							},
						},
					},
					{
						Name: "init-2",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 0,
								Reason:   "Completed",
							},
						},
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, pod).
					Build()
			})

			It("should wait for all of them to complete", func(ctx SpecContext) {
				res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).To(BeNil())
			})

			Context("when one init container is still running", func() {
				BeforeEach(func() {
					pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
						{
							Name: "init-1",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 0,
									Reason:   "Completed",
								},
							},
						},
						{
							Name: "init-2",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
						},
					}
					mockCli = fake.NewClientBuilder().
						WithScheme(k8scheme.BuildWithAllKnownScheme()).
						WithObjects(cluster, pod).
						Build()
				})

				It("should requeue and wait", func(ctx SpecContext) {
					res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
					Expect(err).ToNot(HaveOccurred())
					Expect(res).ToNot(BeNil())
					Expect(res.RequeueAfter).To(Equal(5 * time.Second))
				})
			})

			Context("when one init container fails", func() {
				BeforeEach(func() {
					pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
						{
							Name: "init-1",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 0,
									Reason:   "Completed",
								},
							},
						},
						{
							Name: "init-2",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 1,
									Reason:   "Error",
									Message:  "init failed",
								},
							},
						},
					}
					mockCli = fake.NewClientBuilder().
						WithScheme(k8scheme.BuildWithAllKnownScheme()).
						WithObjects(cluster, pod).
						Build()
				})

				It("should requeue with 10 second delay", func(ctx SpecContext) {
					res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
					Expect(err).ToNot(HaveOccurred())
					Expect(res).ToNot(BeNil())
					Expect(res.RequeueAfter).To(Equal(10 * time.Second))
				})
			})
		})

		Context("when sidecar init containers are present", func() {
			BeforeEach(func() {
				alwaysRestart := corev1.ContainerRestartPolicyAlways
				pod.Spec.InitContainers = []corev1.Container{
					{
						Name: "restore-init",
					},
					{
						Name:          "sidecar-init",
						RestartPolicy: &alwaysRestart,
					},
				}
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
					{
						Name: "restore-init",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 0,
								Reason:   "Completed",
							},
						},
					},
					{
						Name: "sidecar-init",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{},
						},
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, pod).
					Build()
			})

			It("should ignore sidecar containers and return success when non-sidecar containers complete",
				func(ctx SpecContext) {
					res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
					Expect(err).ToNot(HaveOccurred())
					Expect(res).To(BeNil())
				})
		})

		Context("when pod has owner references but init containers completed", func() {
			BeforeEach(func() {
				pod.OwnerReferences = []metav1.OwnerReference{
					{
						Name:       "some-controller",
						Kind:       "Deployment",
						UID:        "12345",
						APIVersion: "apps/v1",
					},
				}
				pod.Status.InitContainerStatuses = []corev1.ContainerStatus{
					{
						Name: "restore-init",
						State: corev1.ContainerState{
							Terminated: &corev1.ContainerStateTerminated{
								ExitCode: 0,
								Reason:   "Completed",
							},
						},
					},
				}
				mockCli = fake.NewClientBuilder().
					WithScheme(k8scheme.BuildWithAllKnownScheme()).
					WithObjects(cluster, pod).
					Build()
			})

			It("should still check and wait for init containers", func(ctx SpecContext) {
				res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
				Expect(err).ToNot(HaveOccurred())
				Expect(res).To(BeNil())
			})
		})
	})
})

var _ = Describe("isSidecarInitContainer", func() {
	Context("when init container has no restart policy", func() {
		It("should return false", func() {
			container := &corev1.Container{
				Name:          "test-init",
				RestartPolicy: nil,
			}
			Expect(isSidecarInitContainer(container)).To(BeFalse())
		})
	})

	Context("when init container has Always restart policy", func() {
		It("should return true", func() {
			alwaysRestart := corev1.ContainerRestartPolicyAlways
			container := &corev1.Container{
				Name:          "test-init",
				RestartPolicy: &alwaysRestart,
			}
			Expect(isSidecarInitContainer(container)).To(BeTrue())
		})
	})

	Context("when init container has OnFailure restart policy", func() {
		It("should return false", func() {
			onFailureRestart := corev1.ContainerRestartPolicyOnFailure
			container := &corev1.Container{
				Name:          "test-init",
				RestartPolicy: &onFailureRestart,
			}
			Expect(isSidecarInitContainer(container)).To(BeFalse())
		})
	})

	Context("when init container has Never restart policy", func() {
		It("should return false", func() {
			neverRestart := corev1.ContainerRestartPolicyNever
			container := &corev1.Container{
				Name:          "test-init",
				RestartPolicy: &neverRestart,
			}
			Expect(isSidecarInitContainer(container)).To(BeFalse())
		})
	})
})

var _ = Describe("hasNonSidecarInitContainers", func() {
	Context("when pod has no init containers", func() {
		It("should return false", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{},
				},
			}
			Expect(hasNonSidecarInitContainers(pod)).To(BeFalse())
		})
	})

	Context("when pod has only non-sidecar init containers", func() {
		It("should return true", func() {
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-1",
						},
					},
				},
			}
			Expect(hasNonSidecarInitContainers(pod)).To(BeTrue())
		})
	})

	Context("when pod has only sidecar init containers", func() {
		It("should return false", func() {
			alwaysRestart := corev1.ContainerRestartPolicyAlways
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:          "sidecar-init",
							RestartPolicy: &alwaysRestart,
						},
					},
				},
			}
			Expect(hasNonSidecarInitContainers(pod)).To(BeFalse())
		})
	})

	Context("when pod has mixed init containers", func() {
		It("should return true", func() {
			alwaysRestart := corev1.ContainerRestartPolicyAlways
			pod := &corev1.Pod{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name: "init-1",
						},
						{
							Name:          "sidecar-init",
							RestartPolicy: &alwaysRestart,
						},
					},
				},
			}
			Expect(hasNonSidecarInitContainers(pod)).To(BeTrue())
		})
	})
})

var _ = Describe("getNonSidecarInitContainerStatuses", func() {
	Context("when no init containers exist", func() {
		It("should return empty slice", func() {
			statuses := getNonSidecarInitContainerStatuses(
				[]corev1.ContainerStatus{},
				[]corev1.Container{},
			)
			Expect(statuses).To(BeEmpty())
		})
	})

	Context("when only non-sidecar init containers exist", func() {
		It("should return all statuses", func() {
			initContainers := []corev1.Container{
				{Name: "init-1"},
				{Name: "init-2"},
			}
			statuses := []corev1.ContainerStatus{
				{Name: "init-1", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
				{Name: "init-2", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
			}

			result := getNonSidecarInitContainerStatuses(statuses, initContainers)
			Expect(result).To(HaveLen(2))
			Expect(result[0].Name).To(Equal("init-1"))
			Expect(result[1].Name).To(Equal("init-2"))
		})
	})

	Context("when only sidecar init containers exist", func() {
		It("should return empty slice", func() {
			alwaysRestart := corev1.ContainerRestartPolicyAlways
			initContainers := []corev1.Container{
				{
					Name:          "sidecar-init",
					RestartPolicy: &alwaysRestart,
				},
			}
			statuses := []corev1.ContainerStatus{
				{Name: "sidecar-init", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
			}

			result := getNonSidecarInitContainerStatuses(statuses, initContainers)
			Expect(result).To(BeEmpty())
		})
	})

	Context("when mixed init containers exist", func() {
		It("should return only non-sidecar statuses", func() {
			alwaysRestart := corev1.ContainerRestartPolicyAlways
			initContainers := []corev1.Container{
				{Name: "init-1"},
				{Name: "sidecar-init", RestartPolicy: &alwaysRestart},
				{Name: "init-2"},
			}
			statuses := []corev1.ContainerStatus{
				{Name: "init-1", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
				{Name: "sidecar-init", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
				{Name: "init-2", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}},
			}

			result := getNonSidecarInitContainerStatuses(statuses, initContainers)
			Expect(result).To(HaveLen(2))
			Expect(result[0].Name).To(Equal("init-1"))
			Expect(result[1].Name).To(Equal("init-2"))
		})
	})
})

var _ = Describe("ensureInitContainersAreCompleted - multiple pods", func() {
	var (
		mockCli k8client.Client
		cluster *apiv1.Cluster
		pod1    *corev1.Pod
		pod2    *corev1.Pod
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}
	})

	Context("when multiple pods exist with mixed init container states", func() {
		BeforeEach(func() {
			pod1 = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
						utils.PodRoleLabelName: string(utils.PodRoleInstance),
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "init-1"},
					},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "init-1",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 0,
									Reason:   "Completed",
								},
							},
						},
					},
				},
			}

			pod2 = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-2",
					Namespace: "default",
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
						utils.PodRoleLabelName: string(utils.PodRoleInstance),
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "init-2"},
					},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "init-2",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
						},
					},
				},
			}

			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, pod1, pod2).
				Build()
		})

		It("should requeue when at least one pod has running init containers", func(ctx SpecContext) {
			res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
			Expect(res.RequeueAfter).To(Equal(5 * time.Second))
		})
	})

	Context("when multiple pods exist and one has a failed init container", func() {
		BeforeEach(func() {
			pod1 = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
						utils.PodRoleLabelName: string(utils.PodRoleInstance),
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "init-1"},
					},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "init-1",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 0,
									Reason:   "Completed",
								},
							},
						},
					},
				},
			}

			pod2 = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-2",
					Namespace: "default",
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
						utils.PodRoleLabelName: string(utils.PodRoleInstance),
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "init-2"},
					},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "init-2",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 1,
									Reason:   "Error",
									Message:  "restore failed",
								},
							},
						},
					},
				},
			}

			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, pod1, pod2).
				Build()
		})

		It("should requeue with 10 second delay when a pod has failed init container", func(ctx SpecContext) {
			res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).ToNot(BeNil())
			Expect(res.RequeueAfter).To(Equal(10 * time.Second))
		})
	})

	Context("when multiple pods exist but only one matches the pod role label", func() {
		BeforeEach(func() {
			pod1 = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-1",
					Namespace: "default",
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
						utils.PodRoleLabelName: string(utils.PodRoleInstance),
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "init-1"},
					},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "init-1",
							State: corev1.ContainerState{
								Terminated: &corev1.ContainerStateTerminated{
									ExitCode: 0,
									Reason:   "Completed",
								},
							},
						},
					},
				},
			}

			pod2 = &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-2",
					Namespace: "default",
					Labels: map[string]string{
						utils.ClusterLabelName: cluster.Name,
						utils.PodRoleLabelName: "other-role",
					},
				},
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{Name: "init-2"},
					},
				},
				Status: corev1.PodStatus{
					InitContainerStatuses: []corev1.ContainerStatus{
						{
							Name: "init-2",
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{},
							},
						},
					},
				},
			}

			mockCli = fake.NewClientBuilder().
				WithScheme(k8scheme.BuildWithAllKnownScheme()).
				WithObjects(cluster, pod1, pod2).
				Build()
		})

		It("should ignore pods that don't match the pod role label", func(ctx SpecContext) {
			res, err := ensureInitContainersAreCompleted(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(BeNil())
		})
	})
})
