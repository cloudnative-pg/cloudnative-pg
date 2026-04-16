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

import "testing"

func FuzzCreatePostgresqlConfigurationSanitization(f *testing.F) { //nolint: gocognit
	f.Add(uint8(17), "off", "target_name", "128MB", "internal'value", true, false, false, false, false)
	f.Add(uint8(14), "on", "", "256MB", "", false, true, true, false, false)
	f.Add(uint8(16), "off", "restore_target", "512MB", "quoted'value", false, false, false, true, true)

	f.Fuzz(func(
		t *testing.T,
		majorSeed uint8,
		sslValue, recoveryTargetName, sharedBuffers, internalValue string,
		includingMandatory, preserveFixedSettingsFromUser, isReplicaCluster, isWalArchivingDisabled, alterSystemEnabled bool,
	) {
		majorVersion := 13 + int(majorSeed%6)

		cfg := CreatePostgresqlConfiguration(ConfigurationInfo{
			Settings:                      CnpgConfigurationSettings,
			MajorVersion:                  majorVersion,
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

		if got := cfg.GetConfig("shared_buffers"); got != sharedBuffers {
			t.Fatalf("non-fixed parameter changed: got %q want %q", got, sharedBuffers)
		}

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
				t.Fatalf(
					"recovery_target_name user value not preserved: got %q want %q",
					got,
					recoveryTargetName,
				)
			}
		default:
			if got := cfg.GetConfig("ssl"); got != "" {
				t.Fatalf("ssl should be removed when fixed settings are not preserved, got %q", got)
			}
			if got := cfg.GetConfig("recovery_target_name"); got != "" {
				t.Fatalf("recovery_target_name should be removed, got %q", got)
			}
		}

		expectedArchiveMode := "on"
		switch {
		case isWalArchivingDisabled:
			expectedArchiveMode = "off"
		case isReplicaCluster:
			expectedArchiveMode = "always"
		}
		if got := cfg.GetConfig("archive_mode"); got != expectedArchiveMode {
			t.Fatalf("archive_mode mismatch: got %q want %q", got, expectedArchiveMode)
		}

		if includingMandatory && majorVersion >= 17 {
			want := "off"
			if alterSystemEnabled {
				want = "on"
			}
			if got := cfg.GetConfig("allow_alter_system"); got != want {
				t.Fatalf("allow_alter_system mismatch: got %q want %q", got, want)
			}
		}

		_, sha1 := CreatePostgresqlConfFile(cfg)
		cfg.OverwriteConfig("cnpg.fuzz_internal", internalValue)
		_, sha2 := CreatePostgresqlConfFile(cfg)

		if sha1 == "" || sha2 == "" {
			t.Fatalf("empty config hash")
		}
		if sha1 != sha2 {
			t.Fatalf("cnpg.* entries must not affect config hash")
		}
	})
}
