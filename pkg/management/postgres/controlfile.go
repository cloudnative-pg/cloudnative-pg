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
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"time"
)

// pgControlFile is the path of the control file, relative to PGDATA
const pgControlFile = "global/pg_control"

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// errControlFileCRCNotFound is returned when no CRC32C field validates the control file
var errControlFileCRCNotFound = errors.New("cannot locate a valid CRC in pg_control")

// ControlFileData is the portion of pg_control this package manipulates.
//
// ControlFileData starts with the uint64 system_identifier and ends with a
// CRC32C of every byte preceding the CRC. The CRC offset differs between
// major versions, so it is located by finding the only 4-byte aligned offset
// whose preceding bytes hash to the value stored there. This keeps the code
// independent from the layout of a specific major version.
type ControlFileData struct {
	raw       []byte
	crcOffset int
}

// ReadControlFile reads and validates global/pg_control inside pgData
func ReadControlFile(pgData string) (*ControlFileData, error) {
	raw, err := os.ReadFile(filepath.Join(pgData, pgControlFile)) // #nosec G304
	if err != nil {
		return nil, err
	}
	return parseControlFile(raw)
}

func parseControlFile(raw []byte) (*ControlFileData, error) {
	// The CRC cannot come before system_identifier (8 bytes) and the
	// pg_control_version field that follows it.
	const minOffset = 12
	found := -1
	for offset := minOffset; offset+4 <= len(raw); offset += 4 {
		if crc32.Checksum(raw[:offset], crc32cTable) != binary.LittleEndian.Uint32(raw[offset:]) {
			continue
		}
		if found != -1 {
			return nil, fmt.Errorf("ambiguous CRC in pg_control at offsets %d and %d", found, offset)
		}
		found = offset
	}
	if found == -1 {
		return nil, errControlFileCRCNotFound
	}
	return &ControlFileData{raw: raw, crcOffset: found}, nil
}

// SystemIdentifier returns the database system identifier
func (c *ControlFileData) SystemIdentifier() uint64 {
	return binary.LittleEndian.Uint64(c.raw[:8])
}

// SetSystemIdentifier changes the system identifier and recomputes the CRC
func (c *ControlFileData) SetSystemIdentifier(id uint64) {
	binary.LittleEndian.PutUint64(c.raw[:8], id)
	binary.LittleEndian.PutUint32(c.raw[c.crcOffset:], crc32.Checksum(c.raw[:c.crcOffset], crc32cTable))
}

// Write atomically replaces global/pg_control inside pgData
func (c *ControlFileData) Write(pgData string) error {
	target := filepath.Join(pgData, pgControlFile)
	tmp := target + ".cnpg-tmp"
	if err := os.WriteFile(tmp, c.raw, 0o600); err != nil {
		return err
	}
	f, err := os.Open(tmp) // #nosec G304
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	_ = f.Close()
	if syncErr != nil {
		return syncErr
	}
	return os.Rename(tmp, target)
}

// NewSystemIdentifier builds a system identifier the same way
// pg_createsubscriber does: seconds, microseconds and the low PID bits
func NewSystemIdentifier(now time.Time, pid int) uint64 {
	id := uint64(now.Unix()) << 32            // #nosec G115
	id |= uint64(now.Nanosecond()/1000) << 12 // #nosec G115
	id |= uint64(pid) & 0xFFF                 // #nosec G115
	return id
}
