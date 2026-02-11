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
	"database/sql"
	"math"
	"os"
	"regexp"

	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/webserver/client/local"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

func collectPGWalArchiveMetric(exporter *Exporter) error {
	ready, done, err := postgres.GetWALArchiveCounters()
	if err != nil {
		return err
	}

	exporter.Metrics.PgWALArchiveStatus.WithLabelValues("ready").Set(float64(ready))
	exporter.Metrics.PgWALArchiveStatus.WithLabelValues("done").Set(float64(done))
	return nil
}

func collectPGStatWAL(e *Exporter) error {
	walStat, err := e.instance.TryGetPgStatWAL()
	if walStat == nil || err != nil {
		return err
	}
	walMetrics := e.Metrics.PgStatWalMetrics
	walMetrics.WalRecords.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalRecords))
	walMetrics.WalFpi.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalFpi))
	walMetrics.WalBytes.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalBytes))
	walMetrics.WALBuffersFull.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WALBuffersFull))
	if version, _ := e.instance.GetPgVersion(); version.Major < 18 {
		walMetrics.WalWrite.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalWrite))
		walMetrics.WalSync.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalSync))
		walMetrics.WalWriteTime.WithLabelValues(walStat.StatsReset).Set(walStat.WalWriteTime)
		walMetrics.WalSyncTime.WithLabelValues(walStat.StatsReset).Set(walStat.WalSyncTime)
	}

	return nil
}

type walSettings struct {
	// expressed in bytes
	walSegmentSize float64
	// expressed in megabytes
	minWalSize float64
	// expressed in megabytes
	maxWalSize float64
	// normalized to obtain the same result of wal_keep_segments
	walKeepSizeNormalized float64
	maxSlotWalKeepSize    float64
	configSha256          string
}

func (s *walSettings) synchronize(db *sql.DB, configSha256 string) error {
	if s.configSha256 != "" && s.configSha256 == configSha256 {
		return nil
	}

	rows, err := db.Query(`
SELECT name, setting FROM pg_catalog.pg_settings
WHERE pg_settings.name
IN ('wal_segment_size', 'min_wal_size', 'max_wal_size', 'wal_keep_size', 'wal_keep_segments', 'max_slot_wal_keep_size')`) // nolint: lll
	if err != nil {
		log.Error(err, "while fetching rows")
		return err
	}

	defer func() {
		err = rows.Close()
		if err != nil {
			log.Error(err, "while closing rows for synchronizeWalSettings")
		}
	}()

	var needsKeepSizeNormalization bool
	for rows.Next() {
		var name string
		var setting *int
		if err := rows.Scan(&name, &setting); err != nil {
			log.Error(err, "while scanning values from the database")
			return err
		}

		normalizedSetting := float64(0)
		if setting != nil {
			normalizedSetting = float64(*setting)
		}

		switch name {
		case "wal_segment_size":
			s.walSegmentSize = normalizedSetting
		case "min_wal_size":
			s.minWalSize = normalizedSetting
		case "max_wal_size":
			s.maxWalSize = normalizedSetting
		case "wal_keep_size":
			needsKeepSizeNormalization = true
			s.walKeepSizeNormalized = normalizedSetting
		case "wal_keep_segments":
			s.walKeepSizeNormalized = normalizedSetting
		case "max_slot_wal_keep_size":
			s.maxSlotWalKeepSize = normalizedSetting
		}
	}

	if err := rows.Err(); err != nil {
		log.Error(err, "while iterating over rows")
		return err
	}

	if needsKeepSizeNormalization {
		s.walKeepSizeNormalized = utils.ToBytes(s.walKeepSizeNormalized) / s.walSegmentSize
	}

	s.configSha256 = configSha256

	return nil
}

var (
	regexPGWalFileName  = regexp.MustCompile("^[0-9A-F]{24}$")
	cachedWalPgSettings walSettings
)

func collectPGWalSettings(exporter *Exporter, db *sql.DB) error {
	pgWalDir, err := os.Open(specs.PgWalPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = pgWalDir.Close()
	}()
	files, err := pgWalDir.Readdirnames(-1)
	if err != nil {
		return err
	}
	var count int
	for _, file := range files {
		if !regexPGWalFileName.MatchString(file) {
			continue
		}
		count++
	}

	if err = cachedWalPgSettings.synchronize(db, exporter.instance.ConfigSha256); err != nil {
		return err
	}

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("count").
		Set(float64(count))

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("size").
		Set(float64(count) * cachedWalPgSettings.walSegmentSize)

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("min").
		Set(utils.ToBytes(cachedWalPgSettings.minWalSize) / cachedWalPgSettings.walSegmentSize)

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("max").
		Set(utils.ToBytes(cachedWalPgSettings.maxWalSize) / cachedWalPgSettings.walSegmentSize)

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("keep").
		Set(cachedWalPgSettings.walKeepSizeNormalized)

	switch cachedWalPgSettings.maxSlotWalKeepSize {
	case -1:
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("slots_max").
			Set(math.NaN())
	default:
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("slots_max").
			Set(utils.ToBytes(cachedWalPgSettings.maxSlotWalKeepSize) / cachedWalPgSettings.walSegmentSize)
	}

	walVolumeSize := getWalVolumeSize()
	switch walVolumeSize {
	case 0:
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("volume_size").
			Set(math.NaN())
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("volume_max").
			Set(math.NaN())
	default:
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("volume_size").
			Set(walVolumeSize)
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("volume_max").
			Set(walVolumeSize / cachedWalPgSettings.walSegmentSize)
	}

	return nil
}

func getWalVolumeSize() float64 {
	cluster, err := local.NewClient().Cache().GetCluster()
	if err != nil || !cluster.ShouldCreateWalArchiveVolume() {
		return 0
	}

	walSize := cluster.Spec.WalStorage.GetSizeOrNil()
	if walSize == nil {
		return 0
	}

	return walSize.AsApproximateFloat64()
}
