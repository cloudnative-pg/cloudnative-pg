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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2/types"
	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"

	// +kubebuilder:scaffold:imports
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	cnpgUtils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/cloudvendors"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/minio"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/sternmultitailer"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	fixturesDir  = "./fixtures"
	RetryTimeout = environment.RetryTimeout
	PollingTime  = objects.PollingTime
)

var (
	env                     *environment.TestingEnvironment
	testLevelEnv            *tests.TestEnvLevel
	testCloudVendorEnv      *cloudvendors.TestEnvVendor
	expectedOperatorPodName string
	operatorPodWasRenamed   bool
	operatorWasRestarted    bool
	quickDeletionPeriod     = int64(1)
	testTimeouts            map[timeouts.Timeout]int
	minioEnv                = &minio.Env{
		Namespace:    "minio",
		ServiceName:  "minio-service.minio",
		CaSecretName: "minio-server-ca-secret",
		TLSSecret:    "minio-server-tls-secret",
	}
)

var _ = SynchronizedBeforeSuite(func() []byte {
	var err error
	env, err = environment.NewTestingEnvironment()
	Expect(err).ShouldNot(HaveOccurred())

	display := func(s string) string {
		if strings.TrimSpace(s) == "" {
			return "<none>"
		}
		return s
	}
	_, _ = fmt.Fprintf(GinkgoWriter, `
E2E test configuration:
  Postgres image:                %s:%s
  Postgres version:              %d
  Postgres image repository:     %s
  PostGIS image repository:      %s
  Pre-rolling update image:      %s
  Cloud vendor:                  %s
  Default storage class:         %s
  CSI storage class:             %s
  Default volume snapshot class: %s
`,
		env.PostgresImageName, env.PostgresImageTag,
		env.PostgresVersion,
		display(env.PostgresImageRepository),
		display(env.PostGISImageRepository),
		display(os.Getenv("E2E_PRE_ROLLING_UPDATE_IMG")),
		display(os.Getenv("TEST_CLOUD_VENDOR")),
		display(env.DefaultStorageClass),
		display(env.CSIStorageClass),
		display(env.DefaultVolumeSnapshotClass),
	)

	// Export detected storage class values as environment variables for
	// code that uses os.Getenv (e.g. MinIO PVC setup).
	for k, v := range map[string]string{
		"E2E_DEFAULT_STORAGE_CLASS":        env.DefaultStorageClass,
		"E2E_CSI_STORAGE_CLASS":            env.CSIStorageClass,
		"E2E_DEFAULT_VOLUMESNAPSHOT_CLASS": env.DefaultVolumeSnapshotClass,
	} {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}

	// Start stern to write the logs of every pod we are interested in. Since we don't have a way to have a selector
	// matching both the operator's and the clusters' pods, we need to start stern twice.
	sternClustersCtx, sternClusterCancel := context.WithCancel(env.Ctx)
	sternClusterDoneChan := sternmultitailer.StreamLogs(sternClustersCtx, env.Interface, clusterPodsLabelSelector(),
		namespaces.SternLogDirectory)
	DeferCleanup(func() {
		sternClusterCancel()
		<-sternClusterDoneChan
	})
	sternOperatorCtx, sternOperatorCancel := context.WithCancel(env.Ctx)
	sternOperatorDoneChan := sternmultitailer.StreamLogs(sternOperatorCtx, env.Interface, operatorPodsLabelSelector(),
		namespaces.SternLogDirectory)
	DeferCleanup(func() {
		sternOperatorCancel()
		<-sternOperatorDoneChan
	})

	_ = corev1.AddToScheme(env.Scheme)
	_ = appsv1.AddToScheme(env.Scheme)

	// Set up a global MinIO service on his own namespace
	err = namespaces.CreateNamespace(env.Ctx, env.Client, minioEnv.Namespace)
	Expect(err).ToNot(HaveOccurred())
	DeferCleanup(func() {
		err := namespaces.DeleteNamespaceAndWait(env.Ctx, env.Client, minioEnv.Namespace, 300)
		Expect(err).ToNot(HaveOccurred())
	})
	minioEnv.Timeout = uint(testTimeouts[timeouts.MinioInstallation])
	minioClient, err := minio.Deploy(minioEnv, env)
	Expect(err).ToNot(HaveOccurred())

	caSecret := minioEnv.CaPair.GenerateCASecret(minioEnv.Namespace, minioEnv.CaSecretName)
	minioEnv.CaSecretObj = *caSecret
	objs := map[string]corev1.Pod{
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
	if env, err = environment.NewTestingEnvironment(); err != nil {
		panic(err)
	}

	// Export detected storage class values as environment variables for
	// backward compatibility with test code that uses os.Getenv and
	// YAML template substitution via envsubst.
	for k, v := range map[string]string{
		"E2E_DEFAULT_STORAGE_CLASS":        env.DefaultStorageClass,
		"E2E_CSI_STORAGE_CLASS":            env.CSIStorageClass,
		"E2E_DEFAULT_VOLUMESNAPSHOT_CLASS": env.DefaultVolumeSnapshotClass,
	} {
		if err := os.Setenv(k, v); err != nil {
			panic(err)
		}
	}

	_ = k8sscheme.AddToScheme(env.Scheme)
	_ = apiv1.AddToScheme(env.Scheme)

	if testLevelEnv, err = tests.TestLevel(); err != nil {
		panic(err)
	}

	if testTimeouts, err = timeouts.Timeouts(); err != nil {
		panic(err)
	}

	if testCloudVendorEnv, err = cloudvendors.TestCloudVendor(); err != nil {
		panic(err)
	}

	var objs map[string]*corev1.Pod
	if err := json.Unmarshal(jsonObjs, &objs); err != nil {
		panic(err)
	}

	minioEnv.Client = objs["minio"]
})

var _ = BeforeEach(func() {
	labelsForTestsBreakingTheOperator := []string{"upgrade", "disruptive"}
	breakingLabelsInCurrentTest := funk.Join(CurrentSpecReport().Labels(),
		labelsForTestsBreakingTheOperator, funk.InnerJoin)

	if len(breakingLabelsInCurrentTest.([]string)) != 0 {
		return
	}

	operatorPod, err := operator.GetPod(env.Ctx, env.Client)
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
	operatorPod, err := operator.GetPod(env.Ctx, env.Client)
	Expect(err).ToNot(HaveOccurred())
	wasRenamed := operator.PodRenamed(operatorPod, expectedOperatorPodName)
	if wasRenamed {
		operatorPodWasRenamed = true
		Fail("operator was renamed")
	}
	wasRestarted := operator.PodRestarted(operatorPod)
	if wasRestarted {
		operatorWasRestarted = true
		Fail("operator was restarted")
	}
})

// clusterPodsLabelSelector returns a label selector to match all the pods belonging to the CNPG clusters
func clusterPodsLabelSelector() labels.Selector {
	labelSelector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      cnpgUtils.ClusterLabelName,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	})
	return labelSelector
}

// operatorPodsLabelSelector returns a label selector to match all the pods belonging to the CNPG operator
func operatorPodsLabelSelector() labels.Selector {
	labelSelector, _ := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/name": "cloudnative-pg",
		},
	})
	return labelSelector
}
