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
	walSegmentSize int
	// expressed in megabytes
	minWalSize int
	// expressed in megabytes
	maxWalSize  int
	walKeepSize int
}

var (
	regexPGWalFileName   = regexp.MustCompile("^[0-9A-F]{24}")
	lastKnownConfSHA     string
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

	if lastKnownConfSHA != exporter.instance.ConfigSha256 {
		lastKnownWalSettings, err = getWALSettings(db)
		if err != nil {
			return err
		}
	}

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("count").
		Set(float64(count))

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("size").
		Set(float64(count * lastKnownWalSettings.walSegmentSize))

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("min").
		Set(float64(lastKnownWalSettings.minWalSize*1024*1024) / float64(lastKnownWalSettings.walSegmentSize))

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("max").
		Set(float64(lastKnownWalSettings.maxWalSize*1024*1024) / float64(lastKnownWalSettings.walSegmentSize))

	exporter.Metrics.PgWALDirectory.
		WithLabelValues("keep").
		Set(float64(lastKnownWalSettings.walKeepSize))

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
			Set(walVolumeSize / float64(lastKnownWalSettings.walSegmentSize))
	}

	lastKnownConfSHA = exporter.instance.ConfigSha256
	return nil
}

func getWALSettings(db *sql.DB) (walSettings, error) {
	settings := walSettings{}
	rows, err := db.Query(`
SELECT name, setting FROM pg_settings 
WHERE pg_settings.name
IN ('wal_segment_size', 'min_wal_size', 'max_wal_size', 'wal_keep_size', 'wal_keep_segments')`)
	if err != nil {
		return settings, err
	}
	if err := rows.Err(); err != nil {
		log.Error(err, "while iterating over rows")
		return settings, err
	}

	defer func() {
		err = rows.Close()
		if err != nil {
			log.Error(err, "while closing rows for SHOW LISTS")
		}
	}()

	for rows.Next() {
		var name string
		var setting *int
		if err := rows.Scan(&name, &setting); err != nil {
			log.Error(err, "while scanning values from the database")
			return settings, err
		}

		var normalizedSetting int
		if setting != nil {
			normalizedSetting = *setting
		}

		switch name {
		case "wal_segment_size":
			settings.walSegmentSize = normalizedSetting
		case "min_wal_size":
			settings.minWalSize = normalizedSetting
		case "max_wal_size":
			settings.maxWalSize = normalizedSetting
		case "wal_keep_size", "wal_keep_segments":
			settings.walKeepSize = normalizedSetting
		}
	}

	return settings, nil
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
