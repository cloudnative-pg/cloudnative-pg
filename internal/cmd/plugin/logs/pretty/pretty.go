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

package pretty

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/stringset"
	"github.com/logrusorgru/aurora/v4"
	"github.com/spf13/cobra"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

type prettyCmd struct {
	loggers   *stringset.Data
	pods      *stringset.Data
	groupSize int
	verbosity int
	minLevel  LogLevel
}

// NewCmd creates a new `kubectl cnpg logs pretty` command
func NewCmd() *cobra.Command {
	var loggers, pods []string
	var sortingGroupSize, verbosity int
	bf := prettyCmd{}

	cmd := &cobra.Command{
		Use:   "pretty",
		Short: "Prettify CNPG logs",
		Long:  "Reads CNPG logs from standard input and pretty-prints them for human consumption",
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return plugin.CompleteClusters(cmd.Context(), args, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			bf.loggers = stringset.From(loggers)
			bf.pods = stringset.From(pods)
			bf.groupSize = sortingGroupSize
			bf.verbosity = verbosity

			recordChannel := make(chan logRecord)
			recordGroupsChannel := make(chan []logRecord)

			var wait sync.WaitGroup

			wait.Go(func() {
				bf.decode(cmd.Context(), os.Stdin, recordChannel)
			})

			wait.Go(func() {
				bf.group(cmd.Context(), recordChannel, recordGroupsChannel)
			})

			wait.Go(func() {
				bf.write(cmd.Context(), recordGroupsChannel, os.Stdout)
			})

			wait.Wait()
			return nil
		},
	}

	cmd.Flags().IntVar(&sortingGroupSize, "sorting-group-size", 1000,
		"The maximum size of the window where logs are collected for sorting")
	cmd.Flags().StringSliceVar(&loggers, "loggers", nil,
		"The list of loggers to receive. Defaults to all.")
	cmd.Flags().StringSliceVar(&pods, "pods", nil,
		"The list of pods to receive from. Defaults to all.")
	cmd.Flags().Var(&bf.minLevel, "min-level",
		`Hides the messages whose log level is less important than the specified one.
Should be empty or one of error, warning, info, debug, or trace.`)
	cmd.Flags().CountVarP(&verbosity, "verbosity", "v",
		"The logs verbosity level. More verbose means more information will be printed")

	return cmd
}

// decode progressively decodes the logs
func (bf *prettyCmd) decode(ctx context.Context, reader io.Reader, recordChannel chan<- logRecord) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		record, err := newLogRecordFromBytes(scanner.Bytes())
		if err != nil {
			_, _ = fmt.Fprintln(
				os.Stderr,
				aurora.Red(fmt.Sprintf("JSON syntax error (%s)", err.Error())),
				scanner.Text())
			continue
		}

		record.normalize()

		if !bf.isRecordRelevant(record) {
			continue
		}

		recordChannel <- *record
	}

	close(recordChannel)
}

// group transforms a stream of logs into a stream of log groups, so that the groups
// can then be sorted
func (bf *prettyCmd) group(ctx context.Context, logChannel <-chan logRecord, groupChannel chan<- []logRecord) {
	bufferArray := make([]logRecord, bf.groupSize)

	buffer := bufferArray[0:0]

	pushLogGroup := func() {
		if len(buffer) == 0 {
			return
		}

		bufferCopy := make([]logRecord, len(buffer))
		copy(bufferCopy, buffer)
		groupChannel <- bufferCopy

		buffer = bufferArray[0:0]
	}

logLoop:
	for {
		timer := time.NewTimer(1 * time.Second)
		defer timer.Stop()

		select {
		case <-ctx.Done():
			break logLoop

		case <-timer.C:
			pushLogGroup()

		case logRecord, ok := <-logChannel:
			if !ok {
				break logLoop
			}

			buffer = append(buffer, logRecord)
			if len(buffer) == bf.groupSize {
				pushLogGroup()
			}
		}
	}

	pushLogGroup()
	close(groupChannel)
}

// write writes the logs on the output
func (bf *prettyCmd) write(ctx context.Context, recordGroupChannel <-chan []logRecord, writer io.Writer) {
	logRecordComparison := func(l1, l2 logRecord) int {
		if l1.TS < l2.TS {
			return -1
		} else if l1.TS > l2.TS {
			return 1
		}

		if l1.LoggingPod < l2.LoggingPod {
			return -1
		} else if l1.LoggingPod == l2.LoggingPod {
			return 0
		}

		return 1
	}
	firstGroup := true

logLoop:
	for {
		select {
		case <-ctx.Done():
			break logLoop

		case logGroupRecord, ok := <-recordGroupChannel:
			if !ok {
				break logLoop
			}

			slices.SortFunc(logGroupRecord, logRecordComparison)

			if !firstGroup {
				_, _ = writer.Write([]byte("---\n"))
			}
			for _, record := range logGroupRecord {
				if err := record.print(writer, bf.verbosity); err != nil {
					bf.emergencyLog(err, "Dumping a log entry")
				}
			}
			firstGroup = false
		}
	}
}

// isRecordRelevant is true when the passed log record is matched
// by the filters set by the user
func (bf *prettyCmd) isRecordRelevant(record *logRecord) bool {
	if bf.loggers.Len() > 0 && !bf.loggers.Has(record.Logger) {
		return false
	}

	if bf.pods.Len() > 0 && !bf.pods.Has(record.LoggingPod) {
		return false
	}

	if bf.minLevel != "" && record.Level.Less(bf.minLevel) {
		return false
	}

	return true
}

func (bf *prettyCmd) emergencyLog(err error, msg string) {
	fmt.Println(aurora.Red("ERROR"), err.Error(), msg)
}
