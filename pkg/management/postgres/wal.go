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
	"database/sql"
	"errors"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
)

var errNoWalArchivePresent = errors.New("no wal-archive present")

// ensureWalArchiveIsWorking behaves slightly differently when executed on primary or a standby.
// On primary, it could run even before the first WAL has completed. For this reason it
// could require a WAL switch, to quicken the check.
// On standby, the mere existence of the standby guarantees that a WAL file has already been generated
// by the pg_basebackup used to prime the standby data directory, so we check only if the WAL
// archive process is not failing.
func ensureWalArchiveIsWorking(instance *Instance) error {
	isPrimary, err := instance.IsPrimary()
	if err != nil {
		return err
	}

	if isPrimary {
		return newWalArchiveBootstrapperForPrimary().ensureFirstWalArchived(instance, retryUntilWalArchiveWorking)
	}

	return newWalArchiveAnalyzerForReplicaInstance(instance.GetPrimaryConnInfo()).
		mustHaveFirstWalArchivedWithBackoff(retryUntilWalArchiveWorking)
}

// walArchiveAnalyzer represents an object that can check for the status of
// WAL archiving, in primary or replicas
// Depending on primary vs. replicas, the DB connection needed is different
type walArchiveAnalyzer struct {
	dbFactory func() (*sql.DB, error)
}

func newWalArchiveAnalyzerForReplicaInstance(primaryConnInfo string) *walArchiveAnalyzer {
	return &walArchiveAnalyzer{
		dbFactory: func() (*sql.DB, error) {
			db, openErr := sql.Open(
				"pgx",
				primaryConnInfo,
			)
			if openErr != nil {
				log.Error(openErr, "can not open postgres database")
				return nil, openErr
			}
			return db, nil
		},
	}
}

func (w *walArchiveAnalyzer) mustHaveFirstWalArchivedWithBackoff(backoff wait.Backoff) error {
	return retry.OnError(backoff, resources.RetryAlways, func() error {
		db, err := w.dbFactory()
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := db.Close(); closeErr != nil {
				log.Debug("Error while closing connection", "err", closeErr.Error())
			}
		}()
		return w.mustHaveFirstWalArchived(db)
	})
}

func (w *walArchiveAnalyzer) mustHaveFirstWalArchived(db *sql.DB) error {
	row := db.QueryRow("SELECT COALESCE(last_archived_time,'-infinity') > " +
		"COALESCE(last_failed_time, '-infinity') AS is_archiving, last_failed_time IS NOT NULL " +
		"FROM pg_catalog.pg_stat_archiver")

	var walArchivingWorking, lastFailedTimePresent bool

	if err := row.Scan(&walArchivingWorking, &lastFailedTimePresent); err != nil {
		log.Error(err, "can't get WAL archiving status")
		return err
	}

	if walArchivingWorking {
		log.Info("WAL archiving is working")
		return nil
	}

	if lastFailedTimePresent {
		log.Info("WAL archiving is not working")
		return errors.New("wal-archive not working")
	}

	return errNoWalArchivePresent
}

// walArchiveBootstrapper is a walArchiveAnalyzer that may create the first
// WAL, in case it is not there yet
// NOTE: this requires that the underlying walArchiveAnalyzer have a DB connection
// pointing to the primary, with privileges to issue a CHECKPOINT
type walArchiveBootstrapper struct {
	walArchiveAnalyzer
	firstWalShipped bool
}

func newWalArchiveBootstrapperForPrimary() *walArchiveBootstrapper {
	return &walArchiveBootstrapper{
		walArchiveAnalyzer: walArchiveAnalyzer{
			dbFactory: func() (*sql.DB, error) {
				db, openErr := sql.Open(
					"pgx",
					fmt.Sprintf("host=%s port=%v dbname=postgres user=postgres sslmode=disable",
						GetSocketDir(),
						GetServerPort(),
					),
				)
				if openErr != nil {
					log.Error(openErr, "can not open postgres database")
					return nil, openErr
				}
				return db, nil
			},
		},
	}
}

var errPrimaryDemoted = errors.New("primary was demoted while waiting for the first wal-archive")

func (w *walArchiveBootstrapper) ensureFirstWalArchived(instance *Instance, backoff wait.Backoff) error {
	return retry.OnError(backoff, func(err error) bool { return !errors.Is(err, errPrimaryDemoted) }, func() error {
		isPrimary, err := instance.IsPrimary()
		if err != nil {
			return fmt.Errorf("error checking primary: %w", err)
		}
		if !isPrimary {
			return errPrimaryDemoted
		}

		db, err := w.dbFactory()
		if err != nil {
			return err
		}
		defer func() {
			if closeErr := db.Close(); closeErr != nil {
				log.Debug("Error while closing connection", "err", closeErr.Error())
			}
		}()

		err = w.mustHaveFirstWalArchived(db)
		if err == nil {
			return nil
		}
		if !errors.Is(err, errNoWalArchivePresent) {
			return err
		}

		if w.firstWalShipped {
			return errors.New("waiting for first wal-archive")
		}

		log.Info("Triggering the first WAL file to be archived")
		if err := w.shipWalFile(db); err != nil {
			return err
		}

		w.firstWalShipped = true

		return errors.New("first wal-archive triggered")
	})
}

func (w *walArchiveBootstrapper) shipWalFile(db *sql.DB) error {
	if _, err := db.Exec("CHECKPOINT"); err != nil {
		return fmt.Errorf("error while requiring a checkpoint: %w", err)
	}

	if _, err := db.Exec("SELECT pg_catalog.pg_switch_wal()"); err != nil {
		return fmt.Errorf("error while switching to a new WAL: %w", err)
	}

	return nil
}
