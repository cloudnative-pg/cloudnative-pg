package storage

import (
	"context"
	"io/fs"
	"os"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
)

// ReconcileWalStorage moves the files from PGDATA/pg_wal to the volume attached, if exists, and
// creates a symlink for it
func ReconcileWalStorage(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	if pgWalExists, err := fileutils.FileExists(specs.PgWalVolumePath); err != nil {
		return err
	} else if !pgWalExists {
		return nil
	}

	pgWalDirInfo, err := os.Lstat(specs.PgWalPath)
	if err != nil {
		return err
	}
	// The pgWalDir it's already a symlink meaning that there's nothing to do
	mode := pgWalDirInfo.Mode() & fs.ModeSymlink
	if !pgWalDirInfo.IsDir() && mode != 0 {
		return nil
	}

	// We discarded every possibility that this has been done, let's move the current file to their
	// new location
	contextLogger.Info("Moving data", "from", specs.PgWalPath, "to", specs.PgWalVolumePgWalPath)
	if err := fileutils.MoveDirectoryContent(specs.PgWalPath, specs.PgWalVolumePgWalPath); err != nil {
		contextLogger.Error(err, "Moving data", "from", specs.PgWalPath, "to",
			specs.PgWalVolumePgWalPath)
		return err
	}

	contextLogger.Debug("Deleting old path", "path", specs.PgWalPath)
	if err := fileutils.RemoveFile(specs.PgWalPath); err != nil {
		contextLogger.Error(err, "Deleting old path", "path", specs.PgWalPath)
		return err
	}

	// We moved all the files now we should create the proper symlink
	contextLogger.Debug("Creating symlink", "from", specs.PgWalPath, "to", specs.PgWalVolumePgWalPath)
	return os.Symlink(specs.PgWalVolumePgWalPath, specs.PgWalPath)
}
