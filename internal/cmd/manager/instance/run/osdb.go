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

package run

import "time"

// OSEntry represents an OS version.
//
// Between the release date and DeprecatedFrom, the OS Version
// is **supported** and **not deprecated**.
//
// Between DeprecatedFrom and SupportedUntil, the OS Version
// is **supported** and **deprecated**.
//
// After SupportedUntil, the OS Version
// is **not supported** and **deprecated**.
type OSEntry struct {
	// Version identifies the operating system version. This is the same
	// as the "VERSION" field in the "/etc/osrelease" file.
	Version string `json:"version"`

	// DeprecatedFrom is the end-of-life date of this OS version
	DeprecatedFrom time.Time `json:"deprecatedFrom"`

	// SupportedUntil is the end of the OS version long-term-support period,
	// as defined.
	SupportedUntil time.Time `json:"supportedUntil"`
}

// IsSupported checks if the release is supported.
func (e *OSEntry) IsSupported(now time.Time) bool {
	return now.Before(e.SupportedUntil)
}

// IsDeprecated checks if the release is deprecated.
func (e *OSEntry) IsDeprecated(now time.Time) bool {
	return now.After(e.DeprecatedFrom)
}

// OSDB is a set of known OS releases
type OSDB struct {
	data map[string]OSEntry
}

var defaultOSDB OSDB

// Register adds a new know OS entry
func (db *OSDB) Register(entry OSEntry) {
	if db.data == nil {
		db.data = make(map[string]OSEntry)
	}
	db.data[entry.Version] = entry
}

// Get gets the OS entry for a known version
func (db *OSDB) Get(version string) (OSEntry, bool) {
	entry, ok := db.data[version]
	return entry, ok
}

func init() {
	// Known Debian releases
	defaultOSDB.Register(OSEntry{
		Version:        "10 (buster)",
		DeprecatedFrom: time.Date(2022, time.September, 10, 0, 0, 0, 0, time.UTC),
		SupportedUntil: time.Date(2024, time.June, 30, 0, 0, 0, 0, time.UTC),
	})
	defaultOSDB.Register(OSEntry{
		Version:        "11 (bullseye)",
		DeprecatedFrom: time.Date(2024, time.August, 14, 0, 0, 0, 0, time.UTC),
		SupportedUntil: time.Date(2026, time.August, 31, 0, 0, 0, 0, time.UTC),
	})
	defaultOSDB.Register(OSEntry{
		Version:        "12 (bookworm)",
		DeprecatedFrom: time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC),
		SupportedUntil: time.Date(2028, time.June, 30, 0, 0, 0, 0, time.UTC),
	})
	defaultOSDB.Register(OSEntry{
		Version:        "13 (trixie)",
		DeprecatedFrom: time.Date(2028, time.August, 9, 0, 0, 0, 0, time.UTC),
		SupportedUntil: time.Date(2030, time.June, 30, 0, 0, 0, 0, time.UTC),
	})
}
