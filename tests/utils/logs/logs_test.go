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

package logs

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing CheckOptionForBarmanCommand", func() {
	// nolint: lll
	const podLogs = `{"level":"info","ts":"2024-03-04T06:07:29Z","msg":"Starting barman-cloud-backup","backupName":"pg-with-backup-20240304135929","backupNamespace":"pg-with-backup-20240304135929","logging_pod":"pg-with-backup-1","options":["--user","postgres","--name","backup-20240304055929","--immediate-checkpoint","--min-chunk-size=5MB","--read-timeout=60","--endpoint-url","http://minio-service:9000","--cloud-provider","aws-s3","s3://cluster-backups/","pg-with-backup"]}`

	It("should return true if all expected options are found", func() {
		parsedEntries := make([]map[string]interface{}, 0, 1)
		parsedEntry := make(map[string]interface{})
		err := json.Unmarshal([]byte(podLogs), &parsedEntry)
		Expect(err).ToNot(HaveOccurred())
		parsedEntries = append(parsedEntries, parsedEntry)
		result, err := CheckOptionsForBarmanCommand(
			parsedEntries,
			"Starting barman-cloud-backup",
			"pg-with-backup-20240304135929",
			"pg-with-backup-1",
			[]string{"--min-chunk-size=5MB", "--read-timeout=60"},
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("should return false if an expected option is not found in the log", func() {
		parsedEntries := make([]map[string]interface{}, 0, 1)
		parsedEntry := make(map[string]interface{})
		err := json.Unmarshal([]byte(podLogs), &parsedEntry)
		Expect(err).ToNot(HaveOccurred())
		parsedEntries = append(parsedEntries, parsedEntry)
		result, err := CheckOptionsForBarmanCommand(
			parsedEntries,
			"Starting barman-cloud-backup",
			"pg-with-backup-20240304135929",
			"pg-with-backup-1",
			// the --vv option is not present in the log file
			[]string{"--min-chunk-size=5MB", "--read-timeout=60", "--vv"},
		)
		Expect(err).To(HaveOccurred())
		Expect(result).To(BeFalse())
	})
})
