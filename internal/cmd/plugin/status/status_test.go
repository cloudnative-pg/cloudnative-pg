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
	"context"
	"fmt"
	"time"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getPrimaryPromotionTime", func() {
	var cluster *apiv1.Cluster

	Context("when CurrentPrimaryTimestamp is empty", func() {
		BeforeEach(func() {
			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: "",
				},
			}
		})

		It("should return an empty string", func() {
			Expect(getPrimaryPromotionTime(cluster)).To(Equal(""))
		})
	})

	Context("when CurrentPrimaryTimestamp is valid", func() {
		It("should return the formatted timestamp", func() {
			now := time.Now()
			uptime := 1 * time.Hour
			currentPrimaryTimestamp := now.Add(-uptime)

			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: currentPrimaryTimestamp.Format(metav1.RFC3339Micro),
				},
			}

			expected := fmt.Sprintf("%s (%s)", currentPrimaryTimestamp.Round(time.Second), uptime)
			Expect(getPrimaryPromotionTimeIdempotent(cluster, now)).To(Equal(expected))
		})
	})

	Context("when CurrentPrimaryTimestamp is invalid", func() {
		BeforeEach(func() {
			cluster = &apiv1.Cluster{
				Status: apiv1.ClusterStatus{
					CurrentPrimaryTimestamp: "invalid timestamp",
				},
			}
		})

		It("should return the error message", func() {
			Expect(getPrimaryPromotionTime(cluster)).To(ContainSubstring("error"))
		})
	})
})

var _ = Describe("getWalArchivingStatus", func() {
	It("should return 'Disabled' when WAL archiving is disabled", func() {
		result := getWalArchivingStatus(false, "", true)
		Expect(result).To(ContainSubstring("Disabled"))
	})

	It("should return 'OK' when archiving is working", func() {
		result := getWalArchivingStatus(true, "", false)
		Expect(result).To(ContainSubstring("OK"))
	})

	It("should return 'Failing' when there is a failed WAL", func() {
		result := getWalArchivingStatus(false, "000000010000000000000001", false)
		Expect(result).To(ContainSubstring("Failing"))
	})

	It("should return 'OK' even when there is a failed WAL if archiving is working", func() {
		result := getWalArchivingStatus(true, "000000010000000000000001", false)
		Expect(result).To(ContainSubstring("OK"))
	})

	It("should return 'Starting Up' when archiving hasn't started yet", func() {
		result := getWalArchivingStatus(false, "", false)
		Expect(result).To(ContainSubstring("Starting Up"))
	})

	It("should prioritize 'Disabled' over other statuses", func() {
		// Even if archiving is working, disabled should take precedence
		result := getWalArchivingStatus(true, "", true)
		Expect(result).To(ContainSubstring("Disabled"))
	})
})

var _ = Describe("status without the pods/exec permission", func() {
	var client *fake.Clientset

	BeforeEach(func() {
		client = fake.NewSimpleClientset()
		client.PrependReactor("create", "selfsubjectaccessreviews",
			func(action k8stesting.Action) (bool, runtime.Object, error) {
				review, ok := action.(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
				Expect(ok).To(BeTrue())
				review.Status.Allowed = false
				return true, review, nil
			})
	})

	It("reports it without running du", func() {
		fullStatus := &PostgresqlStatus{
			PrimaryPod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-example-1"}},
		}

		size, err := fullStatus.getClusterSize(context.Background(), client, time.Second)

		Expect(err).To(MatchError(errExecNotPermitted))
		Expect(size).To(BeEmpty())

		actions := client.Actions()
		Expect(actions).To(HaveLen(1))
		review, ok := actions[0].(k8stesting.CreateAction).GetObject().(*authorizationv1.SelfSubjectAccessReview)
		Expect(ok).To(BeTrue())
		Expect(review.Spec.ResourceAttributes).To(Equal(&authorizationv1.ResourceAttributes{
			Namespace:   "default",
			Verb:        "create",
			Resource:    "pods",
			Subresource: "exec",
			Name:        "cluster-example-1",
		}))
	})

	It("skips the configuration reads", func() {
		fullStatus := &PostgresqlStatus{
			PrimaryPod: corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "cluster-example-1"}},
		}

		errs := fullStatus.printPostgresConfiguration(context.Background(), client, time.Second)

		Expect(errs).To(BeEmpty())
		Expect(client.Actions()).To(HaveLen(1))
	})

	It("does not blame the permission when there is no primary Pod", func() {
		fullStatus := &PostgresqlStatus{}

		_, err := fullStatus.getClusterSize(context.Background(), client, time.Second)

		Expect(err).To(MatchError(utils.ErrorContainerNotFound))
		Expect(client.Actions()).To(BeEmpty())
	})
})
