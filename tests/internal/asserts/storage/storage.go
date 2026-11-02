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

// Package storage provides Ginkgo/Gomega assertions over PVC state.
// Callers that also import tests/utils/storage should alias one of the
// two to avoid the package name collision.
package storage

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	storageutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// AssertPvcHasLabels verifies if the PVCs of a cluster in a given
// namespace contain the expected labels, and that their values reflect
// the current status of the related pods.
func AssertPvcHasLabels(
	env *environment.TestingEnvironment,
	namespace, clusterName string,
) {
	GinkgoHelper()
	By("checking PVC have the correct role labels", func() {
		Eventually(func(g Gomega) {
			pvcList, err := storageutils.GetPVCList(env.Ctx, env.Client, namespace)
			g.Expect(err).ToNot(HaveOccurred())

			for _, pvc := range pvcList.Items {
				podName := fmt.Sprintf("%v-%v", clusterName, pvc.Annotations[utils.ClusterSerialAnnotationName])
				pod := &corev1.Pod{}
				podNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      podName,
				}
				err = env.Client.Get(env.Ctx, podNamespacedName, pod)
				g.Expect(err).ToNot(HaveOccurred())

				ExpectedRole := "replica"
				if specs.IsPodPrimary(*pod) {
					ExpectedRole = "primary"
				}
				ExpectedPvcRole := "PG_DATA"
				if pvc.Name == podName+"-wal" {
					ExpectedPvcRole = "PG_WAL"
				}
				expectedLabels := map[string]string{
					utils.ClusterLabelName:             clusterName,
					utils.PvcRoleLabelName:             ExpectedPvcRole,
					utils.ClusterInstanceRoleLabelName: ExpectedRole,
				}
				g.Expect(storageutils.PvcHasLabels(pvc, expectedLabels)).To(BeTrue(),
					fmt.Sprintf("expectedLabels: %v and found actualLabels on pvc: %v",
						expectedLabels, pod.GetLabels()))
			}
		}, 300, 5).Should(Succeed())
	})
}
