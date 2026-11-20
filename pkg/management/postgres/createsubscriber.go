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

package postgres

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/execlog"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/jackc/pgx/v5"
	"github.com/lib/pq"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

const (
	// CreateSubscriberMarkerFile is written inside PGDATA once the conversion
	// into a logical subscriber is complete. Its presence makes the bootstrap
	// job skip the conversion when it is retried.
	CreateSubscriberMarkerFile = "cnpg_createsubscriber.done"

	// CreateSubscriberEngineNative uses the pg_createsubscriber binary (PG17+)
	CreateSubscriberEngineNative = "native"
	// CreateSubscriberEngineEmulated reproduces the pg_createsubscriber algorithm (PG14)
	CreateSubscriberEngineEmulated = "emulated"

	// createSubscriberNativePort is the port pg_createsubscriber uses for the
	// temporary server; it only listens on the local socket
	createSubscriberNativePort = "50432"

	pgCreateSubscriberName = "pg_createsubscriber"
	pgResetWalName         = "pg_resetwal"
)

// CreateSubscriberDatabase is one database converted into a subscription.
// Publication, subscription and logical slot share the same name.
type CreateSubscriberDatabase struct {
	Name   string `json:"name"`
	OID    uint32 `json:"oid"`
	Object string `json:"object"`
}

// CreateSubscriberPlan holds everything the conversion needs
type CreateSubscriberPlan struct {
	Engine          string
	PhysicalSlot    string
	ObjectPrefix    string
	Databases       []CreateSubscriberDatabase
	RecoveryTimeout time.Duration

	// SourceConnString returns the libpq connection string to the source for
	// the given database, without any secret in it (the password is in a passfile)
	SourceConnString func(database string) string
}

// CreateSubscriberMarker is the content of CreateSubscriberMarkerFile
type CreateSubscriberMarker struct {
	Engine                 string                     `json:"engine"`
	Major                  int                        `json:"major"`
	ConsistentLSN          string                     `json:"consistentLSN"`
	PromotedLSN            string                     `json:"promotedLSN,omitempty"`
	SourceSystemIdentifier string                     `json:"sourceSystemIdentifier"`
	SystemIdentifier       string                     `json:"systemIdentifier"`
	PhysicalSlot           string                     `json:"physicalSlot"`
	Databases              []CreateSubscriberDatabase `json:"databases"`
	CompletedAt            time.Time                  `json:"completedAt"`
}

// ObjectName returns the publication/subscription/slot name for a database OID
func (plan *CreateSubscriberPlan) ObjectName(oid uint32) string {
	return plan.ObjectPrefix + strconv.FormatUint(uint64(oid), 10)
}

// OpenSource opens a connection to the given database of the source
func (plan *CreateSubscriberPlan) OpenSource(database string) (*sql.DB, error) {
	return pool.NewDBConnection(plan.SourceConnString(database), pool.ConnectionProfilePostgresql)
}

// forEachSourceDatabase opens a connection to every planned database on the source
func (plan *CreateSubscriberPlan) forEachSourceDatabase(
	ctx context.Context,
	fn func(ctx context.Context, db *sql.DB, target CreateSubscriberDatabase) error,
) error {
	for _, target := range plan.Databases {
		db, err := plan.OpenSource(target.Name)
		if err != nil {
			return fmt.Errorf("while connecting to source database %q: %w", target.Name, err)
		}
		err = fn(ctx, db, target)
		_ = db.Close()
		if err != nil {
			return fmt.Errorf("source database %q: %w", target.Name, err)
		}
	}
	return nil
}

// ResolveSourceDatabases looks up the OID of every requested database on the
// source and fills plan.Databases. The databases must accept connections.
func (plan *CreateSubscriberPlan) ResolveSourceDatabases(ctx context.Context, admin *sql.DB, names []string) error {
	if len(names) == 0 {
		rows, err := admin.QueryContext(ctx,
			"SELECT datname FROM pg_catalog.pg_database "+
				"WHERE datallowconn AND NOT datistemplate ORDER BY oid")
		if err != nil {
			return err
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				_ = rows.Close()
				return err
			}
			names = append(names, name)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}

	plan.Databases = plan.Databases[:0]
	for _, name := range names {
		var oid uint32
		var allowConn bool
		err := admin.QueryRowContext(ctx,
			"SELECT oid, datallowconn FROM pg_catalog.pg_database WHERE datname = $1", name).
			Scan(&oid, &allowConn)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("database %q does not exist on the source", name)
		}
		if err != nil {
			return err
		}
		if !allowConn {
			return fmt.Errorf("database %q does not accept connections on the source", name)
		}
		plan.Databases = append(plan.Databases, CreateSubscriberDatabase{
			Name:   name,
			OID:    oid,
			Object: plan.ObjectName(oid),
		})
	}
	return nil
}

// SourcePreflight checks that the source can be converted from
func (plan *CreateSubscriberPlan) SourcePreflight(ctx context.Context, admin *sql.DB, targetMajor int) error {
	var versionNum int
	var walLevel string
	var isSuper bool
	var freeSlots, freeSenders int
	err := admin.QueryRowContext(ctx, `
SELECT current_setting('server_version_num')::int,
       current_setting('wal_level'),
       (SELECT rolsuper FROM pg_catalog.pg_roles WHERE rolname = current_user),
       current_setting('max_replication_slots')::int
         - (SELECT count(*) FROM pg_catalog.pg_replication_slots),
       current_setting('max_wal_senders')::int
         - (SELECT count(*) FROM pg_catalog.pg_stat_replication)`).
		Scan(&versionNum, &walLevel, &isSuper, &freeSlots, &freeSenders)
	if err != nil {
		return fmt.Errorf("while reading source settings: %w", err)
	}

	var problems []string
	if sourceMajor := versionNum / 10000; sourceMajor != targetMajor {
		problems = append(problems,
			fmt.Sprintf("source is PostgreSQL %d but the target image is %d", sourceMajor, targetMajor))
	}
	if walLevel != "logical" {
		problems = append(problems, fmt.Sprintf("source wal_level is %q, 'logical' is required", walLevel))
	}
	if !isSuper {
		problems = append(problems, "the source user must be a superuser")
	}
	// One logical slot per database, plus the physical slot of the clone
	if needed := len(plan.Databases) + 1; freeSlots < needed {
		problems = append(problems,
			fmt.Sprintf("source has %d free replication slots, %d needed", freeSlots, needed))
	}
	// pg_basebackup -X stream uses two walsenders
	if freeSenders < 2 {
		problems = append(problems,
			fmt.Sprintf("source has %d free walsenders, 2 needed", freeSenders))
	}
	if len(problems) > 0 {
		return fmt.Errorf("source preflight failed: %s", strings.Join(problems, "; "))
	}
	return nil
}

// dropInactiveSlot drops a replication slot unless it is in use.
// Logical slots must be dropped from the database they belong to.
func dropInactiveSlot(ctx context.Context, db *sql.DB, slot string) error {
	var active sql.NullBool
	err := db.QueryRowContext(ctx,
		"SELECT active FROM pg_catalog.pg_replication_slots WHERE slot_name = $1", slot).Scan(&active)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if active.Bool {
		return fmt.Errorf("replication slot %q is still active", slot)
	}
	_, err = db.ExecContext(ctx, "SELECT pg_catalog.pg_drop_replication_slot($1)", slot)
	return err
}

// CleanupSourceObjects drops the publications and slots a previous attempt
// may have left on the source. Only objects named after this Cluster are touched.
func (plan *CreateSubscriberPlan) CleanupSourceObjects(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)
	err := plan.forEachSourceDatabase(ctx, func(ctx context.Context, db *sql.DB, target CreateSubscriberDatabase) error {
		if err := dropInactiveSlot(ctx, db, target.Object); err != nil {
			return err
		}
		_, err := db.ExecContext(ctx,
			fmt.Sprintf("DROP PUBLICATION IF EXISTS %s", pgx.Identifier{target.Object}.Sanitize()))
		return err
	})
	if err != nil {
		return err
	}
	contextLogger.Info("Cleaned up leftover objects on the source", "databases", plan.Databases)
	return plan.DropPhysicalSlot(ctx)
}

// DropPhysicalSlot drops the physical slot used by the clone, waiting for the
// walsender of a just promoted target to go away
func (plan *CreateSubscriberPlan) DropPhysicalSlot(ctx context.Context) error {
	db, err := plan.OpenSource(plan.Databases[0].Name)
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()

	deadline := time.Now().Add(time.Minute)
	for {
		err = dropInactiveSlot(ctx, db, plan.PhysicalSlot)
		if err == nil || time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// CreatePublicationsAndSlots creates, for every database, a publication FOR ALL
// TABLES and a pgoutput logical slot on the source. It returns the consistent
// point of the last slot: every transaction committed after it is decoded by
// the slots, every transaction committed before it is part of the target.
func (plan *CreateSubscriberPlan) CreatePublicationsAndSlots(ctx context.Context) (string, error) {
	var consistentLSN string
	err := plan.forEachSourceDatabase(ctx, func(ctx context.Context, db *sql.DB, target CreateSubscriberDatabase) error {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES",
			pgx.Identifier{target.Object}.Sanitize())); err != nil {
			return fmt.Errorf("while creating publication: %w", err)
		}
		// Waits for the transactions running on the source to end
		var lsn string
		if err := db.QueryRowContext(ctx,
			"SELECT lsn FROM pg_catalog.pg_create_logical_replication_slot($1, 'pgoutput')",
			target.Object).Scan(&lsn); err != nil {
			return fmt.Errorf("while creating logical replication slot: %w", err)
		}
		log.FromContext(ctx).Info("Created publication and logical slot on the source",
			"database", target.Name, "object", target.Object, "lsn", lsn)
		later, err := LSNGreater(lsn, consistentLSN)
		if err != nil {
			return err
		}
		if later {
			consistentLSN = lsn
		}
		return nil
	})
	if err != nil {
		return "", err
	}

	// recovery_target_lsn with inclusive=true stops after the first record at or
	// beyond the target: make sure one exists even when the source is idle
	db, err := plan.OpenSource(plan.Databases[0].Name)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = db.Close()
	}()
	if _, err := db.ExecContext(ctx,
		"SELECT pg_catalog.pg_logical_emit_message(false, 'cnpg_createsubscriber', 'consistent point')"); err != nil {
		return "", fmt.Errorf("while writing a WAL record past the consistent point: %w", err)
	}
	return consistentLSN, nil
}

// SourceSystemIdentifier reads the system identifier of the source
func (plan *CreateSubscriberPlan) SourceSystemIdentifier(ctx context.Context) (string, error) {
	db, err := plan.OpenSource(plan.Databases[0].Name)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = db.Close()
	}()
	return SystemIdentifier(ctx, db)
}

// SystemIdentifier reads the system identifier of the connected server
func SystemIdentifier(ctx context.Context, db *sql.DB) (string, error) {
	var id string
	err := db.QueryRowContext(ctx,
		"SELECT system_identifier::text FROM pg_catalog.pg_control_system()").Scan(&id)
	return id, err
}

// WaitForStandbyStreaming waits until the standby streams from the source
func WaitForStandbyStreaming(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		var status sql.NullString
		err := db.QueryRowContext(ctx, "SELECT status FROM pg_catalog.pg_stat_wal_receiver").Scan(&status)
		if err == nil && status.String == "streaming" {
			return nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the target is not streaming from the source after %v", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// WaitForPromotion waits until the target reaches the recovery target and promotes
func WaitForPromotion(ctx context.Context, db *sql.DB, timeout time.Duration) error {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		var inRecovery bool
		if err := db.QueryRowContext(ctx, "SELECT pg_catalog.pg_is_in_recovery()").Scan(&inRecovery); err != nil {
			return err
		}
		if !inRecovery {
			return nil
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return fmt.Errorf("the target did not reach the recovery target within %v", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// PromotionSwitchPoint returns the LSN where the promoted target left the
// timeline of the source: the end of the last WAL record it replayed. Every
// transaction whose commit record starts before this point is in the target,
// every other one is still to be applied.
func PromotionSwitchPoint(ctx context.Context, db *sql.DB, pgData string) (string, error) {
	var walFile string
	if err := db.QueryRowContext(ctx,
		"SELECT pg_catalog.pg_walfile_name(pg_catalog.pg_current_wal_lsn())").Scan(&walFile); err != nil {
		return "", err
	}
	if len(walFile) < 8 {
		return "", fmt.Errorf("unexpected WAL file name %q", walFile)
	}
	historyFile := filepath.Join(pgData, pgWalDirectory, walFile[:8]+".history")
	content, err := os.ReadFile(historyFile) // #nosec G304
	if err != nil {
		return "", fmt.Errorf("while reading the timeline history: %w", err)
	}
	return parseHistorySwitchPoint(string(content))
}

// parseHistorySwitchPoint extracts the switch point of the last line of a
// timeline history file ("<parent tli>\t<switch point>\t<reason>")
func parseHistorySwitchPoint(content string) (string, error) {
	var switchPoint string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", fmt.Errorf("invalid timeline history line %q", line)
		}
		switchPoint = fields[1]
	}
	if switchPoint == "" {
		return "", fmt.Errorf("empty timeline history")
	}
	if _, err := ParseLSN(switchPoint); err != nil {
		return "", err
	}
	return switchPoint, nil
}

// DropInheritedLogicalObjects removes, from every database of the target, the
// publications and the subscriptions copied from the source. A foreign
// subscription is detached from its slot before being dropped: dropping it
// directly would drop the slot on its publisher, which is still in use there.
func (plan *CreateSubscriberPlan) DropInheritedLogicalObjects(ctx context.Context, instance *Instance) error {
	contextLogger := log.FromContext(ctx)
	superUser, err := instance.GetSuperUserDB()
	if err != nil {
		return err
	}
	databases, err := listConnectableDatabases(ctx, superUser)
	if err != nil {
		return err
	}

	for _, database := range databases {
		db, err := instance.ConnectionPool().Connection(database)
		if err != nil {
			return err
		}
		publications, err := queryStrings(ctx, db, "SELECT pubname FROM pg_catalog.pg_publication")
		if err != nil {
			return err
		}
		for _, pub := range publications {
			if _, err := db.ExecContext(ctx,
				fmt.Sprintf("DROP PUBLICATION %s", pgx.Identifier{pub}.Sanitize())); err != nil {
				return fmt.Errorf("while dropping inherited publication %q in %q: %w", pub, database, err)
			}
			contextLogger.Info("Dropped inherited publication", "database", database, "publication", pub)
		}

		subscriptions, err := queryStrings(ctx, db, `
SELECT s.subname FROM pg_catalog.pg_subscription s
JOIN pg_catalog.pg_database d ON d.oid = s.subdbid
WHERE d.datname = current_database() AND NOT starts_with(s.subname, $1)`, plan.ObjectPrefix)
		if err != nil {
			return err
		}
		for _, sub := range subscriptions {
			name := pgx.Identifier{sub}.Sanitize()
			for _, stmt := range []string{
				fmt.Sprintf("ALTER SUBSCRIPTION %s DISABLE", name),
				fmt.Sprintf("ALTER SUBSCRIPTION %s SET (slot_name = NONE)", name),
				fmt.Sprintf("DROP SUBSCRIPTION %s", name),
			} {
				if _, err := db.ExecContext(ctx, stmt); err != nil {
					return fmt.Errorf("while dropping inherited subscription %q in %q: %w", sub, database, err)
				}
			}
			contextLogger.Info("Dropped inherited subscription", "database", database, "subscription", sub)
		}
	}
	return nil
}

// CreateSubscriptions creates, enables and positions at startLSN one
// subscription per database, reusing the slots created on the source
func (plan *CreateSubscriberPlan) CreateSubscriptions(ctx context.Context, instance *Instance, startLSN string) error {
	for _, target := range plan.Databases {
		db, err := instance.ConnectionPool().Connection(target.Name)
		if err != nil {
			return err
		}
		name := pgx.Identifier{target.Object}.Sanitize()
		create := fmt.Sprintf(
			"CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s "+
				"WITH (enabled = false, create_slot = false, copy_data = false, slot_name = %s)",
			name, pq.QuoteLiteral(plan.SourceConnString(target.Name)), name, pq.QuoteLiteral(target.Object))
		if _, err := db.ExecContext(ctx, create); err != nil {
			return fmt.Errorf("while creating subscription in %q: %w", target.Name, err)
		}

		var subOID uint32
		if err := db.QueryRowContext(ctx,
			"SELECT oid FROM pg_catalog.pg_subscription WHERE subname = $1", target.Object).Scan(&subOID); err != nil {
			return err
		}
		if _, err := db.ExecContext(ctx, "SELECT pg_catalog.pg_replication_origin_advance($1, $2::pg_lsn)",
			fmt.Sprintf("pg_%d", subOID), startLSN); err != nil {
			return fmt.Errorf("while advancing the replication origin in %q: %w", target.Name, err)
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("ALTER SUBSCRIPTION %s ENABLE", name)); err != nil {
			return err
		}
		log.FromContext(ctx).Info("Created subscription",
			"database", target.Name, "subscription", target.Object, "startLSN", startLSN)
	}
	return nil
}

// VerifySubscriptions checks that every planned subscription exists and is
// enabled. It is meant to run against the converted instance.
func (plan *CreateSubscriberPlan) VerifySubscriptions(ctx context.Context, instance *Instance) error {
	for _, target := range plan.Databases {
		db, err := instance.ConnectionPool().Connection(target.Name)
		if err != nil {
			return err
		}
		var enabled bool
		err = db.QueryRowContext(ctx,
			"SELECT subenabled FROM pg_catalog.pg_subscription WHERE subname = $1", target.Object).Scan(&enabled)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("subscription %q is missing in %q", target.Object, target.Name)
		}
		if err != nil {
			return err
		}
		if !enabled {
			return fmt.Errorf("subscription %q in %q is disabled", target.Object, target.Name)
		}
	}
	return nil
}

func listConnectableDatabases(ctx context.Context, db *sql.DB) ([]string, error) {
	return queryStrings(ctx, db,
		"SELECT datname FROM pg_catalog.pg_database WHERE datallowconn AND NOT datistemplate")
}

func queryStrings(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = rows.Close()
	}()
	var result []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, rows.Err()
}

// ChangeSystemIdentifier gives a stopped PGDATA a new system identifier and
// resets its WAL, so that the WAL written from now on carries the new
// identifier. This is what pg_createsubscriber does as its last step.
func ChangeSystemIdentifier(ctx context.Context, pgData string, id uint64) error {
	controlFile, err := ReadControlFile(pgData)
	if err != nil {
		return fmt.Errorf("while reading pg_control: %w", err)
	}
	controlFile.SetSystemIdentifier(id)
	if err := controlFile.Write(pgData); err != nil {
		return fmt.Errorf("while writing pg_control: %w", err)
	}

	cmd := exec.CommandContext(ctx, pgResetWalName, "-D", pgData) // #nosec G204
	cmd.Env = append(os.Environ(), "LANG=C", "LC_MESSAGES=C")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_resetwal failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// PgCreateSubscriberArgs builds the command line of the native engine
func (plan *CreateSubscriberPlan) PgCreateSubscriberArgs(pgData, socketDir string) []string {
	args := []string{
		"--pgdata", pgData,
		"--publisher-server", plan.SourceConnString(plan.Databases[0].Name),
		"--socketdir", socketDir,
		"--subscriber-port", createSubscriberNativePort,
		"--subscriber-username", "postgres",
		"--recovery-timeout", strconv.Itoa(int(plan.RecoveryTimeout.Seconds())),
		"--verbose",
	}
	for _, target := range plan.Databases {
		args = append(args,
			"--database", target.Name,
			"--publication", target.Object,
			"--subscription", target.Object,
			"--replication-slot", target.Object,
		)
	}
	return args
}

// RunPgCreateSubscriber runs the native engine on a stopped standby PGDATA
func (plan *CreateSubscriberPlan) RunPgCreateSubscriber(ctx context.Context, pgData, socketDir string) error {
	cmd := exec.CommandContext(ctx, pgCreateSubscriberName, plan.PgCreateSubscriberArgs(pgData, socketDir)...) // #nosec G204
	return execlog.RunStreaming(cmd, pgCreateSubscriberName)
}

// RemoveRecoverySettings deletes from the given configuration file the
// recovery and replication settings a conversion may leave behind. If they
// stayed, the next time CNPG demotes this instance it would stop replaying at
// the wrong place or follow the source again.
func RemoveRecoverySettings(file string) error {
	content, err := os.ReadFile(file) // #nosec G304
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	var kept []string
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := scanner.Text()
		key := strings.TrimSpace(line)
		if idx := strings.IndexAny(key, " \t="); idx >= 0 {
			key = key[:idx]
		}
		if strings.HasPrefix(key, "recovery_target") ||
			key == "primary_conninfo" || key == "primary_slot_name" {
			continue
		}
		kept = append(kept, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	result := strings.Join(kept, "\n")
	if len(kept) > 0 {
		result += "\n"
	}
	return os.WriteFile(file, []byte(result), 0o600)
}

// WriteCreateSubscriberMarker atomically writes the completion marker
func WriteCreateSubscriberMarker(pgData string, marker CreateSubscriberMarker) error {
	content, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(pgData, CreateSubscriberMarkerFile)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// ReadCreateSubscriberMarker reads the completion marker, returning nil when
// the conversion never completed on this PGDATA
func ReadCreateSubscriberMarker(pgData string) (*CreateSubscriberMarker, error) {
	content, err := os.ReadFile(filepath.Join(pgData, CreateSubscriberMarkerFile)) // #nosec G304
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var marker CreateSubscriberMarker
	if err := json.Unmarshal(content, &marker); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", CreateSubscriberMarkerFile, err)
	}
	return &marker, nil
}

// ParseLSN converts a textual LSN (X/Y) into a number
func ParseLSN(lsn string) (uint64, error) {
	high, low, ok := strings.Cut(lsn, "/")
	if !ok {
		return 0, fmt.Errorf("invalid LSN %q", lsn)
	}
	h, err := strconv.ParseUint(high, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LSN %q: %w", lsn, err)
	}
	l, err := strconv.ParseUint(low, 16, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid LSN %q: %w", lsn, err)
	}
	return h<<32 | l, nil
}

// LSNGreater reports whether a is after b. An empty b is before everything.
func LSNGreater(a, b string) (bool, error) {
	if b == "" {
		return true, nil
	}
	av, err := ParseLSN(a)
	if err != nil {
		return false, err
	}
	bv, err := ParseLSN(b)
	if err != nil {
		return false, err
	}
	return av > bv, nil
}
