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

type dbProvider func() (*sql.DB, error)

type walArchiveBootstrapper struct {
	firstWalArchiveTriggered bool
	backoff                  *wait.Backoff
	dbProviderFunc           dbProvider
}

func newWalArchiveBootstrapper() *walArchiveBootstrapper {
	return &walArchiveBootstrapper{}
}

func (w *walArchiveBootstrapper) withTimeout(backoff *wait.Backoff) *walArchiveBootstrapper {
	w.backoff = backoff
	return w
}

func (w *walArchiveBootstrapper) withDBProvider(provider dbProvider) *walArchiveBootstrapper {
	w.dbProviderFunc = provider
	return w
}

func (w *walArchiveBootstrapper) withInstanceDBProvider(instance *Instance) *walArchiveBootstrapper {
	return w.withDBProvider(func() (*sql.DB, error) {
		db, openErr := sql.Open(
			"pgx",
			fmt.Sprintf("%s dbname=%s", instance.GetPrimaryConnInfo(), "postgres"),
		)
		if openErr != nil {
			log.Error(openErr, "can not open postgres database")
			return nil, openErr
		}
		return db, nil
	})
}

func (w *walArchiveBootstrapper) execute() error {
	if w.backoff == nil {
		return w.tryBootstrapWal()
	}

	return retry.OnError(*w.backoff, resources.RetryAlways, func() error {
		return w.tryBootstrapWal()
	})
}

func (w *walArchiveBootstrapper) tryBootstrapWal() error {
	db, err := w.dbProviderFunc()
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			log.Error(closeErr, "Error while closing connection")
		}
	}()

	row := db.QueryRow("SELECT COALESCE(last_archived_time,'-infinity') > " +
		"COALESCE(last_failed_time, '-infinity') AS is_archiving, last_failed_time IS NOT NULL " +
		"FROM pg_stat_archiver")

	var walArchivingWorking, lastFailedTimePresent bool

	if err := row.Scan(&walArchivingWorking, &lastFailedTimePresent); err != nil {
		log.Error(err, "can't get WAL archiving status")
		return err
	}

	if walArchivingWorking {
		log.Info("WAL archiving is working, proceeding with the backup")
		return nil
	}

	if lastFailedTimePresent {
		log.Info("WAL archiving is not working, will retry in one minute")
		return errors.New("wal-archive not working")
	}

	if w.firstWalArchiveTriggered {
		log.Info("Waiting for the first WAL file to be archived")
		return errors.New("waiting for first wal-archive")
	}

	log.Info("Triggering the first WAL file to be archived")
	if _, err := db.Exec("CHECKPOINT"); err != nil {
		return fmt.Errorf("error while requiring a checkpoint: %w", err)
	}

	if _, err := db.Exec("SELECT pg_switch_wal()"); err != nil {
		return fmt.Errorf("error while switching to a new WAL: %w", err)
	}

	w.firstWalArchiveTriggered = true
	return errors.New("first wal-archive triggered")
}
