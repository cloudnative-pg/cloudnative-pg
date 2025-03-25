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

package rollout

import (
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rollout manager", func() {
	It("should coordinate rollouts when delays are set", func() {
		startTime := time.Now()
		currentTime := startTime

		const (
			clustersRolloutDelay  = 10 * time.Minute
			instancesRolloutDelay = 5 * time.Minute
		)

		m := New(clustersRolloutDelay, instancesRolloutDelay)
		m.timeProvider = func() time.Time {
			return currentTime
		}

		By("allowing the first rollout immediately", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-example",
			}, "cluster-example-1")

			Expect(result.RolloutAllowed).To(BeTrue())
			Expect(result.TimeToWait).To(BeZero())
		})

		By("waiting for one minute", func() {
			currentTime = currentTime.Add(1 * time.Minute)
		})

		By("checking that a rollout of an instance is not allowed", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-example",
			}, "cluster-example-2")

			Expect(result.RolloutAllowed).To(BeFalse())
			Expect(result.TimeToWait).To(Equal(4 * time.Minute))
			Expect(m.lastUpdate).To(Equal(startTime))
		})

		By("checking that a rollout of a cluster is not allowed", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-bis",
			}, "cluster-bis-1")

			Expect(result.RolloutAllowed).To(BeFalse())
			Expect(result.TimeToWait).To(Equal(9 * time.Minute))
			Expect(m.lastUpdate).To(Equal(startTime))
		})

		By("waiting for five minutes", func() {
			currentTime = currentTime.Add(5 * time.Minute)
		})

		By("checking that a rollout of a cluster is still not allowed", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-bis",
			}, "cluster-bis-1")

			Expect(result.RolloutAllowed).To(BeFalse())
			Expect(result.TimeToWait).To(Equal(4 * time.Minute))
			Expect(m.lastUpdate).To(Equal(startTime))
		})

		By("checking that a rollout of an instance is allowed", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-example",
			}, "cluster-example-2")

			Expect(result.RolloutAllowed).To(BeTrue())
			Expect(result.TimeToWait).To(BeZero())
			Expect(m.lastUpdate).To(Equal(currentTime))
		})

		By("waiting for other eleven minutes", func() {
			currentTime = currentTime.Add(11 * time.Minute)
		})

		By("checking that a rollout of a cluster is allowed", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-bis",
			}, "cluster-bis-1")

			Expect(result.RolloutAllowed).To(BeTrue())
			Expect(result.TimeToWait).To(BeZero())
			Expect(m.lastUpdate).To(Equal(currentTime))
		})
	})

	It("should allow all rollouts when delays are not set", func() {
		m := New(0, 0)

		By("allowing the first rollout immediately", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-example",
			}, "cluster-example-1")

			Expect(result.RolloutAllowed).To(BeTrue())
			Expect(result.TimeToWait).To(BeZero())
		})

		By("allowing a rollout of an instance", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-example",
			}, "cluster-example-2")

			Expect(result.RolloutAllowed).To(BeTrue())
			Expect(result.TimeToWait).To(BeZero())
		})

		By("allowing a rollout of an cluster", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-bis",
			}, "cluster-bis-1")

			Expect(result.RolloutAllowed).To(BeTrue())
			Expect(result.TimeToWait).To(BeZero())
		})

		By("allowing a rollout of another instance", func() {
			result := m.CoordinateRollout(client.ObjectKey{
				Namespace: "default",
				Name:      "cluster-example",
			}, "cluster-example-3")

			Expect(result.RolloutAllowed).To(BeTrue())
			Expect(result.TimeToWait).To(BeZero())
		})
	})
})
