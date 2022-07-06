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
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/mitchellh/go-ps"
	"k8s.io/utils/strings/slices"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// PostgresqlPidFile is the name of the file which contains
// the PostgreSQL PID file
const PostgresqlPidFile = "postmaster.pid" //wokeignore:rule=master

// CheckForExistingPostmaster checks if a postmaster process is running
// on the PGDATA volume. If it is, it returns its process entry.
//
// To do that, this function will read the PID file from the data
// directory and check the existence of the relative process. If the
// process exists, then that process entry is returned.
// If it doesn't exist then the PID file is stale and is removed.
//
// We are requiring a list of PostgreSQL executables that is used
// to check if the process with the ID read from the PID file
// is really a postmaster. This isn't really needed per se, but
// allows this function to be easier to test.
func (instance *Instance) CheckForExistingPostmaster(postgresExecutables ...string) (*os.Process, error) {
	pidFile := path.Join(instance.PgData, PostgresqlPidFile)
	contextLog := log.WithValues("file", pidFile)
	pidFileContents, pid, err := instance.GetPostmasterPidFromFile(pidFile)
	if err != nil {
		// The content of the PID file is wrong.
		// In this case we just remove the PID file, which is assumed
		// to be stale, and continue our work
		contextLog.Info("The PID file content is wrong, deleting it and assuming it's stale",
			"err", err, "pidFileContents", pidFileContents)
		return nil, instance.CleanUpStalePid()
	}

	process, err := ps.FindProcess(pid)
	if err != nil {
		// We cannot find this PID, so we can't really tell if this
		// process exists or not
		return nil, err
	}
	if process == nil {
		// The process doesn't exist and this PID file is stale
		contextLog.Info("The PID file is stale, deleting it", "pidFileContents", pidFileContents)
		return nil, instance.CleanUpStalePid()
	}

	if !slices.Contains(postgresExecutables, process.Executable()) {
		// The process is not running PostgreSQL and this PID file is stale
		contextLog.Info("The PID file is stale (executable mismatch), deleting it",
			"pidFileContents", pidFileContents, "postgresExecutable", process.Executable())
		return nil, instance.CleanUpStalePid()
	}

	// The postmaster PID file is not stale, and we need to keep it
	contextLog.Info("Detected alive postmaster from PID file")
	contextLog.Debug("PID file", "contents", pidFileContents)
	return os.FindProcess(pid)
}

// CleanUpStalePid cleans up the files left around by a crashed PostgreSQL instance.
// It removes the default PostgreSQL pid file and the content of the socket directory.
func (instance *Instance) CleanUpStalePid() error {
	pidFile := path.Join(instance.PgData, PostgresqlPidFile)

	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := fileutils.RemoveDirectoryContent(instance.SocketDirectory); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// GetPostmasterPidFromFile reads the given postmaster pid file, parse it and return its content and the actual pid
func (instance *Instance) GetPostmasterPidFromFile(pidFile string) ([]byte, int, error) {
	pidFileExists, err := fileutils.FileExists(pidFile)
	if err != nil {
		return nil, -1, err
	}

	if !pidFileExists {
		return nil, -1, os.ErrNotExist
	}

	// The PID file is existing. We need to check if it is stale
	// or not
	pidFileContents, err := fileutils.ReadFile(pidFile)
	if err != nil {
		return nil, -1, err
	}

	// Inside the PID file, the first line contain the actual postmaster
	// PID working on the data directory
	pidLine := strings.Split(string(pidFileContents), "\n")[0]
	pid, err := strconv.Atoi(strings.TrimSpace(pidLine))
	return pidFileContents, pid, err
}
