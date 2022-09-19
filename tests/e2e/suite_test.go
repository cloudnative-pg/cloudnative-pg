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
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2/types"
	"github.com/thoas/go-funk"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	// +kubebuilder:scaffold:imports
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
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

var _ = BeforeEach(func() {
	labelsForTestsBreakingTheOperator := []string{"upgrade", "disruptive"}
	breakingLabelsInCurrentTest := funk.Join(CurrentSpecReport().Labels(),
		labelsForTestsBreakingTheOperator, funk.InnerJoin)

	if len(breakingLabelsInCurrentTest.([]string)) != 0 {
		return
	}

	operatorPod, err := env.GetOperatorPod()
	Expect(err).ToNot(HaveOccurred())

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
