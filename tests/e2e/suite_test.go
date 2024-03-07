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

package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2/types"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	// +kubebuilder:scaffold:imports
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	fixturesDir         = "./fixtures"
	RetryTimeout        = utils.RetryTimeout
	PollingTime         = utils.PollingTime
	psqlClientNamespace = "psql-client-namespace"
)

var (
	env                     *utils.TestingEnvironment
	testLevelEnv            *tests.TestEnvLevel
	testCloudVendorEnv      *utils.TestEnvVendor
	psqlClientPod           *corev1.Pod
	expectedOperatorPodName string
	operatorPodWasRenamed   bool
	operatorWasRestarted    bool
	operatorLogDumped       bool
	quickDeletionPeriod     = int64(1)
	testTimeouts            map[utils.Timeout]int
	minioEnv                = &utils.MinioEnv{
		Namespace:    "minio",
		ServiceName:  "minio-service.minio",
		CaSecretName: "minio-server-ca-secret",
		TLSSecret:    "minio-server-tls-secret",
	}
)

var _ = SynchronizedBeforeSuite(func() []byte {
	var err error
	env, err = utils.NewTestingEnvironment()
	Expect(err).ShouldNot(HaveOccurred())

	psqlPod, err := utils.GetPsqlClient(psqlClientNamespace, env)
	Expect(err).ShouldNot(HaveOccurred())
	DeferCleanup(func() {
		err := env.DeleteNamespaceAndWait(psqlClientNamespace, 300)
		Expect(err).ToNot(HaveOccurred())
	})

	// Set up a global MinIO service on his own namespace
	err = env.CreateNamespace(minioEnv.Namespace)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		err := env.DeleteNamespaceAndWait(minioEnv.Namespace, 300)
		Expect(err).ToNot(HaveOccurred())
	})
	minioEnv.Timeout = uint(testTimeouts[utils.MinioInstallation])
	minioClient, err := utils.MinioDeploy(minioEnv, env)
	Expect(err).ToNot(HaveOccurred())

	caSecret := minioEnv.CaPair.GenerateCASecret(minioEnv.Namespace, minioEnv.CaSecretName)
	minioEnv.CaSecretObj = *caSecret
	objs := map[string]corev1.Pod{
		"psql":  *psqlPod,
		"minio": *minioClient,
	}

	jsonObjs, err := json.Marshal(objs)
	if err != nil {
		panic(err)
	}

	return jsonObjs
}, func(jsonObjs []byte) {
	var err error
	// We are creating new testing env object again because above testing env can not serialize and
	// accessible to all nodes (specs)
	if env, err = utils.NewTestingEnvironment(); err != nil {
		panic(err)
	}

	_ = k8sscheme.AddToScheme(env.Scheme)
	_ = apiv1.AddToScheme(env.Scheme)

	if testLevelEnv, err = tests.TestLevel(); err != nil {
		panic(err)
	}

	if testTimeouts, err = utils.Timeouts(); err != nil {
		panic(err)
	}

	if testCloudVendorEnv, err = utils.TestCloudVendor(); err != nil {
		panic(err)
	}

	var objs map[string]*corev1.Pod
	if err := json.Unmarshal(jsonObjs, &objs); err != nil {
		panic(err)
	}

	psqlClientPod = objs["psql"]
	minioEnv.Client = objs["minio"]
})

var _ = SynchronizedAfterSuite(func() {
}, func() {
})

// saveLogs does 2 things:
//   - displays the last `capLines` of error/warning logs on the `output` io.Writer (likely GinkgoWriter)
//   - saves the full logs to a file
//
// along the way it parses the timestamps for convenience, BUT the lines
// of output are not legal JSON
func saveLogs(buf *bytes.Buffer, logsType, specName string, output io.Writer, capLines int) {
	scanner := bufio.NewScanner(buf)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	filename := fmt.Sprintf("out/%s_%s.log", logsType, specName)
	f, err := os.Create(filepath.Clean(filename))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		syncErr := f.Sync()
		if syncErr != nil {
			fmt.Fprintln(output, "ERROR while flushing file:", syncErr)
		}
		closeErr := f.Close()
		if closeErr != nil {
			fmt.Fprintln(output, "ERROR while closing file:", err)
		}
	}()

	// circular buffer to hold the last `capLines` of non-DEBUG operator logs
	lineBuffer := make([]string, capLines)
	// count of lines to be shown in Ginkgo console (error or warning logs)
	linesToShow := 0
	// insertion point in the lineBuffer: values 0 to capLines - 1 (i.e. modulo capLines)
	bufferIdx := 0

	for scanner.Scan() {
		lg := scanner.Text()

		var js map[string]interface{}
		err = json.Unmarshal([]byte(lg), &js)
		if err != nil {
			fmt.Fprintln(output, "ERROR parsing log:", err, lg)
		}
		timestamp, ok := js["ts"].(float64)
		if ok {
			ts := time.UnixMicro(int64(math.Floor(timestamp * 1000000)))
			lg = ts.Format(time.Stamp) + " - " + lg
		}

		// store the latest line of error or warning log to the slice

		if js["level"] == log.WarningLevelString || js["level"] == log.ErrorLevelString {
			lineBuffer[bufferIdx] = lg
			linesToShow++
			// `bufferIdx` walks from `0` to `capLines-1` and then to `0` in a cycle
			bufferIdx = linesToShow % capLines
		}
		// write every line to the file stream
		fmt.Fprintln(f, lg)
	}

	// print the last `capLines` lines of logs to the `output`
	switch {
	case linesToShow == 0:
		fmt.Fprintln(output, "-- no error / warning logs --")
	case linesToShow <= capLines:
		fmt.Fprintln(output, strings.Join(lineBuffer[:linesToShow], "\n"))
	case bufferIdx == 0:
		// if bufferIdx == 0, the buffer just finished filling and is in order
		fmt.Fprintln(output, strings.Join(lineBuffer, "\n"))
	default:
		// the line buffer cycled back and the items 0 to bufferIdx - 1 are newer than the rest
		fmt.Fprintln(output, strings.Join(append(lineBuffer[bufferIdx:], lineBuffer[:bufferIdx]...), "\n"))
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintln(output, "ERROR while scanning:", err)
	}
}

var _ = BeforeEach(func() {
	labelsForTestsBreakingTheOperator := []string{"upgrade", "disruptive"}
	breakingLabelsInCurrentTest := funk.Join(CurrentSpecReport().Labels(),
		labelsForTestsBreakingTheOperator, funk.InnerJoin)

	if len(breakingLabelsInCurrentTest.([]string)) != 0 {
		return
	}

	operatorPod, err := env.GetOperatorPod()
	Expect(err).ToNot(HaveOccurred())

	GinkgoWriter.Println("Putting Tail on the operator log")
	var buf bytes.Buffer
	go func() {
		// get logs without timestamp parsing; for JSON parseability
		err = logs.TailPodLogs(context.TODO(), env.Interface, operatorPod, &buf, false)
		if err != nil {
			_, _ = fmt.Fprintf(&buf, "Error tailing logs, dumping operator logs: %v\n", err)
		}
	}()
	DeferCleanup(func(_ SpecContext) {
		if CurrentSpecReport().Failed() {
			specName := CurrentSpecReport().FullText()
			capLines := 10
			GinkgoWriter.Printf("DUMPING tailed Operator Logs with error/warning (at most %v lines ). Failed Spec: %v\n",
				capLines, specName)
			GinkgoWriter.Println("================================================================================")
			saveLogs(&buf, "operator_logs", strings.ReplaceAll(specName, " ", "_"), GinkgoWriter, capLines)
			GinkgoWriter.Println("================================================================================")
		}
	})

	if operatorPodWasRenamed {
		Skip("Skipping test. Operator was renamed")
	}
	if operatorWasRestarted {
		Skip("Skipping test. Operator was restarted")
	}

	expectedOperatorPodName = operatorPod.GetName()
})

func TestE2ESuite(t *testing.T) {
	RegisterFailHandler(Fail)
	SetDefaultEventuallyPollingInterval(1 * time.Second)
	RunSpecs(t, "CloudNativePG Operator E2E")
}

// Before the end of the tests we should verify that the operator never restarted
// and that the operator pod name didn't change.
// If either of those things happened, the test will fail, and all subsequent
// tests will be SKIPPED, as they would always fail in this node.
var _ = AfterEach(func() {
	if CurrentSpecReport().State.Is(types.SpecStateSkipped) {
		return
	}
	labelsForTestsBreakingTheOperator := []string{"upgrade", "disruptive"}
	breakingLabelsInCurrentTest := funk.Join(CurrentSpecReport().Labels(),
		labelsForTestsBreakingTheOperator, funk.InnerJoin)
	if len(breakingLabelsInCurrentTest.([]string)) != 0 {
		return
	}
	operatorPod, err := env.GetOperatorPod()
	Expect(err).ToNot(HaveOccurred())
	wasRenamed := utils.OperatorPodRenamed(operatorPod, expectedOperatorPodName)
	if wasRenamed {
		operatorPodWasRenamed = true
		Fail("operator was renamed")
	}
	wasRestarted := utils.OperatorPodRestarted(operatorPod)
	if wasRestarted {
		if !operatorLogDumped {
			// get the PREVIOUS operator logs
			requestedLineLength := 10
			lines, err := env.DumpOperatorLogs(wasRestarted, requestedLineLength)
			if err == nil {
				operatorLogDumped = true
				// print out a sample of the last `requestedLineLength` lines of logs
				GinkgoWriter.Println("DUMPING previous operator log due to operator restart:")
				for _, line := range lines {
					GinkgoWriter.Println(line)
				}
			} else {
				GinkgoWriter.Printf("Failed getting the latest operator logs: %v\n", err)
			}
		}
		operatorWasRestarted = true
		Fail("operator was restarted")
	}
})
