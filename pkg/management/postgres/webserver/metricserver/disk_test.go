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

package metricserver

import (
	"github.com/prometheus/client_golang/prometheus/testutil"

	pgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("disk metrics", func() {
	It("sets the four disk gauges from volume usages", func() {
		m := newMetrics()
		usages := []pgpostgres.VolumeUsage{
			{Name: "pgdata", MountPoint: "/pgdata", TotalBytes: 200, UsedBytes: 50, AvailableBytes: 140},
		}

		setDiskMetrics(m, usages)

		Expect(testutil.ToFloat64(m.DiskTotalBytes.WithLabelValues("pgdata"))).To(Equal(200.0))
		Expect(testutil.ToFloat64(m.DiskUsedBytes.WithLabelValues("pgdata"))).To(Equal(50.0))
		Expect(testutil.ToFloat64(m.DiskAvailableBytes.WithLabelValues("pgdata"))).To(Equal(140.0))
		Expect(testutil.ToFloat64(m.DiskPercentUsed.WithLabelValues("pgdata"))).To(BeNumerically("~", 25.0, 0.001))
	})
})
