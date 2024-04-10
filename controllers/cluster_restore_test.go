/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
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
		ctx     context.Context
		mockCli k8client.Client
		cluster *apiv1.Cluster
	)

	getCluster := func(clusterKey k8client.ObjectKey) (*apiv1.Cluster, error) {
		remoteCluster := &apiv1.Cluster{}
		err := mockCli.Get(ctx, clusterKey, remoteCluster)
		return remoteCluster, err
	}

	BeforeEach(func() {
		ctx = context.TODO()
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
		It("should not modify the object", func() {
			origCluster, err := getCluster(k8client.ObjectKeyFromObject(cluster))
			Expect(err).ToNot(HaveOccurred())

			err = ensureClusterIsNotFenced(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())

			remoteCluster, err := getCluster(k8client.ObjectKeyFromObject(cluster))
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

		It("should patch the cluster and remove fenced instances", func() {
			origCluster, err := getCluster(k8client.ObjectKeyFromObject(cluster))
			Expect(err).ToNot(HaveOccurred())
			Expect(origCluster.Annotations).To(HaveKey(utils.FencedInstanceAnnotation))

			err = ensureClusterIsNotFenced(ctx, mockCli, cluster)
			Expect(err).ToNot(HaveOccurred())

			remoteCluster, err := getCluster(k8client.ObjectKeyFromObject(cluster))
			Expect(err).ToNot(HaveOccurred())

			Expect(remoteCluster.ObjectMeta).ToNot(Equal(origCluster.ObjectMeta))
			Expect(remoteCluster.Annotations).ToNot(HaveKey(utils.FencedInstanceAnnotation))
		})
	})
})

var _ = Describe("restoreClusterStatus", func() {
	var (
		ctx     context.Context
		mockCli k8client.Client
		cluster *apiv1.Cluster
	)

	BeforeEach(func() {
		ctx = context.TODO()
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
		It("should patch the cluster with the updated status", func() {
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
		ctx      context.Context
		mockCli  k8client.Client
		cluster  *apiv1.Cluster
		goodPvcs []corev1.PersistentVolumeClaim
		badPvcs  []corev1.PersistentVolumeClaim
	)

	BeforeEach(func() {
		ctx = context.TODO()
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

	It("should fetch only the pvcs that belong to the cluster and without an owner", func() {
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

	It("should correctly restore the orphan pvcs", func() {
		err := restoreOrphanPVCs(ctx, mockCli, cluster, goodPvcs)
		Expect(err).ToNot(HaveOccurred())

		for _, pvc := range goodPvcs {
			Expect(pvc.OwnerReferences).ToNot(BeEmpty())
			Expect(pvc.Annotations[utils.PVCStatusAnnotationName]).To(Equal(persistentvolumeclaim.StatusReady))
		}
	})
})
