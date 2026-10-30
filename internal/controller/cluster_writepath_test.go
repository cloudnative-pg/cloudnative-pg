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
	"time"

	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("withWritePathCheckRequeue", func() {
	clusterWithCheck := func(enabled bool, period int) *apiv1.Cluster {
		return &apiv1.Cluster{
			Spec: apiv1.ClusterSpec{
				Probes: &apiv1.ProbesConfiguration{
					Liveness: &apiv1.LivenessProbe{
						WritePathCheck: &apiv1.WritePathCheckConfiguration{
							Enabled:       ptr.To(enabled),
							PeriodSeconds: ptr.To(int32(period)),
						},
					},
				},
			},
		}
	}

	It("leaves the result alone when the check is disabled", func() {
		Expect(withWritePathCheckRequeue(&apiv1.Cluster{}, ctrl.Result{})).To(Equal(ctrl.Result{}))
		Expect(withWritePathCheckRequeue(clusterWithCheck(false, 10), ctrl.Result{})).To(Equal(ctrl.Result{}))
	})

	It("requeues after the check period when nothing else is scheduled", func() {
		Expect(withWritePathCheckRequeue(clusterWithCheck(true, 10), ctrl.Result{})).
			To(Equal(ctrl.Result{RequeueAfter: 10 * time.Second}))
	})

	It("shortens a longer requeue to the check period", func() {
		Expect(withWritePathCheckRequeue(clusterWithCheck(true, 10), ctrl.Result{RequeueAfter: time.Minute})).
			To(Equal(ctrl.Result{RequeueAfter: 10 * time.Second}))
	})

	It("keeps a shorter requeue", func() {
		Expect(withWritePathCheckRequeue(clusterWithCheck(true, 10), ctrl.Result{RequeueAfter: time.Second})).
			To(Equal(ctrl.Result{RequeueAfter: time.Second}))
	})
})
