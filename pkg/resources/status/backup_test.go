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

package status

import (
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("FlagBackupAsFailed", func() {
	scheme := schemeBuilder.BuildWithAllKnownScheme()
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&apiv1.Cluster{}, &apiv1.Backup{}).
		Build()

	It("selects the new target primary right away", func(ctx SpecContext) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
		}

		backup := &apiv1.Backup{
			ObjectMeta: metav1.ObjectMeta{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
			Spec: apiv1.BackupSpec{
				Cluster: apiv1.LocalObjectReference{
					Name: cluster.Name,
				},
			},
			Status: apiv1.BackupStatus{
				Phase: apiv1.BackupPhaseRunning,
			},
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		Expect(k8sClient.Create(ctx, backup)).To(Succeed())

		err := FlagBackupAsFailed(ctx, k8sClient, backup, cluster, errors.New("my sample error"))
		Expect(err).NotTo(HaveOccurred())

		// Backup status assertions
		Expect(backup.Status.Phase).To(BeEquivalentTo(apiv1.BackupPhaseFailed))
		Expect(backup.Status.Error).To(BeEquivalentTo("my sample error"))

		// Cluster status assertions
		Expect(cluster.Status.LastFailedBackup).ToNot(BeEmpty())
		for _, condition := range cluster.Status.Conditions {
			if condition.Type == string(apiv1.ConditionBackup) {
				Expect(condition.Status).To(BeEquivalentTo(metav1.ConditionFalse))
				Expect(condition.Reason).To(BeEquivalentTo(string(apiv1.ConditionReasonLastBackupFailed)))
				Expect(condition.Message).To(BeEquivalentTo("my sample error"))
			}
		}
	})
})
