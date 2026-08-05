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

package specs

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ApplyBootstrapOverlay", func() {
	// runPod returns a minimal instance pod carrying just the steady-state
	// "instance run" command, so the overlay-appended args are everything after
	// the first three elements.
	runPod := func() *corev1.Pod {
		return &corev1.Pod{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:    PostgresContainerName,
						Command: []string{"/controller/manager", "instance", "run"},
					},
				},
			},
		}
	}

	It("stamps the bootstrap annotation and preserves the drift annotations", func(ctx SpecContext) {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				ImageName: "postgres:18.0",
				Bootstrap: &apiv1.BootstrapConfiguration{InitDB: &apiv1.BootstrapInitDB{}},
			},
		}

		pod, err := NewInstance(ctx, cluster, 1, true)
		Expect(err).ToNot(HaveOccurred())

		specBefore := pod.Annotations[utils.PodSpecAnnotationName]
		//nolint:staticcheck // still in use for backward compatibility
		envHashBefore := pod.Annotations[utils.PodEnvHashAnnotationName]
		Expect(specBefore).ToNot(BeEmpty())

		Expect(ApplyBootstrapOverlay(pod, NewInitDBInstruction(cluster))).To(Succeed())

		// The overlay must not touch the annotations the drift check reads, so a
		// just-bootstrapped pod is not seen as outdated.
		Expect(pod.Annotations[utils.PodSpecAnnotationName]).To(Equal(specBefore))
		//nolint:staticcheck // still in use for backward compatibility
		Expect(pod.Annotations[utils.PodEnvHashAnnotationName]).To(Equal(envHashBefore))

		Expect(pod.Annotations[utils.BootstrapInstanceAnnotationName]).To(Equal("initdb"))
		Expect(pod.Spec.Containers[0].Command).To(ContainElement("--bootstrap-mode=initdb"))
	})

	It("adds the APP_USERNAME environment for an initdb creating the application database", func() {
		cluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{Database: "app", Owner: "app"},
				},
			},
		}
		pod := runPod()
		Expect(ApplyBootstrapOverlay(pod, NewInitDBInstruction(cluster))).To(Succeed())

		Expect(pod.Spec.Containers[0].Env).To(ContainElement(HaveField("Name", "APP_USERNAME")))
	})

	It("rejects a pod whose first container is not the postgres container", func() {
		pod := &corev1.Pod{
			Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "sidecar"}}},
		}
		Expect(ApplyBootstrapOverlay(pod, NewJoinInstruction(apiv1.Cluster{}))).ToNot(Succeed())
	})

	It("never mounts the barman endpoint CA on a restore or snapshot overlay", func() {
		clusterWithCA := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Backup: &apiv1.BackupSource{
							EndpointCA: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{Name: "ca"},
								Key:                  "ca.crt",
							},
						},
					},
				},
			},
		}

		// The recovery endpoint CA is written to disk during the in-process
		// bootstrap, never mounted, so the overlay adds no CA volume, mount or env.
		for _, instruction := range []BootstrapInstruction{
			NewRecoveryInstruction(clusterWithCA, nil),
			NewRestoreSnapshotInstruction(clusterWithCA, &metav1.ObjectMeta{}, nil),
		} {
			pod := runPod()
			Expect(ApplyBootstrapOverlay(pod, instruction)).To(Succeed())
			Expect(pod.Spec.Volumes).ToNot(ContainElement(HaveField("Name", "barman-endpoint-ca")))
			Expect(pod.Spec.Containers[0].VolumeMounts).ToNot(ContainElement(HaveField("Name", "barman-endpoint-ca")))
			Expect(pod.Spec.Containers[0].Env).ToNot(ContainElement(HaveField("Name", "AWS_CA_BUNDLE")))
			Expect(pod.Spec.Containers[0].Env).ToNot(ContainElement(HaveField("Name", "REQUESTS_CA_BUNDLE")))
		}
	})

	It("never mounts an overlay volume onto a writable instance-manager directory", func() {
		// The base pod carries no volume mounts, so every mount present after the
		// overlay was added by it. None may target CertificatesDir (where the
		// instance manager reconciles server.crt/server.key) nor PGDATA/pg_wal: a
		// read-only Secret or ConfigMap mount would shadow those directories and
		// break the pod once it keeps running as the instance (see #11228).
		recoveryCluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					Recovery: &apiv1.BootstrapRecovery{
						Backup: &apiv1.BackupSource{
							EndpointCA: &apiv1.SecretKeySelector{
								LocalObjectReference: apiv1.LocalObjectReference{Name: "ca"},
								Key:                  "ca.crt",
							},
						},
					},
				},
			},
		}
		initDBCluster := apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: apiv1.ClusterSpec{
				Bootstrap: &apiv1.BootstrapConfiguration{
					InitDB: &apiv1.BootstrapInitDB{
						PostInitApplicationSQLRefs: &apiv1.SQLRefs{
							SecretRefs: []apiv1.SecretKeySelector{
								{LocalObjectReference: apiv1.LocalObjectReference{Name: "s"}, Key: "k"},
							},
						},
					},
				},
			},
		}

		forbidden := []string{postgres.CertificatesDir, PgDataPath, PgWalPath}

		for _, instruction := range []BootstrapInstruction{
			NewInitDBInstruction(initDBCluster),
			NewRecoveryInstruction(recoveryCluster, nil),
			NewRestoreSnapshotInstruction(recoveryCluster, &metav1.ObjectMeta{}, nil),
			NewRestoreSnapshotReplicaInstruction(recoveryCluster),
		} {
			pod := runPod()
			Expect(ApplyBootstrapOverlay(pod, instruction)).To(Succeed())
			for _, mount := range pod.Spec.Containers[0].VolumeMounts {
				Expect(forbidden).ToNot(ContainElement(mount.MountPath),
					"overlay mount %q must not shadow a writable instance directory", mount.Name)
			}
		}
	})
})
