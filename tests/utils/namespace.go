/*
Copyright The CloudNativePG Contributors

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

package utils

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
)

func writeInlineOutput(linesToShow, bufferIdx, capLines int, lineBuffer []string, output io.Writer) error {
	// print the last `capLines` lines of logs to the `output`
	_, _ = fmt.Fprintln(output, "-- OPERATOR LOGS for this namespace, with error/warning --")
	var switchErr error
	switch {
	case linesToShow == 0:
		_, switchErr = fmt.Fprintln(output, "-- no error / warning logs --")
	case linesToShow <= capLines:
		_, switchErr = fmt.Fprintln(output, strings.Join(lineBuffer[:linesToShow], "\n"))
	case bufferIdx == 0:
		// if bufferIdx == 0, the buffer just finished filling and is in order
		_, switchErr = fmt.Fprintln(output, strings.Join(lineBuffer, "\n"))
	default:
		// the line buffer cycled back and the items 0 to bufferIdx - 1 are newer than the rest
		_, switchErr = fmt.Fprintln(output, strings.Join(
			append(lineBuffer[bufferIdx:], lineBuffer[:bufferIdx]...),
			"\n"))
	}

	return switchErr
}

type logLine map[string]interface{}

func (l logLine) isAtLeastWarningLevel() bool {
	rawLevel := l["level"]
	return rawLevel == log.WarningLevelString || rawLevel == log.ErrorLevelString
}

func (l logLine) getNamespace() string {
	s, ok := l["namespace"].(string)
	if !ok {
		return ""
	}

	return s
}

func (l logLine) matchesNamespace(ns string) bool {
	return l.getNamespace() == ns
}

// saveNamespaceLogs does 2 things:
//   - displays the last `capLines` of error/warning logs on the `output` io.Writer (likely GinkgoWriter)
//   - saves the full logs to a file
func saveNamespaceLogs(
	buf *bytes.Buffer,
	logsType string,
	specName string,
	namespace string,
	output io.Writer,
	capLines int,
) {
	scanner := bufio.NewScanner(buf)
	scanner.Buffer(make([]byte, 0, 4096), 1024*1024)
	filename := fmt.Sprintf("out/%s_ns-%s_%s.log", logsType, namespace, specName)
	f, createErr := os.Create(filepath.Clean(filename))
	if createErr != nil {
		fmt.Println(createErr)
		return
	}
	defer func() {
		if syncErr := f.Sync(); syncErr != nil {
			if _, err := fmt.Fprintln(output, "ERROR while flushing file:", syncErr); err != nil {
				fmt.Println(err)
			}
		}

		if closeErr := f.Close(); closeErr != nil {
			if _, err := fmt.Fprintln(output, "ERROR while closing file:", closeErr); err != nil {
				fmt.Println(err)
			}
		}
	}()

	// circular buffer to hold the last `capLines` of non-DEBUG operator logs
	importantLogsBuffer := make([]string, capLines)
	importantLogsIdx := 0
	// insertion point in the lineBuffer: values 0 to capLines - 1 (i.e. modulo capLines)
	importantLogsBufferIdx := 0

	for scanner.Scan() {
		rawLine := scanner.Text()

		var parsedLine logLine
		if unmarshalErr := json.Unmarshal([]byte(rawLine), &parsedLine); unmarshalErr != nil {
			if _, err := fmt.Fprintln(output, "ERROR parsing log:", unmarshalErr, rawLine); err != nil {
				fmt.Println(err)
				continue
			}
		}

		if !parsedLine.matchesNamespace(namespace) {
			continue
		}

		// write every matching line to the file stream
		if _, err := fmt.Fprintln(f, rawLine); err != nil {
			fmt.Println(err)
			continue
		}
		if parsedLine.isAtLeastWarningLevel() {
			importantLogsBuffer[importantLogsBufferIdx] = rawLine
			importantLogsIdx++
			// `bufferIdx` walks from `0` to `capLines-1` and then to `0` in a cycle
			importantLogsBufferIdx = importantLogsIdx % capLines
		}
	}

	// print the last `capLines` lines of logs to the `output`
	_ = writeInlineOutput(importantLogsIdx, importantLogsBufferIdx, capLines, importantLogsBuffer, output)

	if scanErr := scanner.Err(); scanErr != nil {
		if _, err := fmt.Fprintln(output, "ERROR while scanning:", scanErr); err != nil {
			fmt.Println(err)
		}
	}
}

// GetOperatorLogs collects the operator logs
func (env TestingEnvironment) GetOperatorLogs(buf *bytes.Buffer) error {
	operatorPod, err := env.GetOperatorPod()
	if err != nil {
		return err
	}

	streamPodLog := logs.StreamingRequest{
		Pod: &operatorPod,
		Options: &corev1.PodLogOptions{
			Timestamps: false,
			Follow:     false,
		},
		Client: env.Interface,
	}
	return streamPodLog.Stream(env.Ctx, buf)
}

// DumpNamespaceOperatorLogs writes the operator logs related to a namespace into a writer
// and also into a file
func (env TestingEnvironment) DumpNamespaceOperatorLogs(namespace, testName string, output io.Writer) {
	var buf bytes.Buffer
	err := env.GetOperatorLogs(&buf)
	if err != nil {
		return
	}
	capLines := 5
	sanitizedTestName := strings.ReplaceAll(testName, " ", "_")
	saveNamespaceLogs(&buf, "operator_logs", sanitizedTestName, namespace, output, capLines)
}

// CleanupNamespace does cleanup duty related to the tear-down of a namespace,
// and is intended to be called in a DeferCleanup clause
func (env TestingEnvironment) CleanupNamespace(
	namespace string,
	testName string,
	testFailed bool,
	output io.Writer,
) error {
	if testFailed {
		env.DumpNamespaceOperatorLogs(namespace, testName, output)
		env.DumpNamespaceObjects(namespace, "out/"+testName+".log")
	}

	if len(namespace) == 0 {
		return fmt.Errorf("namespace is empty")
	}
	exists, _ := fileutils.FileExists("cluster_logs/" + namespace)
	if exists && !testFailed {
		err := fileutils.RemoveDirectory("cluster_logs/" + namespace)
		if err != nil {
			return err
		}
	}

	return env.DeleteNamespace(namespace)
}

// CreateUniqueTestNamespace creates a namespace by using the passed prefix.
// Return the namespace name and any errors encountered.
// The namespace is automatically cleaned up at the end of the test.
func (env TestingEnvironment) CreateUniqueTestNamespace(
	namespacePrefix string,
	opts ...client.CreateOption,
) (string, error) {
	name := env.createdNamespaces.generateUniqueName(namespacePrefix)

	return name, env.CreateTestNamespace(name, opts...)
}

// CreateTestNamespace creates a namespace creates a namespace.
// Prefer CreateUniqueTestNamespace instead, unless you need a
// specific namespace name. If so, make sure there is no collision
// potential.
// The namespace is automatically cleaned up at the end of the test.
func (env TestingEnvironment) CreateTestNamespace(
	name string,
	opts ...client.CreateOption,
) error {
	err := env.CreateNamespace(name, opts...)
	if err != nil {
		return err
	}

	ginkgo.DeferCleanup(func() error {
		return env.CleanupNamespace(
			name,
			ginkgo.CurrentSpecReport().LeafNodeText,
			ginkgo.CurrentSpecReport().Failed(),
			ginkgo.GinkgoWriter,
		)
	})

	return nil
}

// CreateNamespace creates a namespace.
func (env TestingEnvironment) CreateNamespace(name string, opts ...client.CreateOption) error {
	// Exit immediately if the name is empty
	if name == "" {
		return errors.New("cannot create namespace with empty name")
	}

	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})
	_, err := CreateObject(&env, u, opts...)
	return err
}

// EnsureNamespace checks for the presence of a namespace, and if it does not
// exist, creates it
func (env TestingEnvironment) EnsureNamespace(namespace string) error {
	var nsList corev1.NamespaceList
	err := GetObjectList(&env, &nsList)
	if err != nil {
		return err
	}
	for _, ns := range nsList.Items {
		if ns.Name == namespace {
			return nil
		}
	}
	return env.CreateNamespace(namespace)
}

// DeleteNamespace deletes a namespace if existent
func (env TestingEnvironment) DeleteNamespace(name string, opts ...client.DeleteOption) error {
	// Exit immediately if the name is empty
	if name == "" {
		return errors.New("cannot delete namespace with empty name")
	}

	// Exit immediately if the namespace is listed in PreserveNamespaces
	for _, v := range env.PreserveNamespaces {
		if strings.HasPrefix(name, v) {
			return nil
		}
	}

	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	return DeleteObject(&env, u, opts...)
}

// DeleteNamespaceAndWait deletes a namespace if existent and returns when deletion is completed
func (env TestingEnvironment) DeleteNamespaceAndWait(name string, timeoutSeconds int) error {
	// Exit immediately if the namespace is listed in PreserveNamespaces
	for _, v := range env.PreserveNamespaces {
		if strings.HasPrefix(name, v) {
			return nil
		}
	}

	_, _, err := Run(fmt.Sprintf("kubectl delete namespace %v --wait=true --timeout %vs", name, timeoutSeconds))

	return err
}
