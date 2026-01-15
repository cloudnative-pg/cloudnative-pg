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

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("cluster_create unit tests", func() {
	var env *testingEnvironment
	BeforeEach(func() {
		env = buildTestEnvironment()
	})

	It("should NOT create EnableSuperuserAccess if it is disabled", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pooler := newFakePooler(env.client, cluster)
		poolerSecretName := pooler.Name
		cluster.Status.PoolerIntegrations = &apiv1.PoolerIntegrations{
			PgBouncerIntegration: apiv1.PgBouncerIntegrationStatus{
				Secrets: []string{poolerSecretName},
			},
		}

		By("creating prerequisites", func() {
			generateFakeCASecret(
				env.client,
				cluster.GetClientCASecretName(),
				namespace,
				"testdomain.com",
			)
		})

		By("executing reconcilePostgresSecrets", func() {
			err := env.clusterReconciler.reconcilePostgresSecrets(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the superUser secret has not been created", func() {
			superUser := corev1.Secret{}
			err := env.client.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetSuperuserSecretName(), Namespace: namespace},
				&superUser,
			)
			Expect(err).To(HaveOccurred())
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		})

		By("making sure that the appUserSecret has been created", func() {
			appUser := corev1.Secret{}
			err := env.client.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetApplicationSecretName(), Namespace: namespace},
				&appUser,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(appUser.StringData["username"]).To(Equal("app"))
			Expect(appUser.StringData["password"]).To(HaveLen(64))
			Expect(appUser.StringData["dbname"]).To(Equal("app"))
		})

		By("making sure that the pooler secrets has been created", func() {
			poolerSecret := corev1.Secret{}
			err := env.client.Get(
				ctx,
				types.NamespacedName{Name: poolerSecretName, Namespace: namespace},
				&poolerSecret,
			)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	It("should make sure that superUser secret is created if EnableSuperuserAccess is enabled", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		cluster.Spec.EnableSuperuserAccess = ptr.To(true)

		By("executing reconcilePostgresSecrets", func() {
			err := env.clusterReconciler.reconcilePostgresSecrets(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the superUser secret has been created correctly", func() {
			superUser := corev1.Secret{}
			err := env.client.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetSuperuserSecretName(), Namespace: namespace},
				&superUser,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(superUser.StringData["username"]).To(Equal("postgres"))
			Expect(superUser.StringData["password"]).To(HaveLen(64))
			Expect(superUser.StringData["dbname"]).To(Equal("*"))
		})
	})

	It("should make sure that reconcilePostgresServices works correctly", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)

		By("executing reconcilePostgresServices", func() {
			err := env.clusterReconciler.reconcilePostgresServices(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the services have been created", func() {
			expectResourceExists(env.client, cluster.GetServiceReadOnlyName(), namespace, &corev1.Service{})
			expectResourceExists(env.client, cluster.GetServiceReadWriteName(), namespace, &corev1.Service{})
			expectResourceExists(env.client, cluster.GetServiceReadName(), namespace, &corev1.Service{})
		})
	})

	It("should make sure that reconcilePostgresServices works correctly if create any service is enabled",
		func(ctx SpecContext) {
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace)
			configuration.Current.CreateAnyService = true

			By("executing reconcilePostgresServices", func() {
				err := env.clusterReconciler.reconcilePostgresServices(ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
			})

			By("making sure that the services have been created", func() {
				expectResourceExists(env.client, cluster.GetServiceAnyName(), namespace, &corev1.Service{})
				expectResourceExists(env.client, cluster.GetServiceReadOnlyName(), namespace, &corev1.Service{})
				expectResourceExists(env.client, cluster.GetServiceReadWriteName(), namespace, &corev1.Service{})
				expectResourceExists(env.client, cluster.GetServiceReadName(), namespace, &corev1.Service{})
			})
		})

	It("should make sure that reconcilePostgresServices can update the selectors on existing services",
		func(ctx SpecContext) {
			namespace := newFakeNamespace(env.client)
			cluster := newFakeCNPGCluster(env.client, namespace)
			configuration.Current.CreateAnyService = true

			createOutdatedService := func(svc *corev1.Service) {
				cluster.SetInheritedDataAndOwnership(&svc.ObjectMeta)
				svc.Spec.Selector = map[string]string{
					"outdated": "selector",
				}
				err := env.clusterReconciler.Create(ctx, svc)
				Expect(err).ToNot(HaveOccurred())
			}

			checkService := func(before *corev1.Service, expectedLabels map[string]string) {
				var afterChangesService corev1.Service
				err := env.clusterReconciler.Get(ctx, types.NamespacedName{
					Name:      before.Name,
					Namespace: before.Namespace,
				}, &afterChangesService)
				Expect(err).ToNot(HaveOccurred())

				Expect(afterChangesService.Spec.Selector).ToNot(Equal(before.Spec.Selector))
				Expect(afterChangesService.Spec.Selector).To(Equal(expectedLabels))
				Expect(afterChangesService.Labels).To(Equal(before.Labels))
				Expect(afterChangesService.Annotations).To(Equal(before.Annotations))
			}

			var readOnlyService, readWriteService, readService, anyService *corev1.Service
			By("creating the resources with outdated selectors", func() {
				By("creating any service", func() {
					svc := specs.CreateClusterAnyService(*cluster)
					createOutdatedService(svc)
					anyService = svc.DeepCopy()
				})

				By("creating read service", func() {
					svc := specs.CreateClusterReadService(*cluster)
					createOutdatedService(svc)
					readService = svc.DeepCopy()
				})

				By("creating read-write service", func() {
					svc := specs.CreateClusterReadWriteService(*cluster)
					createOutdatedService(svc)
					readWriteService = svc.DeepCopy()
				})
				By("creating read only service", func() {
					svc := specs.CreateClusterReadOnlyService(*cluster)
					createOutdatedService(svc)
					readOnlyService = svc.DeepCopy()
				})
			})

			By("executing reconcilePostgresServices", func() {
				err := env.clusterReconciler.reconcilePostgresServices(ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
			})

			By("checking any service", func() {
				checkService(anyService, map[string]string{
					"cnpg.io/podRole": "instance",
					"cnpg.io/cluster": cluster.Name,
				})
			})

			By("checking read-write service", func() {
				checkService(readWriteService, map[string]string{
					"cnpg.io/cluster":                  cluster.Name,
					utils.ClusterInstanceRoleLabelName: "primary",
				})
			})

			By("checking read service", func() {
				checkService(readService, map[string]string{
					"cnpg.io/cluster": cluster.Name,
					"cnpg.io/podRole": "instance",
				})
			})

			By("checking read only service", func() {
				checkService(readOnlyService, map[string]string{
					"cnpg.io/cluster":                  cluster.Name,
					utils.ClusterInstanceRoleLabelName: "replica",
				})
			})
		})

	It("should make sure that createOrPatchServiceAccount works correctly", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)

		By("executing createOrPatchServiceAccount (create)", func() {
			err := env.clusterReconciler.createOrPatchServiceAccount(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		sa := &corev1.ServiceAccount{}

		By("making sure that the serviceaccount has been created", func() {
			expectResourceExists(env.client, cluster.Name, namespace, sa)
		})

		By("adding an annotation, a label and an image pull secret to the service account", func() {
			sa.Annotations["test"] = "annotation"
			sa.Labels["test"] = "label"
			sa.ImagePullSecrets = append(sa.ImagePullSecrets, corev1.LocalObjectReference{
				Name: "sa-pullsecret",
			})
			err := env.client.Update(context.Background(), sa)
			Expect(err).ToNot(HaveOccurred())
		})

		By("executing createOrPatchServiceAccount (no-patch)", func() {
			err := env.clusterReconciler.createOrPatchServiceAccount(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("making sure that the serviceaccount is untouched because there is no change in the cluster", func() {
			updatedSa := &corev1.ServiceAccount{}
			expectResourceExists(env.client, cluster.Name, namespace, updatedSa)
			Expect(updatedSa).To(BeEquivalentTo(sa))
		})

		By("adding an image pull secret to the cluster to trigger a service account update", func() {
			cluster.Spec.ImagePullSecrets = append(cluster.Spec.ImagePullSecrets, apiv1.LocalObjectReference{
				Name: "cluster-pullsecret",
			})
			err := env.client.Update(context.Background(), cluster)
			Expect(err).ToNot(HaveOccurred())
		})

		By("executing createOrPatchServiceAccount (patch)", func() {
			By("setting owner reference to nil", func() {
				sa.OwnerReferences = nil
				err := env.client.Update(context.Background(), sa)
				Expect(err).ToNot(HaveOccurred())
			})

			By("running patch", func() {
				err := env.clusterReconciler.createOrPatchServiceAccount(ctx, cluster)
				Expect(err).ToNot(HaveOccurred())
			})

			By("making sure that the serviceaccount is patched correctly", func() {
				updatedSA := &corev1.ServiceAccount{}
				expectResourceExists(env.client, cluster.Name, namespace, updatedSA)
				Expect(updatedSA.Annotations["test"]).To(BeEquivalentTo("annotation"))
				Expect(updatedSA.Labels["test"]).To(BeEquivalentTo("label"))
				Expect(updatedSA.ImagePullSecrets).To(ContainElements(corev1.LocalObjectReference{
					Name: "cluster-pullsecret",
				}))
				Expect(updatedSA.ImagePullSecrets).To(ContainElements(corev1.LocalObjectReference{
					Name: "sa-pullsecret",
				}))
				Expect(updatedSA.OwnerReferences).To(BeNil())
			})
		})
	})

	It("should make sure that reconcilePodDisruptionBudget works correctly", func(ctx SpecContext) {
		namespace := newFakeNamespace(env.client)
		cluster := newFakeCNPGCluster(env.client, namespace)
		pdbReplicaName := specs.BuildReplicasPodDisruptionBudget(cluster).Name
		pdbPrimaryName := specs.BuildPrimaryPodDisruptionBudget(cluster).Name
		reconcilePDB := func() {
			err := env.clusterReconciler.reconcilePodDisruptionBudget(ctx, cluster)
			Expect(err).ToNot(HaveOccurred())
		}

		By("creating the primary and replica PDB in a standard case scenario", func() {
			reconcilePDB()
		})

		By("making sure PDB exists", func() {
			expectResourceExists(
				env.client,
				pdbPrimaryName,
				namespace,
				&policyv1.PodDisruptionBudget{},
			)
			expectResourceExists(
				env.client,
				pdbReplicaName,
				namespace,
				&policyv1.PodDisruptionBudget{},
			)
		})

		By("scaling the instances to 2", func() {
			cluster.Spec.Instances = 2
			cluster.Status.Instances = 2
		})

		By("reconciling pdb with two nodes", func() {
			reconcilePDB()
		})

		By("making sure that only the replicas PDB has been deleted", func() {
			expectResourceExists(
				env.client,
				pdbPrimaryName,
				namespace,
				&policyv1.PodDisruptionBudget{},
			)
			expectResourceDoesntExist(
				env.client,
				pdbReplicaName,
				namespace,
				&policyv1.PodDisruptionBudget{},
			)
		})

		By("enabling the cluster maintenance mode", func() {
			reusePVC := true
			cluster.Spec.NodeMaintenanceWindow = &apiv1.NodeMaintenanceWindow{
				InProgress: true,
				ReusePVC:   &reusePVC,
			}
		})

		By("reconciling pdb during the maintenance mode", func() {
			reconcilePDB()
		})

		By("making sure that only the replicas PDB has been deleted", func() {
			expectResourceExists(
				env.client,
				pdbPrimaryName,
				namespace,
				&policyv1.PodDisruptionBudget{},
			)
			expectResourceDoesntExist(
				env.client,
				pdbReplicaName,
				namespace,
				&policyv1.PodDisruptionBudget{},
			)
		})

		By("scaling the instances to 1 during maintenance mode", func() {
			cluster.Spec.Instances = 1
			cluster.Status.Instances = 1
		})

		By("reconciling pdb during the maintenance mode with a single node", func() {
			reconcilePDB()
		})

		By("making sure that both the replicas and main PDB are deleted", func() {
			expectResourceDoesntExist(
				env.client,
				pdbPrimaryName,
				namespace,
				&policyv1.PodDisruptionBudget{},
			)
			expectResourceDoesntExist(
				env.client,
				pdbReplicaName,
				namespace,
				&policyv1.PodDisruptionBudget{},
			)
		})
	})
})

var _ = Describe("check if bootstrap recovery can proceed", func() {
	var env *testingEnvironment
	var namespace, clusterName, name string

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
		clusterName = "awesomeCluster"
		name = "foo"
	})

	_ = DescribeTable("from backup",
		func(backup *apiv1.Backup, expectRequeue bool) {
			cluster := &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterName,
					Namespace: namespace,
				},
				Spec: apiv1.ClusterSpec{
					StorageConfiguration: apiv1.StorageConfiguration{
						Size: "1G",
					},
					Bootstrap: &apiv1.BootstrapConfiguration{
						Recovery: &apiv1.BootstrapRecovery{
							Backup: &apiv1.BackupSource{
								LocalObjectReference: apiv1.LocalObjectReference{
									Name: name,
								},
							},
						},
					},
				},
			}

			ctx := context.Background()
			res, err := env.clusterReconciler.checkReadyForRecovery(ctx, backup, cluster)
			Expect(err).ToNot(HaveOccurred())
			if expectRequeue {
				Expect(res).ToNot(BeNil())
				Expect(res).ToNot(Equal(reconcile.Result{}))
			} else {
				Expect(res).To(Or(BeNil(), Equal(reconcile.Result{})))
			}
		},
		Entry(
			"when bootstrapping from a completed backup",
			&apiv1.Backup{
				Status: apiv1.BackupStatus{
					Phase: apiv1.BackupPhaseCompleted,
				},
			},
			false),
		Entry(
			"when bootstrapping from an incomplete backup",
			&apiv1.Backup{
				Status: apiv1.BackupStatus{
					Phase: apiv1.BackupPhaseRunning,
				},
			},
			true),
		Entry("when bootstrapping a backup that is not there",
			nil, true),
	)
})

var _ = Describe("check if bootstrap recovery can proceed from volume snapshot", func() {
	var env *testingEnvironment
	var namespace, clusterName string
	var cluster *apiv1.Cluster

	BeforeEach(func() {
		env = buildTestEnvironment()
		namespace = newFakeNamespace(env.client)
		clusterName = "awesomeCluster"
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: namespace,
			},
			Spec: apiv1.ClusterSpec{
				StorageConfiguration: apiv1.StorageConfiguration{
					Size: "1G",
				},
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						VolumeSnapshots: &apiv1.DataSource{
							Storage: corev1.TypedLocalObjectReference{
								APIGroup: ptr.To(volumesnapshotv1.GroupName),
								Kind:     apiv1.VolumeSnapshotKind,
								Name:     "pgdata",
							},
						},
					},
				},
			},
		}
	})

	It("should not requeue if bootstrapping from a valid volume snapshot", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pgdata",
						Namespace: namespace,
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-one",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgData),
						},
					},
				},
			},
		}

		mockClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithLists(&snapshots).
			Build()

		newClusterReconciler := &ClusterReconciler{
			Client:          mockClient,
			Scheme:          env.scheme,
			Recorder:        record.NewFakeRecorder(120),
			DiscoveryClient: env.discoveryClient,
		}

		res, err := newClusterReconciler.checkReadyForRecovery(ctx, nil, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(Or(BeNil(), Equal(reconcile.Result{})))
	})

	// nolint: dupl
	It("should requeue if bootstrapping from an invalid volume snapshot", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pgdata",
						Namespace: namespace,
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-one",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgTablespace),
						},
					},
				},
			},
		}

		mockClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithLists(&snapshots).
			Build()

		newClusterReconciler := &ClusterReconciler{
			Client:          mockClient,
			Scheme:          env.scheme,
			Recorder:        record.NewFakeRecorder(120),
			DiscoveryClient: env.discoveryClient,
		}

		res, err := newClusterReconciler.checkReadyForRecovery(ctx, nil, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(res).ToNot(Equal(reconcile.Result{}))
	})

	// nolint: dupl
	It("should requeue if bootstrapping from a snapshot that isn't there", func(ctx SpecContext) {
		snapshots := volumesnapshotv1.VolumeSnapshotList{
			Items: []volumesnapshotv1.VolumeSnapshot{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "foobar",
						Namespace: namespace,
						Labels: map[string]string{
							utils.BackupNameLabelName: "backup-one",
						},
						Annotations: map[string]string{
							utils.PvcRoleLabelName: string(utils.PVCRolePgData),
						},
					},
				},
			},
		}

		mockClient := fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithLists(&snapshots).
			Build()

		newClusterReconciler := &ClusterReconciler{
			Client:          mockClient,
			Scheme:          env.scheme,
			Recorder:        record.NewFakeRecorder(120),
			DiscoveryClient: env.discoveryClient,
		}

		res, err := newClusterReconciler.checkReadyForRecovery(ctx, nil, cluster)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).ToNot(BeNil())
		Expect(res).ToNot(Equal(reconcile.Result{}))
	})
})

var _ = Describe("Set cluster metadata of service account", func() {
	It("must be idempotent, if metadata are not defined", func() {
		sa := &corev1.ServiceAccount{}

		cluster := &apiv1.Cluster{}

		cluster.Spec.ServiceAccountTemplate.MergeMetadata(sa)
		Expect(sa.Annotations).To(BeEmpty())
		Expect(sa.Labels).To(BeEmpty())
	})

	It("must set metadata, if they are defined", func() {
		sa := &corev1.ServiceAccount{}

		annotations := map[string]string{
			"testProvider": "testAnnotation",
		}
		labels := map[string]string{
			"testProvider": "testLabel",
		}
		cluster := &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				ServiceAccountTemplate: &apiv1.ServiceAccountTemplate{
					Metadata: apiv1.Metadata{
						Labels:      labels,
						Annotations: annotations,
					},
				},
			},
		}

		cluster.Spec.ServiceAccountTemplate.MergeMetadata(sa)
		Expect(sa.Annotations).To(BeEquivalentTo(cluster.Spec.ServiceAccountTemplate.Metadata.Annotations))
		Expect(sa.Labels).To(BeEquivalentTo(cluster.Spec.ServiceAccountTemplate.Metadata.Labels))
	})
})

type mockPodMonitorManager struct {
	isEnabled  bool
	podMonitor *monitoringv1.PodMonitor
}

func (m *mockPodMonitorManager) IsPodMonitorEnabled() bool {
	return m.isEnabled
}

func (m *mockPodMonitorManager) BuildPodMonitor() *monitoringv1.PodMonitor {
	return m.podMonitor
}

var _ = Describe("CreateOrPatchPodMonitor", func() {
	var (
		ctx                 context.Context
		fakeCli             k8client.Client
		fakeDiscoveryClient discovery.DiscoveryInterface
		manager             *mockPodMonitorManager
	)

	BeforeEach(func() {
		ctx = context.Background()
		manager = &mockPodMonitorManager{}
		manager.isEnabled = true
		manager.podMonitor = &monitoringv1.PodMonitor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}

		fakeCli = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).Build()

		fakeDiscoveryClient = &fakediscovery.FakeDiscovery{
			Fake: &testing.Fake{
				Resources: []*metav1.APIResourceList{
					{
						GroupVersion: "monitoring.coreos.com/v1",
						APIResources: []metav1.APIResource{
							{
								Name:       "podmonitors",
								Kind:       "PodMonitor",
								Namespaced: true,
							},
						},
					},
				},
			},
		}
	})

	It("should create the PodMonitor  when it is enabled and doesn't already exists", func() {
		err := createOrPatchPodMonitor(ctx, fakeCli, fakeDiscoveryClient, manager)
		Expect(err).ToNot(HaveOccurred())

		podMonitor := &monitoringv1.PodMonitor{}
		err = fakeCli.Get(
			ctx,
			types.NamespacedName{
				Name:      manager.podMonitor.Name,
				Namespace: manager.podMonitor.Namespace,
			},
			podMonitor,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(podMonitor.Name).To(Equal(manager.podMonitor.Name))
		Expect(podMonitor.Namespace).To(Equal(manager.podMonitor.Namespace))
	})

	It("should not return an error when PodMonitor is disabled", func() {
		manager.isEnabled = false
		err := createOrPatchPodMonitor(ctx, fakeCli, fakeDiscoveryClient, manager)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should remove the PodMonitor if it is disabled and is owned by a cluster", func() {
		cluster := apiv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				Kind:       apiv1.ClusterKind,
				APIVersion: apiSGVString,
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		}
		cluster.SetInheritedDataAndOwnership(&manager.podMonitor.ObjectMeta)

		// Create the PodMonitor with the fake client
		err := fakeCli.Create(ctx, manager.podMonitor)
		Expect(err).ToNot(HaveOccurred())

		manager.isEnabled = false
		err = createOrPatchPodMonitor(ctx, fakeCli, fakeDiscoveryClient, manager)
		Expect(err).ToNot(HaveOccurred())

		// Ensure the PodMonitor doesn't exist anymore
		podMonitor := &monitoringv1.PodMonitor{}
		err = fakeCli.Get(
			ctx,
			types.NamespacedName{
				Name:      manager.podMonitor.Name,
				Namespace: manager.podMonitor.Namespace,
			},
			podMonitor,
		)
		Expect(err).To(HaveOccurred())
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})

	It("should NOT remove the PodMonitor if it is not owned by a cluster", func() {
		unownedPodMonitor := &monitoringv1.PodMonitor{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
		}
		err := fakeCli.Create(ctx, unownedPodMonitor)
		Expect(err).ToNot(HaveOccurred())

		manager.isEnabled = false
		err = createOrPatchPodMonitor(ctx, fakeCli, fakeDiscoveryClient, manager)
		Expect(err).ToNot(HaveOccurred())

		podMonitor := &monitoringv1.PodMonitor{}
		err = fakeCli.Get(
			ctx,
			types.NamespacedName{
				Name:      manager.podMonitor.Name,
				Namespace: manager.podMonitor.Namespace,
			},
			podMonitor,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(podMonitor.Name).To(Equal("test"))
	})

	It("should patch the PodMonitor with updated labels and annotations", func() {
		initialLabels := map[string]string{"label1": "value1"}
		initialAnnotations := map[string]string{"annotation1": "value1"}

		manager.podMonitor.Labels = initialLabels
		manager.podMonitor.Annotations = initialAnnotations
		err := fakeCli.Create(ctx, manager.podMonitor)
		Expect(err).ToNot(HaveOccurred())

		updatedLabels := map[string]string{"label1": "changedValue1", "label2": "value2"}
		updatedAnnotations := map[string]string{"annotation1": "changedValue1", "annotation2": "value2"}

		manager.podMonitor.Labels = updatedLabels
		manager.podMonitor.Annotations = updatedAnnotations

		err = createOrPatchPodMonitor(ctx, fakeCli, fakeDiscoveryClient, manager)
		Expect(err).ToNot(HaveOccurred())

		podMonitor := &monitoringv1.PodMonitor{}
		err = fakeCli.Get(
			ctx,
			types.NamespacedName{
				Name:      manager.podMonitor.Name,
				Namespace: manager.podMonitor.Namespace,
			},
			podMonitor,
		)
		Expect(err).ToNot(HaveOccurred())

		Expect(podMonitor.Labels).To(Equal(updatedLabels))
		Expect(podMonitor.Annotations).To(Equal(updatedAnnotations))
	})
})

var _ = Describe("createOrPatchClusterCredentialSecret", func() {
	const (
		secretName = "test-secret"
		namespace  = "test-namespace"
	)
	var (
		proposed *corev1.Secret
		cli      k8client.Client
	)

	BeforeEach(func() {
		cli = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).Build()
		const secretName = "test-secret"
		proposed = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        secretName,
				Namespace:   namespace,
				Labels:      map[string]string{"test": "label"},
				Annotations: map[string]string{"test": "annotation"},
			},
			Data: map[string][]byte{"key": []byte("value")},
		}
	})

	Context("when the secret does not exist", func() {
		It("should create the secret", func(ctx SpecContext) {
			err := createOrPatchClusterCredentialSecret(ctx, cli, proposed)
			Expect(err).NotTo(HaveOccurred())

			var createdSecret corev1.Secret
			err = cli.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &createdSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(createdSecret.Data).To(Equal(proposed.Data))
			Expect(createdSecret.Labels).To(Equal(proposed.Labels))
			Expect(createdSecret.Annotations).To(Equal(proposed.Annotations))
		})
	})

	Context("when the secret exists and is owned by the cluster", func() {
		BeforeEach(func(ctx SpecContext) {
			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:        secretName,
					Namespace:   namespace,
					Labels:      map[string]string{"old": "label"},
					Annotations: map[string]string{"old": "annotation"},
				},
				Data: map[string][]byte{"oldkey": []byte("oldvalue")},
			}
			cluster := apiv1.Cluster{
				TypeMeta: metav1.TypeMeta{
					Kind:       apiv1.ClusterKind,
					APIVersion: apiSGVString,
				},
				ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: namespace},
			}
			cluster.SetInheritedDataAndOwnership(&existingSecret.ObjectMeta)
			Expect(cli.Create(ctx, existingSecret)).To(Succeed())
		})

		It("should patch the secret if metadata differs", func(ctx SpecContext) {
			Expect(proposed.Labels).To(HaveKeyWithValue("test", "label"))
			Expect(proposed.Annotations).To(HaveKeyWithValue("test", "annotation"))

			err := createOrPatchClusterCredentialSecret(ctx, cli, proposed)
			Expect(err).NotTo(HaveOccurred())

			var patchedSecret corev1.Secret
			err = cli.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &patchedSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(patchedSecret.Labels).To(HaveKeyWithValue("test", "label"))
			Expect(patchedSecret.Annotations).To(HaveKeyWithValue("test", "annotation"))
		})

		It("should not patch the secret if metadata is the same", func(ctx SpecContext) {
			var originalSecret corev1.Secret
			err := cli.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &originalSecret)
			Expect(err).NotTo(HaveOccurred())

			// Assuming secretName is the name of the existing secret
			proposed.Name = secretName
			proposed.Labels = map[string]string{"old": "label"}
			proposed.Annotations = map[string]string{"old": "annotation"}

			err = createOrPatchClusterCredentialSecret(ctx, cli, proposed)
			Expect(err).NotTo(HaveOccurred())

			var patchedSecret corev1.Secret
			err = cli.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &patchedSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(originalSecret.Generation).To(Equal(originalSecret.Generation))
		})
	})

	Context("when the secret exists but is not owned by the cluster", func() {
		BeforeEach(func(ctx SpecContext) {
			existingSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      secretName,
					Namespace: namespace,
				},
			}
			Expect(cli.Create(ctx, existingSecret)).To(Succeed())
		})

		It("should not modify the secret", func(ctx SpecContext) {
			var originalSecret corev1.Secret
			err := cli.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &originalSecret)
			Expect(err).NotTo(HaveOccurred())

			err = createOrPatchClusterCredentialSecret(ctx, cli, proposed)
			Expect(err).NotTo(HaveOccurred())

			var patchedSecret corev1.Secret
			err = cli.Get(ctx, types.NamespacedName{Name: secretName, Namespace: namespace}, &patchedSecret)
			Expect(err).NotTo(HaveOccurred())
			Expect(originalSecret.Generation).To(Equal(originalSecret.Generation))
		})
	})
})

var _ = Describe("createOrPatchOwnedPodDisruptionBudget", func() {
	var (
		ctx        context.Context
		fakeClient k8client.Client
		reconciler *ClusterReconciler
		cluster    *apiv1.Cluster
		pdb        *policyv1.PodDisruptionBudget
		err        error
	)

	BeforeEach(func() {
		ctx = context.Background()

		fakeClient = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).Build()
		reconciler = &ClusterReconciler{
			Client:   fakeClient,
			Recorder: record.NewFakeRecorder(10000),
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
		}

		cluster = &apiv1.Cluster{}
		pdb = &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pdb",
				Namespace: "default",
				Labels: map[string]string{
					"test": "value",
				},
			},
			Spec: policyv1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "example"},
				},
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 1,
				},
			},
		}
	})

	Context("when PodDisruptionBudget is nil", func() {
		It("should return nil without error", func() {
			pdb = nil
			err = reconciler.createOrPatchOwnedPodDisruptionBudget(ctx, cluster, pdb)
			Expect(err).ShouldNot(HaveOccurred())
		})
	})

	Context("when creating a new PodDisruptionBudget", func() {
		It("should successfully create the PodDisruptionBudget", func() {
			err = reconciler.createOrPatchOwnedPodDisruptionBudget(ctx, cluster, pdb)
			Expect(err).ShouldNot(HaveOccurred())

			fetchedPdb := &policyv1.PodDisruptionBudget{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, fetchedPdb)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(fetchedPdb.Name).To(Equal(pdb.Name))
		})
	})

	Context("when the PodDisruptionBudget already exists", func() {
		BeforeEach(func() {
			_ = fakeClient.Create(ctx, pdb)
		})

		It("should update the existing PodDisruptionBudget if the metadata is different", func() {
			pdb.Labels["newlabel"] = "newvalue"
			err = reconciler.createOrPatchOwnedPodDisruptionBudget(ctx, cluster, pdb)
			Expect(err).ShouldNot(HaveOccurred())

			fetchedPdb := &policyv1.PodDisruptionBudget{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, fetchedPdb)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(fetchedPdb.Spec).To(Equal(pdb.Spec))
			Expect(fetchedPdb.Labels).To(Equal(pdb.Labels))
		})

		It("should update the existing PodDisruptionBudget if the spec is different", func() {
			pdb.Spec.MinAvailable = ptr.To(intstr.FromInt32(3))
			err = reconciler.createOrPatchOwnedPodDisruptionBudget(ctx, cluster, pdb)
			Expect(err).ShouldNot(HaveOccurred())

			fetchedPdb := &policyv1.PodDisruptionBudget{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, fetchedPdb)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(fetchedPdb.Spec).To(Equal(pdb.Spec))
			Expect(fetchedPdb.Labels).To(Equal(pdb.Labels))
		})

		It("should not update the PodDisruptionBudget if it is the same", func() {
			err = reconciler.createOrPatchOwnedPodDisruptionBudget(ctx, cluster, pdb)
			Expect(err).ShouldNot(HaveOccurred())

			fetchedPdb := &policyv1.PodDisruptionBudget{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: pdb.Name, Namespace: pdb.Namespace}, fetchedPdb)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(fetchedPdb.Spec).To(Equal(pdb.Spec))
			Expect(fetchedPdb.Labels).To(Equal(pdb.Labels))
		})
	})
})

var _ = Describe("deletePodDisruptionBudgetsIfExist", func() {
	const namespace = "default"

	var (
		fakeClient k8client.Client
		reconciler *ClusterReconciler
		cluster    *apiv1.Cluster
		pdbPrimary *policyv1.PodDisruptionBudget
		pdb        *policyv1.PodDisruptionBudget
	)

	BeforeEach(func() {
		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: namespace,
			},
		}
		pdbPrimary = &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name + apiv1.PrimaryPodDisruptionBudgetSuffix,
				Namespace: namespace,
				Labels: map[string]string{
					"test": "value",
				},
			},
			Spec: policyv1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "example"},
				},
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 1,
				},
			},
		}
		pdb = &policyv1.PodDisruptionBudget{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name,
				Namespace: namespace,
				Labels: map[string]string{
					"test": "value",
				},
			},
			Spec: policyv1.PodDisruptionBudgetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "example"},
				},
				MinAvailable: &intstr.IntOrString{
					Type:   intstr.Int,
					IntVal: 1,
				},
			},
		}

		fakeClient = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster, pdbPrimary, pdb).
			Build()

		reconciler = &ClusterReconciler{
			Client:   fakeClient,
			Recorder: record.NewFakeRecorder(10000),
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
		}
	})

	It("should delete the existing PDBs", func(ctx SpecContext) {
		err := fakeClient.Get(ctx, k8client.ObjectKeyFromObject(pdbPrimary), &policyv1.PodDisruptionBudget{})
		Expect(err).ToNot(HaveOccurred())

		err = fakeClient.Get(ctx, k8client.ObjectKeyFromObject(pdbPrimary), &policyv1.PodDisruptionBudget{})
		Expect(err).ToNot(HaveOccurred())

		err = reconciler.deletePodDisruptionBudgetsIfExist(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())

		err = fakeClient.Get(ctx, k8client.ObjectKeyFromObject(pdbPrimary), &policyv1.PodDisruptionBudget{})
		Expect(apierrs.IsNotFound(err)).To(BeTrue())

		err = fakeClient.Get(ctx, k8client.ObjectKeyFromObject(pdb), &policyv1.PodDisruptionBudget{})
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})

	It("should be able to delete the PDB when the primary PDB is missing", func(ctx SpecContext) {
		fakeClient = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			WithObjects(cluster, pdb).
			Build()
		reconciler = &ClusterReconciler{
			Client:   fakeClient,
			Recorder: record.NewFakeRecorder(10000),
			Scheme:   schemeBuilder.BuildWithAllKnownScheme(),
		}

		err := fakeClient.Get(ctx, k8client.ObjectKeyFromObject(pdbPrimary), &policyv1.PodDisruptionBudget{})
		Expect(apierrs.IsNotFound(err)).To(BeTrue())

		err = reconciler.deletePodDisruptionBudgetsIfExist(ctx, cluster)
		Expect(err).ToNot(HaveOccurred())

		err = fakeClient.Get(ctx, k8client.ObjectKeyFromObject(pdb), &policyv1.PodDisruptionBudget{})
		Expect(apierrs.IsNotFound(err)).To(BeTrue())
	})
})

var _ = Describe("Service Reconciling", func() {
	var (
		ctx           context.Context
		cluster       apiv1.Cluster
		reconciler    *ClusterReconciler
		serviceClient k8client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		cluster = apiv1.Cluster{
			TypeMeta: metav1.TypeMeta{
				Kind:       apiv1.ClusterKind,
				APIVersion: apiv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				Managed: &apiv1.ManagedConfiguration{
					Services: &apiv1.ManagedServices{
						Additional: []apiv1.ManagedService{},
					},
				},
			},
		}

		serviceClient = fake.NewClientBuilder().
			WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
			Build()
		reconciler = &ClusterReconciler{
			Client: serviceClient,
		}
	})

	Describe("serviceReconciler", func() {
		var proposedService *corev1.Service

		BeforeEach(func() {
			proposedService = &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{"app": "test"},
					Ports:    []corev1.ServicePort{{Port: 80}},
				},
			}
			cluster.SetInheritedDataAndOwnership(&proposedService.ObjectMeta)
		})

		Context("when service does not exist", func() {
			It("should create a new service if enabled", func() {
				err := reconciler.serviceReconciler(ctx, &cluster, proposedService, true)
				Expect(err).NotTo(HaveOccurred())

				var createdService corev1.Service
				err = serviceClient.Get(ctx, types.NamespacedName{
					Name:      proposedService.Name,
					Namespace: proposedService.Namespace,
				}, &createdService)
				Expect(err).NotTo(HaveOccurred())
				Expect(createdService.Spec.Selector).To(Equal(proposedService.Spec.Selector))
			})

			It("should not create a new service if not enabled", func() {
				err := reconciler.serviceReconciler(ctx, &cluster, proposedService, false)
				Expect(err).NotTo(HaveOccurred())

				var createdService corev1.Service
				err = serviceClient.Get(
					ctx,
					types.NamespacedName{Name: proposedService.Name, Namespace: proposedService.Namespace},
					&createdService,
				)
				Expect(apierrs.IsNotFound(err)).To(BeTrue())
			})
		})

		Context("when service exists", func() {
			BeforeEach(func() {
				err := serviceClient.Create(ctx, proposedService)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should delete the service if not enabled", func() {
				err := reconciler.serviceReconciler(ctx, &cluster, proposedService, false)
				Expect(err).NotTo(HaveOccurred())

				var deletedService corev1.Service
				err = serviceClient.Get(ctx, types.NamespacedName{
					Name:      proposedService.Name,
					Namespace: proposedService.Namespace,
				}, &deletedService)
				Expect(apierrs.IsNotFound(err)).To(BeTrue())
			})

			It("should update the service if necessary", func() {
				existingService := proposedService.DeepCopy()
				existingService.Spec.Selector = map[string]string{"app": "old"}
				err := serviceClient.Update(ctx, existingService)
				Expect(err).NotTo(HaveOccurred())

				err = reconciler.serviceReconciler(ctx, &cluster, proposedService, true)
				Expect(err).NotTo(HaveOccurred())

				var updatedService corev1.Service
				err = serviceClient.Get(ctx, types.NamespacedName{
					Name:      proposedService.Name,
					Namespace: proposedService.Namespace,
				}, &updatedService)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedService.Spec.Selector).To(Equal(proposedService.Spec.Selector))
			})

			It("should preserve existing labels and annotations added by third parties", func() {
				existingService := proposedService.DeepCopy()
				existingService.Labels = map[string]string{"custom-label": "value"}
				existingService.Annotations = map[string]string{"custom-annotation": "value"}
				err := serviceClient.Update(ctx, existingService)
				Expect(err).NotTo(HaveOccurred())

				proposedService.Labels = map[string]string{"app": "test"}
				proposedService.Annotations = map[string]string{"annotation": "test"}

				err = reconciler.serviceReconciler(ctx, &cluster, proposedService, true)
				Expect(err).NotTo(HaveOccurred())

				var updatedService corev1.Service
				err = serviceClient.Get(ctx, types.NamespacedName{
					Name:      proposedService.Name,
					Namespace: proposedService.Namespace,
				}, &updatedService)
				Expect(err).NotTo(HaveOccurred())
				Expect(updatedService.Labels).To(HaveKeyWithValue("custom-label", "value"))
				Expect(updatedService.Annotations).To(HaveKeyWithValue("custom-annotation", "value"))
			})
		})
	})

	Describe("reconcilePostgresServices", func() {
		It("should create the default services", func() {
			err := reconciler.reconcilePostgresServices(ctx, &cluster)
			Expect(err).NotTo(HaveOccurred())
			err = reconciler.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetServiceReadWriteName(), Namespace: cluster.Namespace},
				&corev1.Service{},
			)
			Expect(err).ToNot(HaveOccurred())
			err = reconciler.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetServiceReadName(), Namespace: cluster.Namespace},
				&corev1.Service{},
			)
			Expect(err).ToNot(HaveOccurred())
			err = reconciler.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetServiceReadOnlyName(), Namespace: cluster.Namespace},
				&corev1.Service{},
			)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should not create the default services", func() {
			cluster.Spec.Managed.Services.DisabledDefaultServices = []apiv1.ServiceSelectorType{
				apiv1.ServiceSelectorTypeRW,
				apiv1.ServiceSelectorTypeRO,
				apiv1.ServiceSelectorTypeR,
			}
			err := reconciler.reconcilePostgresServices(ctx, &cluster)
			Expect(err).NotTo(HaveOccurred())
			err = reconciler.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetServiceReadWriteName(), Namespace: cluster.Namespace},
				&corev1.Service{},
			)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
			err = reconciler.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetServiceReadName(), Namespace: cluster.Namespace},
				&corev1.Service{},
			)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
			err = reconciler.Get(
				ctx,
				types.NamespacedName{Name: cluster.GetServiceReadOnlyName(), Namespace: cluster.Namespace},
				&corev1.Service{},
			)
			Expect(apierrs.IsNotFound(err)).To(BeTrue())
		})
	})
})
