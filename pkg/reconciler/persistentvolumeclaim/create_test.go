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

package persistentvolumeclaim

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing create function", func() {
	ctx := context.Background()
	cluster := &apiv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "test-name", Namespace: "default"}}
	var cli k8client.WithWatch
	var instanceName string
	var pvcName string
	var cc *CreateConfiguration

	BeforeEach(func() {
		cli = fake.NewClientBuilder().WithScheme(schemeBuilder.BuildWithAllKnownScheme()).Build()
		cc = &CreateConfiguration{
			Status:     StatusReady,
			NodeSerial: 1,
			Calculator: NewPgDataCalculator(),
			Storage: apiv1.StorageConfiguration{
				Size: "1Gi",
			},
		}

		instanceName = specs.GetInstanceName(cluster.Name, cc.NodeSerial)
		pvcName = cc.Calculator.GetName(instanceName)
	})

	Context("when PVC does not exist", func() {
		It("should create the PVC successfully", func() {
			err := createIfNotExists(ctx, cli, cluster, cc)
			Expect(err).ToNot(HaveOccurred())

			var expectedPVC corev1.PersistentVolumeClaim
			err = cli.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: "default"}, &expectedPVC)
			Expect(err).ToNot(HaveOccurred())

			Expect(expectedPVC.Annotations).To(Equal(map[string]string{
				utils.ClusterSerialAnnotationName:   "1",
				utils.OperatorVersionAnnotationName: versions.Version,
				utils.PVCStatusAnnotationName:       "ready",
			}))
			Expect(expectedPVC.Spec.AccessModes).To(Equal([]corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}))
			Expect(expectedPVC.Spec.Resources.Requests).To(Equal(corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse(cc.Storage.Size),
			}))
		})
	})

	Context("when PVC already exists", func() {
		It("should not return an error", func() {
			cli = fake.NewClientBuilder().
				WithScheme(schemeBuilder.BuildWithAllKnownScheme()).
				WithObjects(&corev1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      pvcName,
						Namespace: "default",
					},
				}).
				Build()

			err := createIfNotExists(ctx, cli, cluster, cc)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	It("should return ErrNextLoop on invalid size", func() {
		cc.Storage.Size = "typo"
		err := createIfNotExists(ctx, cli, cluster, cc)
		Expect(err).To(Equal(utils.ErrNextLoop))
	})
})
