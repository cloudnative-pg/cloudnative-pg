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

// Package volumeusage enumerates the instance's data volumes and measures
// their disk usage.
package volumeusage

import (
	"path/filepath"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"

	pgpostgres "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/diskusage"
)

// BasePaths are the filesystem locations probed to discover volumes.
type BasePaths struct {
	PGData          string
	WALVolume       string
	TablespacesRoot string
}

// DefaultBasePaths returns the production mount points.
func DefaultBasePaths() BasePaths {
	return BasePaths{
		PGData:          specs.PgDataPath,
		WALVolume:       specs.PgWalVolumePath,
		TablespacesRoot: specs.PgTablespaceVolumePath,
	}
}

type mount struct {
	name string
	path string
}

// listMounts discovers the volumes that currently exist on disk.
// Enumeration errors are logged as warnings and skipped; pgdata and any
// groups already discovered are always included in the returned slice.
func listMounts(p BasePaths) []mount {
	mounts := []mount{{name: "pgdata", path: p.PGData}}

	if exists, err := fileutils.FileExists(p.WALVolume); err != nil {
		log.Warning("could not check WAL volume existence; skipping WAL volume", "path", p.WALVolume, "error", err)
	} else if exists {
		mounts = append(mounts, mount{name: "wal", path: p.WALVolume})
	}

	if exists, err := fileutils.FileExists(p.TablespacesRoot); err != nil {
		log.Warning(
			"could not check tablespaces root existence; skipping tablespaces",
			"path", p.TablespacesRoot,
			"error", err,
		)
	} else if exists {
		entries, err := fileutils.GetDirectoryContent(p.TablespacesRoot)
		if err != nil {
			log.Warning("could not list tablespaces directory; skipping tablespaces", "path", p.TablespacesRoot, "error", err)
		} else {
			for _, entry := range entries {
				mounts = append(mounts, mount{
					name: "tbs-" + entry,
					path: filepath.Join(p.TablespacesRoot, entry),
				})
			}
		}
	}

	return mounts
}

// Collect measures each discovered volume. Volumes that cannot be measured
// are skipped with a warning; Collect never returns an error so that a single
// bad mount never suppresses reporting for the others.
func Collect(p BasePaths) []pgpostgres.VolumeUsage {
	mounts := listMounts(p)

	usages := make([]pgpostgres.VolumeUsage, 0, len(mounts))
	for _, m := range mounts {
		u, err := diskusage.Get(m.path)
		if err != nil {
			log.Warning("could not measure volume usage", "volume", m.name, "path", m.path, "error", err)
			continue
		}
		usages = append(usages, pgpostgres.VolumeUsage{
			Name:           m.name,
			MountPoint:     m.path,
			TotalBytes:     u.TotalBytes,
			UsedBytes:      u.UsedBytes,
			AvailableBytes: u.AvailableBytes,
		})
	}
	return usages
}

// SharesFilesystem reports whether any two usages are backed by the same
// filesystem, which indicates a provisioner that does not isolate volumes
// (e.g. a directory-based host-path provisioner).
func SharesFilesystem(usages []diskusage.Usage) bool {
	seen := make(map[uint64]struct{}, len(usages))
	for _, u := range usages {
		if _, ok := seen[u.FilesystemID]; ok {
			return true
		}
		seen[u.FilesystemID] = struct{}{}
	}
	return false
}
