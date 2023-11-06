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
	"sigs.k8s.io/controller-runtime/pkg/client"

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
		level            = tests.Medium
		ERROR            = "error"
		firstTablespace  = "atablespace"
		secondTablespace = "anothertablespace"
		namespacePrefix  = "tablespaces"
	)
	var (
		clusterName string
		namespace   string
		cluster     *apiv1.Cluster
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	clusterSetup := func(clusterManifest string) {
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
	}

	Context("new cluster with tablespaces", Ordered, func() {
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		clusterManifest := fixturesDir + "/tablespaces/cluster-with-tablespaces.yaml.template"
		BeforeAll(func() {
			clusterSetup(clusterManifest)
		})

		It("can verify tablespaces and PVC were created", func() {
			AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, testTimeouts[testUtils.Short])
			AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[testUtils.Short])
			AssertDatabaseContainsTablespaces(cluster, testTimeouts[testUtils.Short])
		})
	})

	Context("plain cluster", Ordered, func() {
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		clusterManifest := fixturesDir + "/tablespaces/cluster-without-tablespaces.yaml.template"
		BeforeAll(func() {
			clusterSetup(clusterManifest)
		})

		It("can update cluster adding tablespaces", func() {
			By("adding tablespaces to the spec and patching", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ShouldCreateTablespaces()).To(BeFalse())

				updated := cluster.DeepCopy()
				updated.Spec.Tablespaces = map[string]*apiv1.TablespaceConfiguration{
					"atablespace": {
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
					},
					"anothertablespace": {
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
					},
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())

				cluster, err = env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ShouldCreateTablespaces()).To(BeTrue())
			})
			By("waiting for the cluster to be ready", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReady], env)
			})
		})

		It("can verify tablespaces and PVC were created", func() {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.ShouldCreateTablespaces()).To(BeTrue())

			AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, testTimeouts[testUtils.PodRollout])
			AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[testUtils.PodRollout])
			AssertDatabaseContainsTablespaces(cluster, testTimeouts[testUtils.PodRollout])
		})
	})
})

func AssertClusterHasMountPointsAndVolumesForTablespaces(cluster *apiv1.Cluster, timeout int) {
	namespace := cluster.ObjectMeta.Namespace
	clusterName := cluster.ObjectMeta.Name
	podMountPaths := func(pod corev1.Pod) (bool, []string) {
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
		return hasPostgresContainer, mountPaths
	}

	By("checking the mount points and volumes in the pods", func() {
		Eventually(func(g Gomega) {
			g.Expect(cluster.ShouldCreateTablespaces()).To(BeTrue())
			g.Expect(cluster.Spec.Tablespaces).To(HaveLen(2))
			podList, err := env.GetClusterPodList(namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				g.Expect(pod.Spec.Containers).ToNot(BeEmpty())
				hasPostgresContainer, mountPaths := podMountPaths(pod)
				g.Expect(hasPostgresContainer).To(BeTrue())
				for tbsName := range cluster.Spec.Tablespaces {
					g.Expect(mountPaths).To(ContainElements(
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
					g.Expect(volumeNames).To(ContainElement(
						tbsName,
					))
					g.Expect(claimNames).To(ContainElement(
						pod.Name + "-tbs-" + tbsName,
					))
				}
			}
		}, timeout).Should(Succeed())
	})
}

func AssertClusterHasPvcsAndDataDirsForTablespaces(cluster *apiv1.Cluster, timeout int) {
	namespace := cluster.ObjectMeta.Namespace
	clusterName := cluster.ObjectMeta.Name
	By("checking all the required PVCs were created", func() {
		Eventually(func(g Gomega) {
			pvcList, err := env.GetPVCList(namespace)
			g.Expect(err).ShouldNot(HaveOccurred())
			var tablespacePvcNames []string
			for _, pvc := range pvcList.Items {
				roleLabel, found := pvc.Labels[utils.PvcRoleLabelName]
				g.Expect(found).To(BeTrue())
				if roleLabel != utils.PVCRolePgTablespace {
					continue
				}
				tablespacePvcNames = append(tablespacePvcNames, pvc.Name)
				tbsName := pvc.Labels[utils.TablespaceNameLabelName]
				g.Expect(tbsName).ToNot(BeEmpty())
				_, labelTbsInCluster := cluster.Spec.Tablespaces[tbsName]
				g.Expect(labelTbsInCluster).To(BeTrue())
				for tbs, config := range cluster.Spec.Tablespaces {
					if tbsName == tbs {
						g.Expect(pvc.Spec.Resources.Requests.Storage()).
							To(BeEquivalentTo(config.Storage.GetSizeOrNil()))
					}
				}
			}
			podList, err := env.GetClusterPodList(namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())
			for _, pod := range podList.Items {
				for tbsName := range cluster.Spec.Tablespaces {
					g.Expect(tablespacePvcNames).To(ContainElement(pod.Name + "-tbs-" + tbsName))
				}
			}
		}, timeout).Should(Succeed())
	})
	By("checking the data directory for the tablespaces is owned by postgres", func() {
		Eventually(func(g Gomega) {
			pvcList, err := env.GetPodList(namespace)
			g.Expect(err).ShouldNot(HaveOccurred())
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
					g.Expect(stdErr).To(BeEmpty())
					g.Expect(err).ShouldNot(HaveOccurred())
					g.Expect(owner).To(ContainSubstring("postgres"))
				}
			}
		}, timeout).Should(Succeed())
	})
}

func AssertDatabaseContainsTablespaces(cluster *apiv1.Cluster, timeout int) {
	namespace := cluster.ObjectMeta.Namespace
	clusterName := cluster.ObjectMeta.Name
	By("checking the expected tablespaces are in the database", func() {
		Eventually(func(g Gomega) {
			primary, err := env.GetClusterPrimary(namespace, clusterName)
			g.Expect(err).ShouldNot(HaveOccurred())
			tbsListing, stdErr, err := env.ExecQueryInInstancePod(
				testUtils.PodLocator{
					Namespace: namespace,
					PodName:   primary.Name,
				}, testUtils.DatabaseName("app"),
				"SELECT spcname FROM pg_tablespace;",
			)
			g.Expect(stdErr).To(BeEmpty())
			g.Expect(err).ShouldNot(HaveOccurred())
			for tbsName := range cluster.Spec.Tablespaces {
				g.Expect(tbsListing).To(ContainSubstring(tbsName))
			}
		}, timeout).Should(Succeed())
	})
}
