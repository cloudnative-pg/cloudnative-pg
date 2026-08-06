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

package webserver

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("getContinuousArchivingCondition", func() {
	It("reports a failure when an error is set", func() {
		asr := ArchiveStatusRequest{Error: "boom"}

		condition := asr.getContinuousArchivingCondition()
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(string(apiv1.ConditionReasonContinuousArchivingFailing)))
		Expect(condition.Message).To(Equal("boom"))
	})

	It("gives the error precedence over the not-configured flag", func() {
		asr := ArchiveStatusRequest{Error: "boom", NotConfigured: true}

		condition := asr.getContinuousArchivingCondition()
		Expect(condition.Status).To(Equal(metav1.ConditionFalse))
		Expect(condition.Reason).To(Equal(string(apiv1.ConditionReasonContinuousArchivingFailing)))
	})

	It("reports an archiver-less no-op as True with a distinct reason", func() {
		asr := ArchiveStatusRequest{NotConfigured: true}

		condition := asr.getContinuousArchivingCondition()
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(string(apiv1.ConditionReasonContinuousArchivingNotConfigured)))
	})

	It("reports a real archiving success as True/Success", func() {
		asr := ArchiveStatusRequest{}

		condition := asr.getContinuousArchivingCondition()
		Expect(condition.Status).To(Equal(metav1.ConditionTrue))
		Expect(condition.Reason).To(Equal(string(apiv1.ConditionReasonContinuousArchivingSuccess)))
	})
})
