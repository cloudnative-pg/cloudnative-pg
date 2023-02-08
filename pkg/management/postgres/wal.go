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

package postgres

import (
	"database/sql"
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
)

var errNoWalArchivePresent = errors.New("no wal-archive present")

type walArchiveAnalyzer struct {
	dbFactory func() (*sql.DB, error)
}

// ensureWalArchiveIsWorking behave slightly differently when executed on primary or a standby.
// On primary, it could run very early, when the first WAL has never completed. For this reason it
// could require a WAL switch, to quicken the check.
// On standby, the mere existence of the standby guarantee that a WAL file has already been generated
// by the pg_basebakup used to prime the standby data directory, so we check only if the WAL
// archive process is not failing.
func ensureWalArchiveIsWorking(instance *Instance) error {
	isPrimary, err := instance.IsPrimary()
	if err != nil {
		return err
	}

	if isPrimary {
		return newWalArchiveBootstrapperForPrimary().ensureFirstWalArchived(retryUntilWalArchiveWorking)
	}

	return newWalArchiveAnalyzerForReplicaInstance(instance.GetPrimaryConnInfo()).
		mustHaveFirstWalArchivedWithBackoff(retryUntilWalArchiveWorking)
}

func newWalArchiveAnalyzerForReplicaInstance(primaryConnInfo string) *walArchiveAnalyzer {
	return &walArchiveAnalyzer{
		dbFactory: func() (*sql.DB, error) {
			db, openErr := sql.Open(
				"pgx",
				fmt.Sprintf("%s dbname=%s", primaryConnInfo, "postgres"),
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
		"FROM pg_stat_archiver")

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

type walArchiveBootstrapper struct {
	walArchiveAnalyzer
	createdFirstWal bool
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

func (w *walArchiveBootstrapper) ensureFirstWalArchived(backoff wait.Backoff) error {
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

		err = w.mustHaveFirstWalArchived(db)
		if !errors.Is(err, errNoWalArchivePresent) {
			return err
		}

		if w.createdFirstWal {
			return errors.New("waiting for first wal-archive")
		}

		if walArchiveErr := w.triggerFirstWalArchive(db); walArchiveErr != nil {
			return walArchiveErr
		}

		return errors.New("first wal-archive triggered")
	})
}

func (w *walArchiveBootstrapper) triggerFirstWalArchive(db *sql.DB) error {
	log.Info("Triggering the first WAL file to be archived")
	if _, err := db.Exec("CHECKPOINT"); err != nil {
		return fmt.Errorf("error while requiring a checkpoint: %w", err)
	}

	if _, err := db.Exec("SELECT pg_switch_wal()"); err != nil {
		return fmt.Errorf("error while switching to a new WAL: %w", err)
	}

	return nil
}
