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
	"bytes"
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tablespaces tests", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		clusterManifest = fixturesDir + "/tablespaces/cluster-with-tablespaces.yaml.template"
		level           = tests.Medium
		ERROR           = "error"
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("plain vanilla cluster", Ordered, func() {
		const (
			firstTablespace  = "atablespace"
			secondTablespace = "anothertablespace"
			namespacePrefix  = "tablespaces"
		)
		var clusterName, namespace string
		var cluster *apiv1.Cluster
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			clusterName, err = env.GetResourceNameFromYAML(clusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("creating a cluster and having it be ready", func() {
				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
			cluster, err = env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			clusterLogs := logs.ClusterStreamingRequest{
				Cluster: cluster,
				Options: &corev1.PodLogOptions{
					Follow: true,
				},
			}
			var buffer bytes.Buffer
			go func() {
				defer GinkgoRecover()
				err = clusterLogs.SingleStream(context.TODO(), &buffer)
				Expect(err).ToNot(HaveOccurred())
			}()

			DeferCleanup(func(ctx SpecContext) {
				if CurrentSpecReport().Failed() {
					specName := CurrentSpecReport().FullText()
					capLines := 10
					GinkgoWriter.Printf("DUMPING tailed CLUSTER Logs with error/warning (at most %v lines ). Failed Spec: %v\n",
						capLines, specName)
					GinkgoWriter.Println("================================================================================")
					saveLogs(&buffer, "cluster_logs_", strings.ReplaceAll(specName, " ", "_"), GinkgoWriter, capLines)
					GinkgoWriter.Println("================================================================================")
				}
			})
		})

		It("creates the PVCs and mount points required for tablespaces", func() {
			AssertClusterHasMountPointsAndVolumesForTablespaces(cluster)
			AssertClusterHasPvcsAndDataDirsForTablespaces(cluster)
			AssertDatabaseContainsTablespaces(cluster)
		})
	})
})

func AssertClusterHasMountPointsAndVolumesForTablespaces(cluster *apiv1.Cluster) {
	namespace := cluster.ObjectMeta.Namespace
	clusterName := cluster.ObjectMeta.Name
	By("checking the mount points and volumes in the pods", func() {
		Expect(cluster.ShouldCreateTablespaces()).To(BeTrue())
		Expect(cluster.Spec.Tablespaces).To(HaveLen(2))
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			Expect(pod.Spec.Containers).ToNot(BeEmpty())
			var hasPostgresContainer bool
			var mountPaths []string

			for _, ctr := range pod.Spec.Containers {
				if ctr.Name == "postgres" {
					hasPostgresContainer = true
					for _, mt := range ctr.VolumeMounts {
						mountPaths = append(mountPaths, mt.MountPath)
					}
				}
			}
			Expect(hasPostgresContainer).To(BeTrue())
			for tbsName := range cluster.Spec.Tablespaces {
				Expect(mountPaths).To(ContainElements(
					"/var/lib/postgresql/tablespaces/" + tbsName,
				))
			}

			var volumeNames []string
			var claimNames []string
			for _, vol := range pod.Spec.Volumes {
				volumeNames = append(volumeNames, vol.Name)
				if vol.PersistentVolumeClaim != nil {
					claimNames = append(claimNames, vol.PersistentVolumeClaim.ClaimName)
				}
			}
			for tbsName := range cluster.Spec.Tablespaces {
				Expect(volumeNames).To(ContainElement(
					tbsName,
				))
				Expect(claimNames).To(ContainElement(
					pod.Name + "-tbs-" + tbsName,
				))
			}
		}
	})
}

func AssertClusterHasPvcsAndDataDirsForTablespaces(cluster *apiv1.Cluster) {
	namespace := cluster.ObjectMeta.Namespace
	clusterName := cluster.ObjectMeta.Name
	By("checking all the required PVCs were created", func() {
		pvcList, err := env.GetPVCList(namespace)
		Expect(err).ShouldNot(HaveOccurred())
		var tablespacePvcNames []string
		for _, pvc := range pvcList.Items {
			roleLabel, found := pvc.Labels[utils.PvcRoleLabelName]
			Expect(found).To(BeTrue())
			if roleLabel != utils.PVCRolePgTablespace {
				continue
			}
			tablespacePvcNames = append(tablespacePvcNames, pvc.Name)
			tbsName := pvc.Labels[utils.TablespaceNameLabelName]
			Expect(tbsName).ToNot(BeEmpty())
			_, labelTbsInCluster := cluster.Spec.Tablespaces[tbsName]
			Expect(labelTbsInCluster).To(BeTrue())
			for tbs, config := range cluster.Spec.Tablespaces {
				if tbsName == tbs {
					Expect(pvc.Spec.Resources.Requests.Storage()).
						To(BeEquivalentTo(config.Storage.GetSizeOrNil()))
				}
			}
		}
		podList, err := env.GetClusterPodList(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			for tbsName := range cluster.Spec.Tablespaces {
				Expect(tablespacePvcNames).To(ContainElement(pod.Name + "-tbs-" + tbsName))
			}
		}
	})
	By("checking the data directory for the tablespaces is owned by postgres", func() {
		pvcList, err := env.GetPodList(namespace)
		Expect(err).ShouldNot(HaveOccurred())
		for _, pod := range pvcList.Items {
			for tbsName := range cluster.Spec.Tablespaces {
				dataDir := fmt.Sprintf("/var/lib/postgresql/tablespaces/%s/data", tbsName)
				owner, stdErr, err := env.ExecCommandInInstancePod(
					testUtils.PodLocator{
						Namespace: namespace,
						PodName:   pod.Name,
					}, nil,
					"stat", "-c", `'%U'`, dataDir,
				)
				Expect(stdErr).To(BeEmpty())
				Expect(err).ShouldNot(HaveOccurred())
				Expect(owner).To(ContainSubstring("postgres"))
			}
		}
	})
}

func AssertDatabaseContainsTablespaces(cluster *apiv1.Cluster) {
	namespace := cluster.ObjectMeta.Namespace
	clusterName := cluster.ObjectMeta.Name
	By("checking the expected tablespaces are in the database", func() {
		primary, err := env.GetClusterPrimary(namespace, clusterName)
		Expect(err).ShouldNot(HaveOccurred())
		tbsListing, stdErr, err := env.ExecQueryInInstancePod(
			testUtils.PodLocator{
				Namespace: namespace,
				PodName:   primary.Name,
			}, testUtils.DatabaseName("app"),
			"SELECT spcname FROM pg_tablespace;",
		)
		Expect(stdErr).To(BeEmpty())
		Expect(err).ShouldNot(HaveOccurred())
		for tbsName := range cluster.Spec.Tablespaces {
			Expect(tbsListing).To(ContainSubstring(tbsName))
		}
	})
}
