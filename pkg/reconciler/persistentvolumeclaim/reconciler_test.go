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

package persistentvolumeclaim

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	utils2 "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type clientMock struct {
	client.Reader
	client.Writer
	client.StatusClient
	client.SubResourceClientConstructor

	timesCalled int
}

func (cm *clientMock) Patch(
	ctx context.Context, obj client.Object,
	patch client.Patch, opts ...client.PatchOption,
) error {
	cm.timesCalled++
	return nil
}

func (cm *clientMock) Scheme() *runtime.Scheme {
	return &runtime.Scheme{}
}

func (cm *clientMock) RESTMapper() meta.RESTMapper {
	return nil
}

var _ = Describe("PVC reconciliation", func() {
	const clusterName = "myCluster"

	It("will reconcile each PVC's pvc-role labels if there are no pods", func() {
		cl := clientMock{}
		pvcs := []corev1.PersistentVolumeClaim{
			makePVC(clusterName, "1", utils2.PVCRolePgData, false),
			makePVC(clusterName, "2", utils2.PVCRolePgWal, false),      // role is out of sync with name
			makePVC(clusterName, "3-wal", utils2.PVCRolePgData, false), // role is out of sync with name
			makePVC(clusterName, "3", utils2.PVCRolePgData, false),
		}
		instanceNames := []string{clusterName + "-1", clusterName + "-2", clusterName + "-3"}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: instanceNames,
			},
		}

		err := reconcileOperatorLabels(
			context.Background(),
			&cl,
			cluster,
			[]corev1.Pod{},
			pvcs)
		Expect(err).NotTo(HaveOccurred())
		// we expect to patch only the two PVC's whose role does not match their name
		Expect(cl.timesCalled).To(Equal(2))
	})
	It("will reconcile each PVC's pvc-role and instance-relative labels if there are pods", func() {
		cl := clientMock{}
		clusterName := "myCluster"

		pods := []corev1.Pod{
			makePod(clusterName, "1"), // pvc instanceName should be set to this pod name
			makePod(clusterName, "2"), // pvc instanceName should be set to this pod name
			makePod(clusterName, "3"), // pvc instanceName should be set to this pod name
		}

		pvcs := []corev1.PersistentVolumeClaim{
			makePVC(clusterName, "1", utils2.PVCRolePgData, false),
			makePVC(clusterName, "2", utils2.PVCRolePgWal, false),      // role is out of sync with name
			makePVC(clusterName, "3-wal", utils2.PVCRolePgData, false), // role is out of sync with name
			makePVC(clusterName, "3", utils2.PVCRolePgData, false),
		}
		instanceNames := []string{clusterName + "-1", clusterName + "-2", clusterName + "-3"}
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
			Status: apiv1.ClusterStatus{
				InstanceNames: instanceNames,
			},
		}

		err := reconcileOperatorLabels(
			context.Background(),
			&cl,
			cluster,
			pods,
			pvcs)
		Expect(err).NotTo(HaveOccurred())
		// we expect to patch all the PVC's with the instanceName label
		Expect(cl.timesCalled).To(Equal(4))
	})
})
