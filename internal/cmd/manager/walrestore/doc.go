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

// Package walrestore implements the wal-restore command, which PostgreSQL
// invokes as restore_command to fetch WAL segments from the archive during
// recovery.
//
// # PostgreSQL recovery state machine
//
// PostgreSQL's recovery loop (WaitForWALToBecomeAvailable in
// src/backend/access/transam/xlogrecovery.c) tries WAL sources in order:
// ARCHIVE (restore_command), PG_WAL, and — only in standby mode — STREAM
// (walreceiver). When streaming disconnects, walreceiver is shut down and the
// state machine falls back to ARCHIVE. In non-standby mode an archive failure
// returns XLREAD_FAIL immediately, with no streaming fallback.
//
// # restore_command exit code semantics
//
// RestoreArchivedFile in src/backend/access/transam/xlogarchive.c interprets
// the exit status as follows:
//
//   - Exit 0: segment restored successfully.
//   - Any other normal exit (e.g. exit 1): segment not in the archive.
//     PostgreSQL logs a DEBUG2 message and tries the next source.
//     wait_result_is_any_signal(rc, true) returns false for a normally-exiting
//     process, so no FATAL is raised.
//   - Exit 128+N (shell signal convention): wait_result_is_any_signal checks
//     whether WIFEXITED && exit_status == 128+signum. For SIGTERM (128+15=143)
//     the earlier check wait_result_is_signal(rc, SIGTERM) fires first and
//     calls proc_exit(1) — a clean PostgreSQL shutdown. For other signals
//     (SIGINT=130, SIGQUIT=131, …) wait_result_is_any_signal returns true and
//     PostgreSQL raises FATAL.
//
// # WAL gaps during primary transition
//
// In standby mode, exit 1 on a transient error is harmless: PostgreSQL falls
// back to streaming and returns to the archive when streaming disconnects.
//
// In non-standby mode the picture is entirely different. XLREAD_FAIL does not
// crash PostgreSQL — it causes PostgreSQL to consider recovery complete,
// reach a consistent state from the base backup, and open as primary. The
// instance manager sees a healthy primary and proceeds normally. Any WAL that
// existed in the archive but was not applied is silently lost.
//
// This affects two scenarios:
//
//  1. Post-promotion archive draining: when CNPG promotes a replica, PostgreSQL
//     leaves standby mode and drains remaining WAL from the archive before
//     opening for writes. A transient restore_command failure at this point
//     causes the new primary to open with a gap relative to the old primary's
//     committed transactions.
//
//  2. Bootstrap from backup: a standalone cluster bootstrapped via
//     spec.bootstrap.recovery replays WAL from the archive to reach either a
//     specific recovery target (PITR) or the latest available WAL. In both
//     cases PostgreSQL is not in standby mode. PITR and "recover to latest"
//     do not differ in outcome — any transient failure leaves WAL in the
//     archive unapplied and PostgreSQL opens as primary without it. The only
//     practical difference is that longer recoveries raise the probability of
//     encountering a transient error, not the severity of the outcome.
//
// The steady-state designated primary of a replica cluster is not affected
// either: it replicates from an external primary and runs in standby mode, so
// a restore_command failure falls back to streaming rather than triggering
// XLREAD_FAIL. Note that the first-pod bootstrap of a replica cluster *is*
// affected — it replays the base backup in non-standby mode before
// transitioning into standby, which is why the retry loop still covers the
// bootstrap case for replica clusters (see below).
//
// # Retry loop
//
// To prevent the silent data-loss outcome, this package retries transient
// restore_command failures instead of returning immediately to PostgreSQL.
// The retry loop applies in two situations, which together cover the
// dangerous scenarios above and exclude normal standby replicas:
//
//   - Bootstrap (cluster.Status.CurrentPrimary == ""): the first pod is
//     replaying a base backup in non-standby mode, regardless of whether the
//     cluster is standard or a replica cluster. setPrimaryInstance
//     (cluster_create.go) sets TargetPrimary before the restore job is
//     created, well before the PostgreSQL pod starts calling restore_command.
//   - Designated primary of a non-replica cluster
//     (cluster.Status.TargetPrimary == podName && !cluster.IsReplica()):
//     this pod is draining the archive in preparation for promotion. CNPG
//     sets TargetPrimary to the promoted pod before the instance manager
//     triggers promotion, so the cache is consistent by the time WAL recovery
//     begins.
//
// The designated primary of a replica cluster in steady state is deliberately
// excluded: it never leaves standby mode under PostgreSQL's own logic, so
// exit 1 from restore_command does not risk XLREAD_FAIL — streaming
// replication from the external source is the natural fallback.
//
// When the retry budget is exhausted we exit with code 143 (128+SIGTERM).
// PostgreSQL detects this as SIGTERM and calls proc_exit(1) — a clean
// shutdown. The instance manager restarts PostgreSQL and recovery resumes from
// its last checkpoint, which is a retry at the process level. Returning exit 1
// instead would trigger XLREAD_FAIL and the silent open-as-primary outcome
// described above.
package walrestore
