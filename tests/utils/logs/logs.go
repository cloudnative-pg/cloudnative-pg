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
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
)

// ParseJSONLogs returns the pod's logs of a given pod name,
// in the form of a list of JSON entries
func ParseJSONLogs(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	namespace string, podName string,
) ([]map[string]any, error) {
	// Gather pod logs
	podLogs, err := pods.Logs(ctx, kubeInterface, namespace, podName)
	if err != nil {
		return nil, err
	}

	// In pod logs, each row is stored as JSON object. Split the JSON objects into JSON array
	logEntries := strings.Split(podLogs, "\n")
	parsedLogEntries := make([]map[string]any, len(logEntries))
	for i, entry := range logEntries {
		if entry == "" {
			continue
		}
		parsedEntry := make(map[string]any)
		err := json.Unmarshal([]byte(entry), &parsedEntry)
		if err != nil {
			return nil, err
		}
		parsedLogEntries[i] = parsedEntry
	}
	return parsedLogEntries, nil
}

// HasLogger verifies if a given value exists inside a list of JSON entries
func HasLogger(logEntries []map[string]any, logger string) bool {
	for _, logEntry := range logEntries {
		if logEntry["logger"] == logger {
			return true
		}
	}
	return false
}

// AssertQueryRecord verifies if any of the message record field of each JSON row
// contains the expectedResult string, then applies the assertions related to the log format
func AssertQueryRecord(logEntries []map[string]any, errorTestQuery string, message string, logger string) bool {
	for _, logEntry := range logEntries {
		if IsWellFormedLogForLogger(logEntry, logger) &&
			CheckRecordForQuery(logEntry, errorTestQuery, "postgres", "app", message) {
			return true
		}
	}
	return false
}

// IsWellFormedLogForLogger verifies if the message record field of the given map
// contains the expectedResult string. If the message field matches the expectedResult,
// then the related record is returned. Otherwise return nil.
func IsWellFormedLogForLogger(item map[string]any, loggerField string) bool {
	if logger, ok := item["logger"]; !ok || logger != loggerField {
		return false
	}
	if msg, ok := item["msg"]; !ok || msg == "" {
		return false
	}
	if record, ok := item["record"]; ok && record != "" {
		_, ok = record.(map[string]any)
		if !ok {
			return false
		}
	}

	return true
}

// CheckRecordForQuery applies some assertions related to the format of the JSON log fields keys and values
func CheckRecordForQuery(entry map[string]any, errorTestQuery, user, database, message string) bool {
	record, ok := entry["record"]
	if !ok || record == nil {
		return false
	}
	recordMap, isMap := record.(map[string]any)
	// JSON entry assertions
	if !isMap || recordMap["user_name"] != user ||
		recordMap["database_name"] != database ||
		recordMap["query"] != errorTestQuery ||
		!strings.Contains(message, recordMap["message"].(string)) {
		return false
	}

	// Check the format of the log_time field
	timeFormat := "2006-01-02 15:04:05.999 UTC"
	_, err := time.Parse(timeFormat, recordMap["log_time"].(string))
	return err == nil
}

// CheckOptionsForBarmanCommand checks if the expected options are used from the barman command execution log
func CheckOptionsForBarmanCommand(
	logEntries []map[string]any,
	message, backupName, podName string,
	optionsExpected []string,
) (bool, error) {
	var optionsInLog any
	for _, logEntry := range logEntries {
		if logEntry["msg"] == message &&
			logEntry["backupName"] == backupName &&
			logEntry["logging_pod"] == podName {
			optionsInLog = logEntry["options"]
			break // We only need to check the first occurrence of the message
		}
	}
	if optionsInLog == nil {
		return false, fmt.Errorf("no log entry found for message %v, backupName %v and logging_pod %v",
			message,
			backupName,
			podName,
		)
	}

	optionsSlice, isSlice := optionsInLog.([]any)
	if !isSlice {
		return false, fmt.Errorf("optionsInLog is not a slice %v", optionsInLog)
	}

	for _, option := range optionsExpected {
		if !slices.ContainsFunc(optionsSlice, func(opt any) bool { return opt == option }) {
			return false, fmt.Errorf("option %v is not found in logEntry %v",
				option,
				optionsInLog,
			)
		}
	}
	return true, nil
}
