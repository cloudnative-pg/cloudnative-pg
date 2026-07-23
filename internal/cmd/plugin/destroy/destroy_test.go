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

package destroy

import (
	"context"
	"io"
	"os"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/controller"
	"github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	clusterName  = "cluster-example"
	instanceName = "cluster-example-1"
	namespace    = "test-ns"
)

func redirectStdoutToGinkgoWriter() {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	DeferCleanup(func() {
		_ = w.Close()
		os.Stdout = old
		_, _ = io.Copy(GinkgoWriter, r)
	})
}

func newOwningCluster() *apiv1.Cluster {
	return &apiv1.Cluster{
		TypeMeta: metav1.TypeMeta{
			Kind:       apiv1.ClusterKind,
			APIVersion: apiv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterName,
			Namespace: namespace,
		},
	}
}

func newOwnedPod(cluster *apiv1.Cluster) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: cluster.Namespace,
		},
	}
	cluster.SetInheritedDataAndOwnership(&pod.ObjectMeta)
	return pod
}

func newUnownedPod(namespace, instanceName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName,
			Namespace: namespace,
		},
	}
}

// newOwnedPVC builds a PVC as it looks right after being created for an instance:
// owned by the cluster and carrying the role labels GetInstancePVCs discovers it by.
func newOwnedPVC(
	cluster *apiv1.Cluster,
	calc persistentvolumeclaim.ExpectedObjectCalculator,
) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      calc.GetName(instanceName),
			Namespace: cluster.Namespace,
			Labels:    calc.GetLabels(instanceName),
			Annotations: map[string]string{
				utils.PVCStatusAnnotationName: persistentvolumeclaim.StatusReady,
			},
		},
	}
	cluster.SetInheritedDataAndOwnership(&pvc.ObjectMeta)
	return pvc
}

// newDetachedPVC builds a PVC as the keep-pvc path leaves it behind: no cluster
// ownership, but still discoverable and marked detached for the instance.
func newDetachedPVC(
	namespace string,
	calc persistentvolumeclaim.ExpectedObjectCalculator,
	instanceName string,
) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      calc.GetName(instanceName),
			Namespace: namespace,
			Labels:    calc.GetLabels(instanceName),
			Annotations: map[string]string{
				utils.PVCStatusAnnotationName: persistentvolumeclaim.StatusDetached,
			},
		},
	}
}

// newDanglingPVC builds a PVC that is discoverable and unowned but was never
// marked detached, e.g. leftover from an unrelated state.
func newDanglingPVC(
	namespace string,
	calc persistentvolumeclaim.ExpectedObjectCalculator,
	instanceName string,
) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      calc.GetName(instanceName),
			Namespace: namespace,
			Labels:    calc.GetLabels(instanceName),
			Annotations: map[string]string{
				utils.PVCStatusAnnotationName: persistentvolumeclaim.StatusReady,
			},
		},
	}
}

func newInstanceJob(namespace, instanceName string) *batchv1.Job {
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instanceName + "-join-job",
			Namespace: namespace,
			Labels: map[string]string{
				utils.InstanceNameLabelName: instanceName,
			},
		},
	}
}

var _ = Describe("Destroy", func() {
	BeforeEach(func() {
		plugin.Namespace = namespace
		redirectStdoutToGinkgoWriter()
	})

	It("deletes the PVCs, the pod and the job when not keeping the PVC", func(ctx SpecContext) {
		cluster := newOwningCluster()
		pod := newOwnedPod(cluster)
		pgData := newOwnedPVC(cluster, persistentvolumeclaim.NewPgDataCalculator())
		pgWal := newOwnedPVC(cluster, persistentvolumeclaim.NewPgWalCalculator())
		job := newInstanceJob(namespace, instanceName)

		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pod, pgData, pgWal, job).
			Build()

		Expect(Destroy(ctx, clusterName, instanceName, false)).To(Succeed())

		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(pgData), &corev1.PersistentVolumeClaim{}))).To(BeTrue())
		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(pgWal), &corev1.PersistentVolumeClaim{}))).To(BeTrue())
		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}))).To(BeTrue())
	})

	It("detaches the PVCs, deletes the pod and deletes the job when keeping the PVC", func(ctx SpecContext) {
		cluster := newOwningCluster()
		pod := newOwnedPod(cluster)
		pgData := newOwnedPVC(cluster, persistentvolumeclaim.NewPgDataCalculator())
		pgWal := newOwnedPVC(cluster, persistentvolumeclaim.NewPgWalCalculator())
		job := newInstanceJob(namespace, instanceName)

		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pod, pgData, pgWal, job).
			Build()

		Expect(Destroy(ctx, clusterName, instanceName, true)).To(Succeed())

		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{}))).To(BeTrue())
		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}))).To(BeTrue())

		for _, seeded := range []*corev1.PersistentVolumeClaim{pgData, pgWal} {
			var updated corev1.PersistentVolumeClaim
			Expect(plugin.Client.Get(ctx, client.ObjectKeyFromObject(seeded), &updated)).To(Succeed())
			_, isOwned := controller.IsOwnedByCluster(&updated)
			Expect(isOwned).To(BeFalse())
			Expect(updated.Annotations[utils.PVCStatusAnnotationName]).To(Equal(persistentvolumeclaim.StatusDetached))
			Expect(updated.Labels[utils.InstanceNameLabelName]).To(Equal(instanceName))
		}
	})

	It("deletes the PVCs before the pod when not keeping the PVC", func(ctx SpecContext) {
		cluster := newOwningCluster()
		pod := newOwnedPod(cluster)
		pgData := newOwnedPVC(cluster, persistentvolumeclaim.NewPgDataCalculator())

		var callOrder []string
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pod, pgData).
			WithInterceptorFuncs(interceptor.Funcs{
				Delete: func(
					ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption,
				) error {
					switch obj.(type) {
					case *corev1.PersistentVolumeClaim:
						callOrder = append(callOrder, "delete-pvc")
					case *corev1.Pod:
						callOrder = append(callOrder, "delete-pod")
					}
					return c.Delete(ctx, obj, opts...)
				},
			}).
			Build()

		Expect(Destroy(ctx, clusterName, instanceName, false)).To(Succeed())

		Expect(callOrder).To(Equal([]string{"delete-pvc", "delete-pod"}))
	})

	It("detaches the PVCs before deleting the pod when keeping the PVC", func(ctx SpecContext) {
		cluster := newOwningCluster()
		pod := newOwnedPod(cluster)
		pgData := newOwnedPVC(cluster, persistentvolumeclaim.NewPgDataCalculator())

		var callOrder []string
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pod, pgData).
			WithInterceptorFuncs(interceptor.Funcs{
				Update: func(
					ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.UpdateOption,
				) error {
					if _, ok := obj.(*corev1.PersistentVolumeClaim); ok {
						callOrder = append(callOrder, "update-pvc")
					}
					return c.Update(ctx, obj, opts...)
				},
				Delete: func(
					ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.DeleteOption,
				) error {
					if _, ok := obj.(*corev1.Pod); ok {
						callOrder = append(callOrder, "delete-pod")
					}
					return c.Delete(ctx, obj, opts...)
				},
			}).
			Build()

		Expect(Destroy(ctx, clusterName, instanceName, true)).To(Succeed())

		Expect(callOrder).To(Equal([]string{"update-pvc", "delete-pod"}))
	})

	It("returns an error when the pod is not owned by the cluster, without keeping the PVC", func(ctx SpecContext) {
		pod := newUnownedPod(namespace, instanceName)
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pod).
			Build()

		err := Destroy(ctx, clusterName, instanceName, false)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not owned by cluster"))
	})

	It("returns an error when the pod is not owned by the cluster, keeping the PVC", func(ctx SpecContext) {
		pod := newUnownedPod(namespace, instanceName)
		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pod).
			Build()

		err := Destroy(ctx, clusterName, instanceName, true)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("is not owned by cluster"))
	})

	It("cleans up the PVCs and the job and succeeds when the pod is already gone", func(ctx SpecContext) {
		cluster := newOwningCluster()
		pgData := newOwnedPVC(cluster, persistentvolumeclaim.NewPgDataCalculator())
		job := newInstanceJob(namespace, instanceName)

		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pgData, job).
			Build()

		Expect(Destroy(ctx, clusterName, instanceName, false)).To(Succeed())

		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(pgData), &corev1.PersistentVolumeClaim{}))).To(BeTrue())
		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(job), &batchv1.Job{}))).To(BeTrue())
	})

	It("deletes an already-detached PVC left over from a previous keep-pvc run", func(ctx SpecContext) {
		pgData := newDetachedPVC(namespace, persistentvolumeclaim.NewPgDataCalculator(), instanceName)

		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pgData).
			Build()

		Expect(Destroy(ctx, clusterName, instanceName, false)).To(Succeed())

		Expect(apierrs.IsNotFound(
			plugin.Client.Get(ctx, client.ObjectKeyFromObject(pgData), &corev1.PersistentVolumeClaim{}))).To(BeTrue())
	})

	It("leaves an unowned PVC without the detached marker untouched", func(ctx SpecContext) {
		pgData := newDanglingPVC(namespace, persistentvolumeclaim.NewPgDataCalculator(), instanceName)

		plugin.Client = fake.NewClientBuilder().
			WithScheme(scheme.BuildWithAllKnownScheme()).
			WithObjects(pgData).
			Build()

		Expect(Destroy(ctx, clusterName, instanceName, false)).To(Succeed())

		var stillThere corev1.PersistentVolumeClaim
		Expect(plugin.Client.Get(ctx, client.ObjectKeyFromObject(pgData), &stillThere)).To(Succeed())
	})
})
