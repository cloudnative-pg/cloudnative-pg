/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metricserver

import (
	"database/sql"
	"math"
	"os"
	"regexp"

	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
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

func collectPGWALStat(e *Exporter) error {
	walStat, err := e.instance.TryGetPgStatWAL()
	if walStat == nil || err != nil {
		return err
	}
	walMetrics := e.Metrics.PgStatWalMetrics
	walMetrics.WalSync.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalSync))
	walMetrics.WalSyncTime.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalSyncTime))
	walMetrics.WALBuffersFull.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WALBuffersFull))
	walMetrics.WalFpi.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalFpi))
	walMetrics.WalWrite.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalWrite))
	walMetrics.WalBytes.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalBytes))
	walMetrics.WalWriteTime.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalWriteTime))
	walMetrics.WalRecords.WithLabelValues(walStat.StatsReset).Set(float64(walStat.WalRecords))

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

func (s walSettings) synchronizeWALSettings(db *sql.DB, configSha256 string) (walSettings, error) {
	if s.configSha256 == configSha256 {
		return s, nil
	}

	settings := walSettings{
		configSha256: configSha256,
	}
	rows, err := db.Query(`
SELECT name, setting FROM pg_settings 
WHERE pg_settings.name
IN ('wal_segment_size', 'min_wal_size', 'max_wal_size', 'wal_keep_size', 'wal_keep_segments', 'max_slot_wal_keep_size')`) // nolint: lll
	if err != nil {
		log.Error(err, "while fetching rows")
		return settings, err
	}
	if err := rows.Err(); err != nil {
		log.Error(err, "while iterating over rows")
		return settings, err
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
			return settings, err
		}

		normalizedSetting := float64(0)
		if setting != nil {
			normalizedSetting = float64(*setting)
		}

		switch name {
		case "wal_segment_size":
			settings.walSegmentSize = normalizedSetting
		case "min_wal_size":
			settings.minWalSize = normalizedSetting
		case "max_wal_size":
			settings.maxWalSize = normalizedSetting
		case "wal_keep_size":
			needsKeepSizeNormalization = true
			settings.walKeepSizeNormalized = normalizedSetting
		case "wal_keep_segments":
			settings.walKeepSizeNormalized = normalizedSetting
		case "max_slot_wal_keep_size":
			settings.maxSlotWalKeepSize = normalizedSetting
		}
	}

	if needsKeepSizeNormalization {
		settings.walKeepSizeNormalized = (settings.walKeepSizeNormalized * 1024 * 1024) / settings.walSegmentSize
	}

	return settings, nil
}

var (
	regexPGWalFileName   = regexp.MustCompile("^[0-9A-F]{24}")
	lastKnownWalSettings walSettings
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

	lastKnownWalSettings, err = lastKnownWalSettings.synchronizeWALSettings(db, exporter.instance.ConfigSha256)
	if err != nil {
		return err
	}

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("count").
		Set(float64(count))

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("size").
		Set(float64(count) * lastKnownWalSettings.walSegmentSize)

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("min").
		Set(lastKnownWalSettings.minWalSize * 1024 * 1024 / lastKnownWalSettings.walSegmentSize)

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("max").
		Set(lastKnownWalSettings.maxWalSize * 1024 * 1024 / lastKnownWalSettings.walSegmentSize)

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("keep").
		Set(lastKnownWalSettings.walKeepSizeNormalized)

	if lastKnownWalSettings.maxSlotWalKeepSize == -1 || lastKnownWalSettings.maxSlotWalKeepSize == 0 {
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("slots_max").
			Set(math.NaN())
	} else {
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("slots_max").
			Set(lastKnownWalSettings.maxSlotWalKeepSize * 1024 * 1024 / lastKnownWalSettings.walSegmentSize)
	}

	walVolumeSize := getWalVolumeSize()
	if walVolumeSize == 0 {
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("volume_size").
			Set(math.NaN())
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("volume_max").
			Set(math.NaN())
	} else {
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("volume_size").
			Set(walVolumeSize)
		exporter.Metrics.PgWALDirectory.
			WithLabelValues("volume_max").
			Set(walVolumeSize / lastKnownWalSettings.walSegmentSize)
	}

	return nil
}

func getWalVolumeSize() float64 {
	cluster, err := cache.LoadCluster()
	if err != nil || !cluster.ShouldCreateWalArchiveVolume() {
		return 0
	}

	walSize := cluster.Spec.WalStorage.GetSizeOrNil()
	if walSize == nil {
		return 0
	}

	return walSize.AsApproximateFloat64()
}
