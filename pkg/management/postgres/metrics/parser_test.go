/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package metrics

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestUtils(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Metrics parser suite")
}

var _ = Describe("Metrics parser", func() {
	It("correctly handles the pg_exporter example queries", func() {
		result, err := ParseQueries([]byte(pgExporterQueries))
		Expect(err).To(BeNil())

		Expect(result).ToNot(BeNil())
		Expect(len(result)).To(Equal(7))

		Expect(result["pg_replication"].Query).To(Equal("SELECT CASE WHEN NOT pg_is_in_recovery() [...]"))
		Expect(result["pg_replication"].Primary).To(BeTrue())
		Expect(result["pg_replication"].Master).To(BeFalse()) // wokeignore:rule=master
		Expect(len(result["pg_replication"].Metrics)).To(Equal(1))
		Expect(len(result["pg_replication"].Metrics[0])).To(Equal(1))
		Expect(result["pg_replication"].Metrics[0]["lag"].Usage).To(Equal(ColumnUsage("GAUGE")))
		Expect(result["pg_replication"].Metrics[0]["lag"].Description).To(Equal(
			"Replication lag behind primary in seconds"))

		Expect(result["pg_postmaster"].Primary).To(BeFalse()) // wokeignore:rule=master
		Expect(result["pg_postmaster"].Master).To(BeTrue())   // wokeignore:rule=master
	})

	It("correctly handles YAML errors", func() {
		result, err := ParseQueries([]byte(`
test:
  toast
    bread`))
		Expect(err).ToNot(BeNil())
		Expect(result).To(BeNil())
	})
})

const pgExporterQueries = `
pg_replication:
  query: "SELECT CASE WHEN NOT pg_is_in_recovery() [...]"
  primary: true
  metrics:   # wokeignore:rule=master
    - lag:
        usage: "GAUGE"
        description: "Replication lag behind primary in seconds"

pg_postmaster:  # wokeignore:rule=master
  query: "SELECT pg_postmaster_start_time as start_time from pg_postmaster_start_time()"  # wokeignore:rule=master
  master: true  # wokeignore:rule=master
  metrics:
    - start_time:
        usage: "GAUGE"
        description: "Time at which postmaster started"  # wokeignore:rule=master

pg_stat_user_tables:
  query: |
   SELECT
     current_database() datname,
     schemaname,
     relname,
     seq_scan,
     seq_tup_read,
     idx_scan,
     idx_tup_fetch,
     n_tup_ins,
     n_tup_upd,
     n_tup_del,
     n_tup_hot_upd,
     n_live_tup,
     n_dead_tup,
     n_mod_since_analyze,
     COALESCE(last_vacuum, '1970-01-01Z') as last_vacuum,
     COALESCE(last_autovacuum, '1970-01-01Z') as last_autovacuum,
     COALESCE(last_analyze, '1970-01-01Z') as last_analyze,
     COALESCE(last_autoanalyze, '1970-01-01Z') as last_autoanalyze,
     vacuum_count,
     autovacuum_count,
     analyze_count,
     autoanalyze_count
   FROM
     pg_stat_user_tables
  metrics:
    - datname:
        usage: "LABEL"
        description: "Name of current database"
    - schemaname:
        usage: "LABEL"
        description: "Name of the schema that this table is in"
    - relname:
        usage: "LABEL"
        description: "Name of this table"
    - seq_scan:
        usage: "COUNTER"
        description: "Number of sequential scans initiated on this table"
    - seq_tup_read:
        usage: "COUNTER"
        description: "Number of live rows fetched by sequential scans"
    - idx_scan:
        usage: "COUNTER"
        description: "Number of index scans initiated on this table"
    - idx_tup_fetch:
        usage: "COUNTER"
        description: "Number of live rows fetched by index scans"
    - n_tup_ins:
        usage: "COUNTER"
        description: "Number of rows inserted"
    - n_tup_upd:
        usage: "COUNTER"
        description: "Number of rows updated"
    - n_tup_del:
        usage: "COUNTER"
        description: "Number of rows deleted"
    - n_tup_hot_upd:
        usage: "COUNTER"
        description: "Number of rows HOT updated (i.e., with no separate index update required)"
    - n_live_tup:
        usage: "GAUGE"
        description: "Estimated number of live rows"
    - n_dead_tup:
        usage: "GAUGE"
        description: "Estimated number of dead rows"
    - n_mod_since_analyze:
        usage: "GAUGE"
        description: "Estimated number of rows changed since last analyze"
    - last_vacuum:
        usage: "GAUGE"
        description: "Last time at which this table was manually vacuumed (not counting VACUUM FULL)"
    - last_autovacuum:
        usage: "GAUGE"
        description: "Last time at which this table was vacuumed by the autovacuum daemon"
    - last_analyze:
        usage: "GAUGE"
        description: "Last time at which this table was manually analyzed"
    - last_autoanalyze:
        usage: "GAUGE"
        description: "Last time at which this table was analyzed by the autovacuum daemon"
    - vacuum_count:
        usage: "COUNTER"
        description: "Number of times this table has been manually vacuumed (not counting VACUUM FULL)"
    - autovacuum_count:
        usage: "COUNTER"
        description: "Number of times this table has been vacuumed by the autovacuum daemon"
    - analyze_count:
        usage: "COUNTER"
        description: "Number of times this table has been manually analyzed"
    - autoanalyze_count:
        usage: "COUNTER"
        description: "Number of times this table has been analyzed by the autovacuum daemon"

pg_statio_user_tables:
  query: "SELECT current_database() datname, schemaname, relname, [...]"
  metrics:
    - datname:
        usage: "LABEL"
        description: "Name of current database"
    - schemaname:
        usage: "LABEL"
        description: "Name of the schema that this table is in"
    - relname:
        usage: "LABEL"
        description: "Name of this table"
    - heap_blks_read:
        usage: "COUNTER"
        description: "Number of disk blocks read from this table"
    - heap_blks_hit:
        usage: "COUNTER"
        description: "Number of buffer hits in this table"
    - idx_blks_read:
        usage: "COUNTER"
        description: "Number of disk blocks read from all indexes on this table"
    - idx_blks_hit:
        usage: "COUNTER"
        description: "Number of buffer hits in all indexes on this table"
    - toast_blks_read:
        usage: "COUNTER"
        description: "Number of disk blocks read from this table's TOAST table (if any)"
    - toast_blks_hit:
        usage: "COUNTER"
        description: "Number of buffer hits in this table's TOAST table (if any)"
    - tidx_blks_read:
        usage: "COUNTER"
        description: "Number of disk blocks read from this table's TOAST table indexes (if any)"
    - tidx_blks_hit:
        usage: "COUNTER"
        description: "Number of buffer hits in this table's TOAST table indexes (if any)"

pg_database:
  query: "SELECT pg_database.datname, pg_database_size(pg_database.datname) as size_bytes FROM pg_database"
  primary: true
  cache_seconds: 30
  metrics:
    - datname:
        usage: "LABEL"
        description: "Name of the database"
    - size_bytes:
        usage: "GAUGE"
        description: "Disk space used by the database"

pg_stat_statements:
  query: "SELECT t2.rolname, t3.datname, queryid, calls, [...]"
  primary: true
  metrics:
    - rolname:
        usage: "LABEL"
        description: "Name of user"
    - datname:
        usage: "LABEL"
        description: "Name of database"
    - queryid:
        usage: "LABEL"
        description: "Query ID"
    - calls:
        usage: "COUNTER"
        description: "Number of times executed"
    - total_time_seconds:
        usage: "COUNTER"
        description: "Total time spent in the statement, in milliseconds"
    - min_time_seconds:
        usage: "GAUGE"
        description: "Minimum time spent in the statement, in milliseconds"
    - max_time_seconds:
        usage: "GAUGE"
        description: "Maximum time spent in the statement, in milliseconds"
    - mean_time_seconds:
        usage: "GAUGE"
        description: "Mean time spent in the statement, in milliseconds"
    - stddev_time_seconds:
        usage: "GAUGE"
        description: "Population standard deviation of time spent in the statement, in milliseconds"
    - rows:
        usage: "COUNTER"
        description: "Total number of rows retrieved or affected by the statement"
    - shared_blks_hit:
        usage: "COUNTER"
        description: "Total number of shared block cache hits by the statement"
    - shared_blks_read:
        usage: "COUNTER"
        description: "Total number of shared blocks read by the statement"
    - shared_blks_dirtied:
        usage: "COUNTER"
        description: "Total number of shared blocks dirtied by the statement"
    - shared_blks_written:
        usage: "COUNTER"
        description: "Total number of shared blocks written by the statement"
    - local_blks_hit:
        usage: "COUNTER"
        description: "Total number of local block cache hits by the statement"
    - local_blks_read:
        usage: "COUNTER"
        description: "Total number of local blocks read by the statement"
    - local_blks_dirtied:
        usage: "COUNTER"
        description: "Total number of local blocks dirtied by the statement"
    - local_blks_written:
        usage: "COUNTER"
        description: "Total number of local blocks written by the statement"
    - temp_blks_read:
        usage: "COUNTER"
        description: "Total number of temp blocks read by the statement"
    - temp_blks_written:
        usage: "COUNTER"
        description: "Total number of temp blocks written by the statement"
    - blk_read_time_seconds:
        usage: "COUNTER"
        description: "Total time the statement spent reading blocks, in milliseconds"
    - blk_write_time_seconds:
        usage: "COUNTER"
        description: "Total time the statement spent writing blocks, in milliseconds"

pg_stat_activity:
  query: |
    WITH
      metrics AS (
        SELECT
          application_name,
          SUM(EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - state_change))::bigint)::float AS process_idle_seconds_sum,
          COUNT(*) AS process_idle_seconds_count
        FROM pg_stat_activity
        WHERE state = 'idle'
        GROUP BY application_name
      ),
      buckets AS (
        SELECT
          application_name,
          le,
          SUM(
            CASE WHEN EXTRACT(EPOCH FROM (CURRENT_TIMESTAMP - state_change)) <= le
              THEN 1
              ELSE 0
            END
          )::bigint AS bucket
        FROM
          pg_stat_activity,
          UNNEST(ARRAY[1, 2, 5, 15, 30, 60, 90, 120, 300]) AS le
        GROUP BY application_name, le
        ORDER BY application_name, le
      )
    SELECT
      application_name,
      process_idle_seconds_sum,
      process_idle_seconds_count,
      ARRAY_AGG(le) AS process_idle_seconds,
      ARRAY_AGG(bucket) AS process_idle_seconds_bucket
    FROM metrics JOIN buckets USING (application_name)
    GROUP BY 1, 2, 3
  metrics:
    - application_name:
        usage: "LABEL"
        description: "Application Name"
    - process_idle_seconds:
        usage: "HISTOGRAM"
        description: "Idle time of server processes"
`
