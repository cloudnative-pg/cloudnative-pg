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
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2/types"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	// +kubebuilder:scaffold:imports
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/sternmultitailer"

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

	// Start stern to write the logs of every single pod we create under cluster_logs
	sternCtx, sternCancel := context.WithCancel(env.Ctx)
	done := env.SternClusters.CatchClusterLogs(sternCtx, env.Interface)
	DeferCleanup(func() {
		sternCancel()
		<-done
	})

	// Start stern on operator pods
	sternOperatorCtx, sternOperatorCancel := context.WithCancel(env.Ctx)
	operatorDoneChan := env.SternOperator.CatchOperatorLogs(sternOperatorCtx, env.Interface)
	DeferCleanup(func() {
		sternOperatorCancel()
		<-operatorDoneChan
	})

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

var _ = ReportAfterSuite("Gathering failed reports", func(report Report) {
	if report.SuiteSucceeded {
		err := fileutils.RemoveDirectory(sternmultitailer.OperatorLogsDirectory)
		Expect(err).ToNot(HaveOccurred())
		err = fileutils.RemoveDirectory(sternmultitailer.ClusterLogsDirectory)
		Expect(err).ToNot(HaveOccurred())
	}
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
		operatorWasRestarted = true
		Fail("operator was restarted")
	}
})
