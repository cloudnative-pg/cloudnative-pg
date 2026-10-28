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
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgbouncerasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/pgbouncer"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/opentelemetry"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/operator"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OpenTelemetry log export", Label(tests.LabelObservability), func() {
	const (
		clusterFile     = fixturesDir + "/opentelemetry/cluster-opentelemetry.yaml.template"
		poolerFile      = fixturesDir + "/opentelemetry/pooler-opentelemetry.yaml"
		clusterName     = "cluster-opentelemetry"
		poolerName      = "pooler-opentelemetry"
		namespacePrefix = "opentelemetry-e2e"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	It("exports the logs of the instances and of PgBouncer", func() {
		namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
		Expect(err).ToNot(HaveOccurred())
		Expect(opentelemetry.Deploy(env.Ctx, env.Client, namespace, 240)).To(Succeed())

		clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterFile)
		pgbouncerasserts.AssertPgBouncerPoolerIsSetUp(env, namespace, poolerFile, 1)

		primary, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		primaryLocator := exec.PodLocator{Namespace: namespace, PodName: primary.Name}

		By("exporting the PostgreSQL records as structured attributes", func() {
			const errorTestQuery = "selecct 1"
			_, _, err := exec.QueryInInstancePod(env.Ctx, env.Client, env.Interface, env.RestClientConfig,
				primaryLocator, postgres.AppDBName, errorTestQuery)
			Expect(err).To(HaveOccurred())

			Eventually(func(g Gomega) {
				g.Expect(exportedPodRecords(g, namespace, primary.Name)).To(ContainElement(SatisfyAll(
					HaveField("Scope", "postgres"),
					HaveField("Attributes", HaveKeyWithValue("record", SatisfyAll(
						HaveKeyWithValue("error_severity", "ERROR"),
						HaveKeyWithValue("query", errorTestQuery),
					))),
				)))
			}, 60).Should(Succeed())
		})

		By("forwarding the records of wal-archive", func() {
			// An unreachable object store makes wal-archive log an error
			cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(objects.Patch(env.Ctx, env.Client, cluster, client.RawPatch(types.MergePatchType, []byte(
				`{"spec":{"backup":{"barmanObjectStore":{"destinationPath":"s3://unreachable/",`+
					`"endpointURL":"http://127.0.0.1:1","s3Credentials":{"inheritFromIAMRole":true}}}}}`,
			)))).To(Succeed())

			Eventually(func(g Gomega) {
				_, _, err := exec.QueryInInstancePod(env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					primaryLocator, postgres.PostgresDBName, "CHECKPOINT; SELECT pg_catalog.pg_switch_wal()")
				g.Expect(err).ToNot(HaveOccurred())

				g.Expect(exportedPodRecords(g, namespace, primary.Name)).To(ContainElement(SatisfyAll(
					HaveField("Scope", "wal-archive"),
					HaveField("Attributes", HaveKey("exception.message")),
				)))
			}, 120, 10).Should(Succeed())

			Expect(objects.Patch(env.Ctx, env.Client, cluster, client.RawPatch(types.MergePatchType,
				[]byte(`{"spec":{"backup":null}}`)))).To(Succeed())
		})

		By("exporting the PgBouncer records as structured attributes", func() {
			poolerPods := &corev1.PodList{}
			Expect(objects.List(env.Ctx, env.Client, poolerPods, client.InNamespace(namespace),
				client.MatchingLabels{utils.PgbouncerNameLabel: poolerName})).To(Succeed())
			Expect(poolerPods.Items).To(HaveLen(1))

			Eventually(func(g Gomega) {
				g.Expect(exportedPodRecords(g, namespace, poolerPods.Items[0].Name)).To(ContainElement(
					HaveField("Attributes", HaveKeyWithValue("record", HaveKey("msg"))),
				))
			}, 60).Should(Succeed())

			assertContainerLogsAreExported(namespace, &poolerPods.Items[0], "pgbouncer")
		})

		By("exporting every record written by the instances", func() {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(podList.Items).To(HaveLen(2))
			for i := range podList.Items {
				// The bootstrap-instance init container runs the init and join
				// subcommands, which export their last records before exiting
				assertContainerLogsAreExported(namespace, &podList.Items[i],
					specs.BootstrapWorkContainerName, specs.PostgresContainerName)
			}
		})
	})
})

var _ = Describe("OpenTelemetry log export of the operator", Serial,
	Label(tests.LabelDisruptive, tests.LabelNoOpenshift, tests.LabelObservability), func() {
		const (
			namespacePrefix = "opentelemetry-operator-e2e"
			level           = tests.Medium
		)

		BeforeEach(func() {
			if testLevelEnv.Depth < int(level) {
				Skip("Test depth is lower than the amount requested for this test")
			}
		})

		It("exports the logs of the operator", func() {
			namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			Expect(opentelemetry.Deploy(env.Ctx, env.Client, namespace, 240)).To(Succeed())

			By("configuring the operator to export its logs via HTTP", func() {
				deployment, err := operator.GetDeployment(env.Ctx, env.Client)
				Expect(err).ToNot(HaveOccurred())
				originalEnv := deployment.Spec.Template.Spec.Containers[0].Env
				setOperatorEnv(append(slices.Clone(originalEnv),
					corev1.EnvVar{
						Name: "K8S_POD_NAME",
						ValueFrom: &corev1.EnvVarSource{
							FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
						},
					},
					corev1.EnvVar{
						Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
						Value: fmt.Sprintf("http://%s.%s.svc:4318", opentelemetry.CollectorName, namespace),
					},
					corev1.EnvVar{
						Name:  "OTEL_RESOURCE_ATTRIBUTES",
						Value: "k8s.pod.name=$(K8S_POD_NAME)",
					},
				))
				DeferCleanup(setOperatorEnv, originalEnv)
			})

			By("exporting every record written by the operator", func() {
				operatorPod, err := operator.GetPod(env.Ctx, env.Client)
				Expect(err).ToNot(HaveOccurred())
				assertContainerLogsAreExported(namespace, &operatorPod, "manager")
			})
		})
	})

// setOperatorEnv sets the environment of the operator container, and waits
// for the rollout of the operator
func setOperatorEnv(envVars []corev1.EnvVar) {
	GinkgoHelper()
	deployment, err := operator.GetDeployment(env.Ctx, env.Client)
	Expect(err).ToNot(HaveOccurred())
	updated := deployment.DeepCopy()
	updated.Spec.Template.Spec.Containers[0].Env = envVars
	Expect(objects.Patch(env.Ctx, env.Client, updated, client.MergeFrom(&deployment))).To(Succeed())
	Expect(operator.WaitForReady(env.Ctx, env.Client, 120, true)).To(Succeed())
}

// exportedPodRecords returns the records that the collector in the given
// namespace received from a pod
func exportedPodRecords(g Gomega, collectorNamespace, podName string) []opentelemetry.Record {
	records, err := opentelemetry.Records(collectorNamespace)
	g.Expect(err).ToNot(HaveOccurred())

	var result []opentelemetry.Record
	for _, record := range records {
		if record.Resource["k8s.pod.name"] == podName {
			result = append(result, record)
		}
	}
	return result
}

// assertContainerLogsAreExported verifies that the collector in the given
// namespace receives every record that the containers of a pod write on
// their standard output, and no other one. The records are matched by
// message, as the forwarded ones, such as those of wal-archive, are exported
// with the time they were forwarded at.
func assertContainerLogsAreExported(collectorNamespace string, pod *corev1.Pod, containers ...string) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		exportedBefore := exportedPodRecords(g, collectorNamespace, pod.Name)
		var output []byte
		for _, container := range containers {
			containerOutput, err := env.Interface.CoreV1().Pods(pod.Namespace).
				GetLogs(pod.Name, &corev1.PodLogOptions{Container: container}).
				DoRaw(env.Ctx)
			g.Expect(err).ToNot(HaveOccurred())
			output = append(output, containerOutput...)
		}
		readTime := time.Now()
		exportedAfter := exportedPodRecords(g, collectorNamespace, pod.Name)

		written := make(map[string]int)
		// The most recent records are left the time to be exported
		writtenBefore := make(map[string]int)
		for _, line := range strings.Split(string(output), "\n") {
			var entry struct {
				Message string    `json:"msg"`
				Time    time.Time `json:"ts"`
			}
			// Only the JSON records are written by the logger, and exported
			if json.Unmarshal([]byte(line), &entry) != nil {
				continue
			}
			written[entry.Message]++
			if entry.Time.Before(readTime.Add(-10 * time.Second)) {
				writtenBefore[entry.Message]++
			}
		}
		g.Expect(writtenBefore).ToNot(BeEmpty())

		for message, count := range countByMessage(exportedBefore) {
			g.Expect(written[message]).To(BeNumerically(">=", count),
				"exported record not written on the standard output: %q", message)
		}
		exported := countByMessage(exportedAfter)
		for message, count := range writtenBefore {
			g.Expect(exported[message]).To(BeNumerically(">=", count),
				"record written on the standard output and not exported: %q", message)
		}
	}, 120, 10).Should(Succeed())
}

func countByMessage(records []opentelemetry.Record) map[string]int {
	result := make(map[string]int)
	for _, record := range records {
		message, _ := record.Body.(string)
		result[message]++
	}
	return result
}
