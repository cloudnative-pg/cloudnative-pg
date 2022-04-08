/*
Copyright 2019-2022 The CloudNativePG Contributors

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

package run

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

var pgBouncerLogRegex = regexp.
	MustCompile(`(?s)(?P<Timestamp>^.*) \[(?P<Pid>[0-9]+)\] (?P<Level>[A-Z]+) (?P<Msg>.+)$`)

type pgBouncerLogWriter struct {
	Logger log.Logger
}

func (p *pgBouncerLogWriter) Write(in []byte) (n int, err error) {
	// pgbouncer can write multi-line logs, and each continuation line starts
	// with "\t". This code will parse that and work on each log line separately

	currentLogLine := ""

	sc := bufio.NewScanner(bytes.NewReader(in))
	for sc.Scan() {
		logLine := sc.Text()
		if strings.HasPrefix(logLine, "\t") {
			currentLogLine += logLine
			continue
		}

		if currentLogLine != "" {
			p.writePgbouncerLogLine(currentLogLine)
		}

		currentLogLine = logLine
	}

	if currentLogLine != "" {
		p.writePgbouncerLogLine(currentLogLine)
	}

	return len(in), nil
}

func (p *pgBouncerLogWriter) writePgbouncerLogLine(line string) {
	matches := pgBouncerLogRegex.FindStringSubmatch(line)
	switch {
	case matches == nil:
		p.Logger.WithValues("matched", false).Info(line)
	case len(matches) != 5:
		p.Logger.WithValues("matched", false, "matches", matches).Info(line)
	default:
		p.Logger.Info("record", "record",
			pgBouncerLogRecord{
				Timestamp: matches[1],
				Pid:       matches[2],
				Level:     matches[3],
				Msg:       matches[4],
			})
	}
}

type pgBouncerLogRecord struct {
	Timestamp string `json:"timestamp"`
	Pid       string `json:"pid"`
	Level     string `json:"level"`
	Msg       string `json:"msg"`
}
