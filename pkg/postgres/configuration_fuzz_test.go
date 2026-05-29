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
	"strings"
	"testing"

	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
)

func FuzzCreatePostgresqlConfigurationSanitization(f *testing.F) {
	f.Add(uint8(17), "off", "target_name", "128MB", "internal'value", true, false, false, false, false)
	f.Add(uint8(14), "on", "", "256MB", "", false, true, true, false, false)
	f.Add(uint8(16), "off", "restore_target", "512MB", "quoted'value", false, false, false, true, true)
	f.Add(uint8(15), "on", "", "64MB", "with\nnewline\tand\\backslash", true, false, false, false, true)

	f.Fuzz(func(
		t *testing.T,
		majorSeed uint8,
		sslValue, recoveryTargetName, sharedBuffers, internalValue string,
		includingMandatory, preserveFixedSettingsFromUser, isReplicaCluster, isWalArchivingDisabled, alterSystemEnabled bool,
	) {
		majorVersion := 13 + int(majorSeed%6)

		cfg := CreatePostgresqlConfiguration(ConfigurationInfo{
			Settings:                      CnpgConfigurationSettings,
			Version:                       version.New(uint64(majorVersion), 0),
			IncludingMandatory:            includingMandatory,
			PreserveFixedSettingsFromUser: preserveFixedSettingsFromUser,
			IsReplicaCluster:              isReplicaCluster,
			IsWalArchivingDisabled:        isWalArchivingDisabled,
			IsAlterSystemEnabled:          alterSystemEnabled,
			UserSettings: map[string]string{
				"ssl":                  sslValue,
				"recovery_target_name": recoveryTargetName,
				"shared_buffers":       sharedBuffers,
			},
		})

		checkFixedSettings(t, cfg, sslValue, recoveryTargetName, includingMandatory, preserveFixedSettingsFromUser)
		checkArchiveMode(t, cfg, isWalArchivingDisabled, isReplicaCluster)
		checkAlterSystem(t, cfg, majorVersion, includingMandatory, alterSystemEnabled)

		cfg.OverwriteConfig("custom.fuzz_value", internalValue)
		_, sha1 := CreatePostgresqlConfFile(cfg)
		cfg.OverwriteConfig("cnpg.fuzz_internal", internalValue)
		rendered, sha2 := CreatePostgresqlConfFile(cfg)
		if sha1 == "" || sha2 == "" {
			t.Fatalf("empty config hash")
		}
		if sha1 != sha2 {
			t.Fatalf("cnpg.* entries must not affect config hash")
		}

		checkRenderedConfWellFormed(t, rendered)
	})
}

func checkFixedSettings(
	t *testing.T,
	cfg *PgConfiguration,
	sslValue, recoveryTargetName string,
	includingMandatory, preserveFixedSettingsFromUser bool,
) {
	t.Helper()
	switch {
	case includingMandatory:
		if got := cfg.GetConfig("ssl"); got != "on" {
			t.Fatalf("ssl must be sanitized to mandatory value, got %q", got)
		}
		if got := cfg.GetConfig("recovery_target_name"); got != "" {
			t.Fatalf("recovery_target_name must be dropped, got %q", got)
		}
	case preserveFixedSettingsFromUser:
		if got := cfg.GetConfig("ssl"); got != sslValue {
			t.Fatalf("ssl user value not preserved: got %q want %q", got, sslValue)
		}
		if got := cfg.GetConfig("recovery_target_name"); got != recoveryTargetName {
			t.Fatalf("recovery_target_name user value not preserved: got %q want %q", got, recoveryTargetName)
		}
	default:
		if got := cfg.GetConfig("ssl"); got != "" {
			t.Fatalf("ssl should be removed when fixed settings are not preserved, got %q", got)
		}
		if got := cfg.GetConfig("recovery_target_name"); got != "" {
			t.Fatalf("recovery_target_name should be removed, got %q", got)
		}
	}
}

func checkArchiveMode(t *testing.T, cfg *PgConfiguration, isWalArchivingDisabled, isReplicaCluster bool) {
	t.Helper()
	want := "on"
	switch {
	case isWalArchivingDisabled:
		want = "off"
	case isReplicaCluster:
		want = "always"
	}
	if got := cfg.GetConfig("archive_mode"); got != want {
		t.Fatalf("archive_mode mismatch: got %q want %q", got, want)
	}
}

func checkAlterSystem(
	t *testing.T,
	cfg *PgConfiguration,
	majorVersion int,
	includingMandatory, alterSystemEnabled bool,
) {
	t.Helper()
	if !includingMandatory || majorVersion < 17 {
		return
	}
	want := "off"
	if alterSystemEnabled {
		want = "on"
	}
	if got := cfg.GetConfig("allow_alter_system"); got != want {
		t.Fatalf("allow_alter_system mismatch: got %q want %q", got, want)
	}
}

// checkRenderedConfWellFormed enforces the postgresql.conf injection
// invariant: a user-controlled value cannot break out of its `key = 'value'`
// line through an unescaped newline or quote.
func checkRenderedConfWellFormed(t *testing.T, rendered string) {
	t.Helper()
	for i, line := range strings.Split(rendered, "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, " = ")
		if !ok {
			t.Fatalf("line %d has no `key = value` separator: %q", i, line)
		}
		if key == "" || strings.ContainsAny(key, " \t'\"") {
			t.Fatalf("line %d has malformed key %q", i, key)
		}
		if len(value) < 2 || !strings.HasPrefix(value, "'") || !strings.HasSuffix(value, "'") {
			t.Fatalf("line %d value not single-quoted: %q", i, value)
		}
	}
}
