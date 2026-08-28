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
	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/volumeusage"
	pgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
)

// setDiskMetrics resets and repopulates the disk gauges from the usages.
func setDiskMetrics(m *metrics, usages []pgpostgres.VolumeUsage) {
	m.DiskTotalBytes.Reset()
	m.DiskUsedBytes.Reset()
	m.DiskAvailableBytes.Reset()
	m.DiskPercentUsed.Reset()

	for _, u := range usages {
		m.DiskTotalBytes.WithLabelValues(u.Name).Set(float64(u.TotalBytes))
		m.DiskUsedBytes.WithLabelValues(u.Name).Set(float64(u.UsedBytes))
		m.DiskAvailableBytes.WithLabelValues(u.Name).Set(float64(u.AvailableBytes))

		var percent float64
		if u.TotalBytes > 0 {
			percent = 100 * float64(u.UsedBytes) / float64(u.TotalBytes)
		}
		m.DiskPercentUsed.WithLabelValues(u.Name).Set(percent)
	}
}

// collectDiskMetrics measures the instance's volumes and updates the gauges.
// It is safe to call even when PostgreSQL is down.
func (e *Exporter) collectDiskMetrics() {
	usages := volumeusage.Collect(volumeusage.DefaultBasePaths())
	log.Trace("collected disk usage metrics", "volumes", len(usages))
	setDiskMetrics(e.Metrics, usages)
}
