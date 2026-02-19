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

package sternmultitailer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"text/template"
	"time"

	"github.com/stern/stern/stern"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// StreamLogs opens a goroutine to execute stern on all the pods that match
// the labelSelector. Their logs will be written to disk in the outputBaseDir, split by namespace, pod and container,
// using a namespace/pod/container.log file for each container of matching pods.
// Close the ctx context to terminate stern execution.
// Returns a channel that will be closed when all the logs have been written to disk
// and the ones we asked to remove have been deleted.
func StreamLogs(
	ctx context.Context,
	client kubernetes.Interface,
	labelSelector labels.Selector,
	outputBaseDir string,
) chan struct{} {
	outPipeReader, outPipeWriter := io.Pipe()
	errOut := os.Stdout

	// JSON output
	pod := regexp.MustCompile(".*")
	container := regexp.MustCompile(".*")
	t := "{ \"message\": {{json .Message}}, " +
		"\"namespace\": \"{{.Namespace}}\", " +
		"\"podName\": \"{{.PodName}}\", " +
		"\"containerName\": \"{{.ContainerName}}\" }\n"

	funs := template.FuncMap{
		"json": func(v any) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
	}

	parsedTemplate, _ := template.New("log").Funcs(funs).Parse(t)

	config := &stern.Config{
		Namespaces:            []string{},
		PodQuery:              pod,
		ContainerQuery:        container,
		ExcludePodQuery:       []*regexp.Regexp{},
		Timestamps:            false,
		TimestampFormat:       stern.TimestampFormatDefault,
		Location:              time.UTC,
		ExcludeContainerQuery: []*regexp.Regexp{},
		ContainerStates: []stern.ContainerState{
			stern.RUNNING,
		},
		Exclude:             []*regexp.Regexp{},
		Include:             []*regexp.Regexp{},
		Highlight:           []*regexp.Regexp{},
		InitContainers:      true,
		EphemeralContainers: true,
		Since:               48 * time.Hour,
		AllNamespaces:       true,
		LabelSelector:       labelSelector,
		FieldSelector:       fields.Everything(),
		TailLines:           nil,
		Template:            parsedTemplate,
		Follow:              true,
		Resource:            "",
		OnlyLogLines:        true,
		MaxLogRequests:      50,
		Stdin:               false,
		DiffContainer:       false,

		Out:    outPipeWriter,
		ErrOut: errOut,
	}

	outputDone := make(chan struct{})
	go func() {
		err := stern.Run(ctx, client, config)
		if err != nil {
			fmt.Printf("stern failed: %v", err)
		}
		if <-ctx.Done(); true {
			_ = outPipeWriter.Close()
			_ = errOut.Close()
		}
	}()

	go func() {
		outputWriter(outputBaseDir, outPipeReader)
		close(outputDone)
	}()

	return outputDone
}

func outputWriter(baseDir string, logReader io.Reader) {
	r := bufio.NewReader(logReader)
	openFilesMap := make(map[string]*os.File)
	defer func() {
		for k, file := range openFilesMap {
			_ = file.Close()
			delete(openFilesMap, k)
		}
	}()
	for {
		lineBytes, readErr := r.ReadBytes('\n')
		// If we have a read error, skip the line
		if readErr != nil && readErr != io.EOF {
			fmt.Printf("could not read log line from pipe: %v\n", readErr)
			continue
		}

		// If we have an EOF and the line is empty, I'm done
		if readErr == io.EOF && len(lineBytes) == 0 {
			break
		}

		// Otherwise, we have a line to process
		var logLine stern.Log
		err := json.Unmarshal(lineBytes, &logLine)
		if err != nil {
			fmt.Printf("could not unmarshal log line %v: %v\n", logLine, err)
			continue
		}

		file, err := getLogFile(baseDir, logLine, openFilesMap)
		if err != nil {
			fmt.Printf("no file to write log line %v: %v\n", logLine, err)
			continue
		}

		_, err = fmt.Fprintf(file, "%v\n", logLine.Message)
		if err != nil {
			fmt.Printf("could not write message to file %v: %v\n", file.Name(), err)
			continue
		}
	}
}

// Get an open file for the log, or open a new one
func getLogFile(baseDir string, log stern.Log, openFilesMap map[string]*os.File) (*os.File,
	error,
) {
	filePath := path.Join(baseDir, log.Namespace, log.PodName, log.ContainerName+".log")
	dirFile := path.Dir(filePath)

	file, ok := openFilesMap[filePath]
	if ok {
		return file, nil
	}

	// If we don't have the file already opened, we open it
	err := os.MkdirAll(dirFile, 0o700)
	if err != nil {
		return nil, fmt.Errorf("cannot ensure directory existence (%v): %w", dirFile, err)
	}
	file, err = os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600) // nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("cannot open file %v: %w", filePath, err)
	}

	openFilesMap[filePath] = file
	return file, nil
}
