package controller

import (
	"context"
	"fmt"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"time"
)

type DisKWatcher struct {
	instance *postgres.Instance

	globalCtx    context.Context
	globalCancel context.CancelFunc
}

func NewDiskWatcher(ctx context.Context, instance *postgres.Instance) *DisKWatcher {
	return &DisKWatcher{
		instance:  instance,
		globalCtx: ctx,
	}
}

func (dw *DisKWatcher) Start(ctx context.Context) error {
	contextLog := log.FromContext(ctx).WithName("DiskWatcher")
	go func() {
		updateInterval := 5 * time.Second
		ticker := time.NewTicker(updateInterval)

		defer func() {
			ticker.Stop()
			contextLog.Info("Terminated DiskWatcher loop")
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}

			newInterval := dw.instance.DiskWatcherCheckInterval
			if newInterval == 0 {
				// If the interval is set to 0, it might mean the one of the following
				// 1. The feature is disabled
				// 2. The feature is enabled but the instance reconcilation is not done yet
				// In any of these two cases, we will do nothing here and wait.
				continue
			}

			if newInterval != updateInterval {
				// If the interval has changed, update the ticker
				ticker.Reset(newInterval)
				updateInterval = newInterval
			}

			err := dw.reconcileDiskUsageWatcher(ctx, contextLog)
			if err != nil {
				contextLog.Warning("reconciling disk usage watcher", "err", err)
				continue
			}
		}
	}()
	<-ctx.Done()
	return nil
}

func (dw *DisKWatcher) reconcileDiskUsageWatcher(ctx context.Context, ctxLog log.Logger) error {
	if !dw.instance.CanCheckReadiness() {
		return fmt.Errorf("instance is not ready yet")
	}

	isPrimary, err := dw.instance.IsPrimary()
	if err != nil {
		return fmt.Errorf("check if the instance is primary: %w", err)
	}

	if !isPrimary {
		// Instance is not primary, skip the reconcile
		return nil
	}

	used, err := dw.instance.GetPgDataDiskUsage()
	if err != nil {
		return fmt.Errorf("calculate disk usage: %w", err)
	}

	conn, err := dw.instance.GetSuperUserDB()
	if err != nil {
		return fmt.Errorf("get connection to the postgres database: %w", err)
	}

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// This is a no-op when the transaction is committed
		_ = tx.Rollback()
	}()

	// Get the list of databases and their default_transaction_read_only status
	statusByDatabase, err := dw.instance.GetDBAndStatus(tx)
	if err != nil {
		return fmt.Errorf("get databases and their status: %w", err)
	}

	killExistingConnections := false
	if used < dw.instance.ReadOnlyDiskUsageThreshold {
		// Disk usage is below the threshold, set all databases to read-write
		for dbName, isReadOnly := range statusByDatabase {
			if isReadOnly {
				err := dw.instance.OpenDatabase(tx, dbName)
				if err != nil {
					return fmt.Errorf("open database %s: %w", dbName, err)
				}
				killExistingConnections = true
			}
		}
	} else {
		// Disk usage is above the threshold, set all databases to read-only
		for dbName, isReadOnly := range statusByDatabase {
			if !isReadOnly {
				err := dw.instance.CloseDatabase(tx, dbName)
				if err != nil {
					return fmt.Errorf("close database %s: %w", dbName, err)
				}
				killExistingConnections = true
			}
		}
	}

	if killExistingConnections {
		ctxLog.Warning(
			"default_transaction_readonly state changed, dropping existing user connections for changes to take effect",
			"disk_usage", fmt.Sprintf("%.2f", used),
			"threshold", fmt.Sprintf("%.2f", dw.instance.ReadOnlyDiskUsageThreshold))
		// Drop existing connections for changes to take effect. This is necessary
		// because the old connections will retain the old read-only session configuration.
		err = dw.instance.DropUserConnections()
		if err != nil {
			return fmt.Errorf("drop existing connections: %w", err)
		}
	}

	return tx.Commit()
}
