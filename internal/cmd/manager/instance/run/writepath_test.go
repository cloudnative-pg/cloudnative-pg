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

package run

import (
	"context"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/instance/run/lease"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type recordingAcquirer struct {
	stopRenewals int
	yields       int
}

func (*recordingAcquirer) Acquire(context.Context, lease.Config) error { return nil }
func (f *recordingAcquirer) Yield(context.Context)                     { f.yields++ }
func (f *recordingAcquirer) StopRenewing(context.Context)              { f.stopRenewals++ }

var _ = Describe("newWritePathMonitor", func() {
	It("keeps the lease-age monitor by stopping the renewal instead of yielding the lease", func() {
		acquirer := &recordingAcquirer{}
		instance := postgres.NewInstance().WithPodName("cluster-example-1")
		monitor := newWritePathMonitor(instance, acquirer)

		monitor.OnStalled()

		Expect(acquirer.stopRenewals).To(Equal(1))
		Expect(acquirer.yields).To(BeZero())
	})

	It("reads the configuration of the cluster", func() {
		instance := postgres.NewInstance().WithPodName("cluster-example-1")
		monitor := newWritePathMonitor(instance, &recordingAcquirer{})
		Expect(monitor.Config().Enabled).To(BeFalse())

		enabled := true
		period, timeout, clear := int32(5), int32(20), int32(2)
		instance.SetCluster(&apiv1.Cluster{Spec: apiv1.ClusterSpec{Probes: &apiv1.ProbesConfiguration{
			Liveness: &apiv1.LivenessProbe{WritePathCheck: &apiv1.WritePathCheckConfiguration{
				Enabled: &enabled, PeriodSeconds: &period, TimeoutSeconds: &timeout, ClearAfterSuccesses: &clear,
			}},
		}}})
		cfg := monitor.Config()
		Expect(cfg.Enabled).To(BeTrue())
		Expect(cfg.Period.Seconds()).To(BeEquivalentTo(5))
		Expect(cfg.Timeout.Seconds()).To(BeEquivalentTo(20))
		Expect(cfg.ClearAfterSuccesses).To(Equal(2))
	})

	It("declares a failover started only when another instance is the target primary", func() {
		instance := postgres.NewInstance().WithPodName("cluster-example-1")
		monitor := newWritePathMonitor(instance, &recordingAcquirer{})

		instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: "cluster-example-1"}})
		Expect(monitor.FailoverStarted()).To(BeFalse())
		instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: apiv1.PendingFailoverMarker}})
		Expect(monitor.FailoverStarted()).To(BeTrue())
		instance.SetCluster(&apiv1.Cluster{Status: apiv1.ClusterStatus{TargetPrimary: "cluster-example-2"}})
		Expect(monitor.FailoverStarted()).To(BeTrue())
	})
})
