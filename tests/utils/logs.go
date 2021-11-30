/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"encoding/json"
	"strings"
	"time"
)

// ParseJSONLogs returns the pod's logs of a given pod name,
// in the form of a list of JSON entries
func ParseJSONLogs(namespace string, podName string, env *TestingEnvironment) ([]map[string]interface{}, error) {
	// Gather pod logs
	podLogs, err := env.GetPodLogs(namespace, podName)
	if err != nil {
		return nil, err
	}

	// In pod logs, each row is stored as JSON object. Split the JSON objects into JSON array
	logEntries := strings.Split(podLogs, "\n")
	parsedLogEntries := make([]map[string]interface{}, len(logEntries))
	for i, entry := range logEntries {
		if entry == "" {
			continue
		}
		parsedEntry := make(map[string]interface{})
		err := json.Unmarshal([]byte(entry), &parsedEntry)
		if err != nil {
			return nil, err
		}
		parsedLogEntries[i] = parsedEntry
	}
	return parsedLogEntries, nil
}

// HasLogger verifies if a given value exists inside a list of JSON entries
func HasLogger(logEntries []map[string]interface{}, logger string) bool {
	for _, logEntry := range logEntries {
		if logEntry["logger"] == logger {
			return true
		}
	}
	return false
}

// AssertQueryRecord verifies if any of the message record field of each JSON row
// contains the expectedResult string, then applies the assertions related to the log format
func AssertQueryRecord(logEntries []map[string]interface{}, errorTestQuery string, message string, logger string) bool {
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
func IsWellFormedLogForLogger(item map[string]interface{}, loggerField string) bool {
	if logger, ok := item["logger"]; !ok || logger != loggerField {
		return false
	}
	if msg, ok := item["msg"]; !ok || msg == "" {
		return false
	}
	if record, ok := item["record"]; ok && record != "" {
		_, ok = record.(map[string]interface{})
		if !ok {
			return false
		}
	}

	return true
}

// CheckRecordForQuery applies some assertions related to the format of the JSON log fields keys and values
func CheckRecordForQuery(entry map[string]interface{}, errorTestQuery, user, database, message string) bool {
	record, ok := entry["record"]
	if !ok || record == nil {
		return false
	}
	recordMap, isMap := record.(map[string]interface{})
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
