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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2/types"
	"github.com/thoas/go-funk"
	"golang.org/x/net/context"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	// +kubebuilder:scaffold:imports
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	fixturesDir  = "./fixtures"
	RetryTimeout = utils.RetryTimeout
	PollingTime  = utils.PollingTime
)

var (
	env                     *utils.TestingEnvironment
	testLevelEnv            *tests.TestEnvLevel
	expectedOperatorPodName string
	operatorPodWasRenamed   bool
	operatorWasRestarted    bool
	operatorLogDumped       bool
)

var _ = BeforeSuite(func() {
	var err error
	env, err = utils.NewTestingEnvironment()
	if err != nil {
		panic(err)
	}
	testLevelEnv, err = tests.TestLevel()
	if err != nil {
		panic(err)
	}
	_ = k8sscheme.AddToScheme(env.Scheme)
	_ = apiv1.AddToScheme(env.Scheme)
})

// saveOperatorLogs does 2 things:
//   - displays the non-DEBUG operator logs as part of the Ginkgo output
//   - saves the full logs to a file
//
// along the way it parses the timestamps for convenience, BUT the lines
// of output are not legal JSON
func saveOperatorLogs(buf bytes.Buffer, specName string) {
	scanner := bufio.NewScanner(&buf)
	filename := "out/operator_logs_" + specName + ".log"
	f, err := os.Create(filepath.Clean(filename))
	if err != nil {
		fmt.Println(err)
		return
	}
	defer func() {
		syncErr := f.Sync()
		if syncErr != nil {
			fmt.Fprintln(GinkgoWriter, "ERROR while flushing file:", syncErr)
		}
		closeErr := f.Close()
		if closeErr != nil {
			fmt.Fprintln(GinkgoWriter, "ERROR while closing file:", err)
		}
	}()
	for scanner.Scan() {
		lg := scanner.Text()
		var js map[string]interface{}
		err = json.Unmarshal([]byte(lg), &js)
		if err != nil {
			GinkgoWriter.Println("Error parsing log:", err, lg)
		}
		timestamp, ok := js["ts"].(float64)
		if ok {
			ts := time.UnixMicro(int64(timestamp * 1000000))
			lg = ts.Format(time.Stamp) + " - " + lg
		}

		if js["level"] != "debug" {
			fmt.Fprintln(GinkgoWriter, lg)
		}
		fmt.Fprintln(f, lg)
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintln(GinkgoWriter, "ERROR while scanning:", err)
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
		err = logs.TailPodLogs(context.TODO(), operatorPod, &buf)
		if err != nil {
			_, _ = fmt.Fprintf(&buf, "Error dumping operator logs: %v\n", err)
		}
	}()
	DeferCleanup(func(ctx SpecContext) {
		if CurrentSpecReport().Failed() {
			GinkgoWriter.Println("DUMPING Operator Logs. Failed Spec:",
				CurrentSpecReport().LeafNodeText)
			saveOperatorLogs(buf, CurrentSpecReport().LeafNodeText)
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
				GinkgoWriter.Println("DUMPING previous operator log:")
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
