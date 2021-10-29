/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package e2e

import (
	"testing"
	"time"

	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	"github.com/thoas/go-funk"

	// +kubebuilder:scaffold:imports
	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/tests"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	samplesDir  = "../../docs/src/samples"
	fixturesDir = "./fixtures"
)

var (
	env                     *tests.TestingEnvironment
	testLevelEnv            *tests.TestEnvLevel
	expectedOperatorPodName string
)

var _ = BeforeSuite(func() {
	var err error
	env, err = tests.NewTestingEnvironment()
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
	if len(breakingLabelsInCurrentTest.([]string)) == 0 {
		operatorPod, err := env.GetOperatorPod()
		Expect(err).NotTo(HaveOccurred())
		expectedOperatorPodName = operatorPod.GetName()
	}
})

func TestE2ESuite(t *testing.T) {
	RegisterFailHandler(Fail)
	SetDefaultEventuallyPollingInterval(200 * time.Millisecond)
	RunSpecs(t, "Cloud Native PostgreSQL Operator E2E")
}

// Before the end of the tests we should verify that the operator never restarted
// and that the operator pod name didn't change.
var _ = AfterEach(func() {
	labelsForTestsBreakingTheOperator := []string{"upgrade", "disruptive"}
	breakingLabelsInCurrentTest := funk.Join(CurrentSpecReport().Labels(),
		labelsForTestsBreakingTheOperator, funk.InnerJoin)
	if len(breakingLabelsInCurrentTest.([]string)) == 0 {
		AssertOperatorPodUnchanged(expectedOperatorPodName)
	}
})
