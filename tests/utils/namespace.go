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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	filename := fmt.Sprintf("out/%s_ns-%s_%s.log", logsType, namespace, specName)
	f, err := os.Create(filepath.Clean(filename))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		var err error
		syncErr := f.Sync()
		if syncErr != nil {
			_, err = fmt.Fprintln(output, "ERROR while flushing file:", syncErr)
		}
		if err != nil {
			fmt.Println(err)
		}
		closeErr := f.Close()
		if closeErr != nil {
			_, err = fmt.Fprintln(output, "ERROR while closing file:", err)
		}
		if err != nil {
			fmt.Println(err)
		}
	}()

	// circular buffer to hold the last `capLines` of non-DEBUG operator logs
	lineBuffer := make([]string, capLines)
	linesIdx := 0
	// insertion point in the lineBuffer: values 0 to capLines - 1 (i.e. modulo capLines)
	bufferIdx := 0

	for scanner.Scan() {
		lg := scanner.Text()

		var js map[string]interface{}
		err = json.Unmarshal([]byte(lg), &js)
		if err != nil {
			_, err = fmt.Fprintln(output, "ERROR parsing log:", err, lg)
			if err != nil {
				fmt.Println(err)
				continue
			}
		}

		isImportant := func(js map[string]interface{}) bool {
			return js["level"] == log.WarningLevelString || js["level"] == log.ErrorLevelString
		}

		// store the latest line of error or warning log to the slice,
		// output every line to the file
		if js["namespace"] == namespace {
			// write every matching line to the file stream
			_, err := fmt.Fprintln(f, lg)
			if err != nil {
				fmt.Println(err)
				continue
			}
			if isImportant(js) {
				lineBuffer[bufferIdx] = lg
				linesIdx++
				// `bufferIdx` walks from `0` to `capLines-1` and then to `0` in a cycle
				bufferIdx = linesIdx % capLines
			}
		}
	}

	// print the last `capLines` lines of logs to the `output`
	_ = writeInlineOutput(linesIdx, bufferIdx, capLines, lineBuffer, output)

	if err := scanner.Err(); err != nil {
		_, err := fmt.Fprintln(output, "ERROR while scanning:", err)
		if err != nil {
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
	return streamPodLog.Stream(context.TODO(), buf)
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
	return env.DeleteNamespace(namespace)
}

// CleanupNamespaceAndWait does cleanup just like CleanupNamespace, but waits for
// the namespace to be deleted, with a timeout
func (env TestingEnvironment) CleanupNamespaceAndWait(
	namespace string,
	testName string,
	testFailed bool,
	timeoutSeconds int,
	output io.Writer,
) error {
	lines, err := env.DumpOperatorLogs(false, 10)
	if err != nil {
		_, _ = fmt.Fprintf(output, "cleanupNamespace: error dumping opertor logs: %v\n", err)
	}
	_, _ = fmt.Fprintln(output, strings.Join(lines, "\n"))
	if testFailed {
		env.DumpNamespaceObjects(namespace, "out/"+testName+".log")
	}
	return env.DeleteNamespaceAndWait(namespace, timeoutSeconds)
}

// CreateUniqueNamespace creates a namespace by using the passed prefix.
// Return the namespace name and any errors encountered.
func (env TestingEnvironment) CreateUniqueNamespace(
	namespacePrefix string,
	opts ...client.CreateOption,
) (string, error) {
	name := env.createdNamespaces.generateUniqueName(namespacePrefix)

	return name, env.CreateNamespace(name, opts...)
}

// CreateNamespace creates a namespace.
// Prefer CreateUniqueNamespace instead, unless you need a
// specific namespace name. If so, make sure there is no collision
// potential
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
