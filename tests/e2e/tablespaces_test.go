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
	"path/filepath"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Tablespaces tests", Label(tests.LabelSmoke, tests.LabelStorage, tests.LabelBasic), func() {
	const (
		level           = tests.Medium
		namespacePrefix = "tablespaces"
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

	Context("on a new cluster with tablespaces", Ordered, func() {
		var backupName string
		var err error
		const (
			minioCaSecName  = "minio-server-ca-secret"
			minioTLSSecName = "minio-server-tls-secret"
			clusterManifest = fixturesDir +
				"/tablespaces/cluster-with-tablespaces.yaml.template"
			clusterBackupManifest = fixturesDir +
				"/tablespaces/cluster-with-tablespaces-backup.yaml.template"
		)
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})
		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			By("creating ca and tls certificate secrets", func() {
				// create CA certificates
				_, caPair, err := testUtils.CreateSecretCA(namespace, clusterName, minioCaSecName, true, env)
				Expect(err).ToNot(HaveOccurred())

				// sign and create secret using CA certificate and key
				serverPair, err := caPair.CreateAndSignPair("minio-service", certs.CertTypeServer,
					[]string{"minio-service.internal.mydomain.net, minio-service.default.svc, minio-service.default,"},
				)
				Expect(err).ToNot(HaveOccurred())
				serverSecret := serverPair.GenerateCertificateSecret(namespace, minioTLSSecName)
				err = env.Client.Create(env.Ctx, serverSecret)
				Expect(err).ToNot(HaveOccurred())
			})

			By("creating the credentials for minio", func() {
				AssertStorageCredentialsAreCreated(namespace, "backup-storage-creds", "minio", "minio123")
			})

			By("setting up minio", func() {
				setup, err := testUtils.MinioSSLSetup(namespace)
				Expect(err).ToNot(HaveOccurred())
				err = testUtils.InstallMinio(env, setup, uint(testTimeouts[testUtils.MinioInstallation]))
				Expect(err).ToNot(HaveOccurred())
			})

			// Create the minio client pod and wait for it to be ready.
			// We'll use it to check if everything is archived correctly
			By("setting up minio client pod", func() {
				minioClient := testUtils.MinioSSLClient(namespace)
				err := testUtils.PodCreateAndWaitForReady(env, &minioClient, 240)
				Expect(err).ToNot(HaveOccurred())
			})

			clusterSetup(clusterManifest)
		})

		It("can verify tablespaces and PVC were created", func() {
			AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[testUtils.Short])
			AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[testUtils.Short])
			AssertDatabaseContainsTablespaces(cluster, testTimeouts[testUtils.Short])
		})

		It("can create the backup and verify content in the object store", func() {
			backupName, err = env.GetResourceNameFromYAML(clusterBackupManifest)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("creating backup %s and verifying backup is ready", backupName), func() {
				testUtils.ExecuteBackup(namespace, clusterBackupManifest, false, testTimeouts[testUtils.BackupIsReady], env)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
			})

			By("verifying the number of tars in minio", func() {
				latestBaseBackupContainsExpectedTars(clusterName, namespace, 1, 3)
			})

			By("verifying backup status", func() {
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.LastSuccessfulBackup, err
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.LastFailedBackup, err
				}, 30).Should(BeEmpty())
			})
		})

		It("can update the cluster adding a new tablespace and backup again", func() {
			By("adding a new tablespace to the cluster", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				addTablespaces(cluster, map[string]apiv1.TablespaceConfiguration{
					"thirdtablespace": {
						Storage: apiv1.StorageConfiguration{
							Size: "1Gi",
						},
					},
				})

				cluster, err = env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())
			})

			By("verifying there are 3 tablespaces and PVCs were created", func() {
				cluster, err = env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.Spec.Tablespaces).To(HaveLen(3))

				AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 3, testTimeouts[testUtils.PodRollout])
				AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[testUtils.PodRollout])
				AssertDatabaseContainsTablespaces(cluster, testTimeouts[testUtils.PodRollout])
			})

			By("waiting for the cluster to be ready", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReady], env)
			})

			By("verifying expected number of PVCs for tablespaces", func() {
				// 2 pods x 3 tablespaces = 6 pvcs for tablespaces
				eventuallyHasExpectedNumberOfPVCs(6, namespace)
			})

			By("creating new backup and verifying backup is ready", func() {
				backupCondition, err := testUtils.GetConditionsInClusterStatus(
					namespace,
					clusterName,
					env,
					apiv1.ConditionBackup,
				)
				Expect(err).ShouldNot(HaveOccurred())
				_, stderr, err := testUtils.Run(fmt.Sprintf("kubectl cnpg backup %s -n %s", clusterName, namespace))
				Expect(stderr).To(BeEmpty())
				Expect(err).ShouldNot(HaveOccurred())
				AssertBackupConditionTimestampChangedInClusterStatus(
					namespace,
					clusterName,
					apiv1.ConditionBackup,
					&backupCondition.LastTransitionTime,
				)
				AssertBackupConditionInClusterStatus(namespace, clusterName)
			})

			By("verifying the number of tars in the latest base backup", func() {
				backups := 2
				eventuallyHasCompletedBackups(namespace, backups)
				// in the latest base backup, we expect 4 tars
				//   (data.tar + 3 tars for each of the 3 tablespaces)
				latestBaseBackupContainsExpectedTars(clusterName, namespace, backups, 4)
			})

			By("verify backup status", func() {
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.FirstRecoverabilityPoint, err
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.LastSuccessfulBackup, err
				}, 30).ShouldNot(BeEmpty())
				Eventually(func() (string, error) {
					cluster, err := env.GetCluster(namespace, clusterName)
					return cluster.Status.LastFailedBackup, err
				}, 30).Should(BeEmpty())
			})
		})
	})
	Context("on a plain cluster with primaryUpdateMethod=restart", Ordered, func() {
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		clusterManifest := fixturesDir + "/tablespaces/cluster-without-tablespaces.yaml.template"
		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})
			clusterSetup(clusterManifest)
		})

		It("can update cluster by adding tablespaces", func() {
			By("adding tablespaces to the spec and patching", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeFalse())

				addTablespaces(cluster, map[string]apiv1.TablespaceConfiguration{
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
				})

				cluster, err = env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())
			})
			By("verify tablespaces and PVC were created", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())

				AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[testUtils.PodRollout])
				AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[testUtils.PodRollout])
				AssertDatabaseContainsTablespaces(cluster, testTimeouts[testUtils.PodRollout])
			})
			By("waiting for the cluster to be ready again", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReady], env)
			})
		})

		It("can hibernate via plugin a cluster with tablespaces", func() {
			assertCanHibernateClusterWithTablespaces(namespace, clusterName, testUtils.HibernateImperatively, 2)
		})

		It("can hibernate via annotation a cluster with tablespaces", func() {
			assertCanHibernateClusterWithTablespaces(namespace, clusterName, testUtils.HibernateDeclaratively, 6)
		})

		It("can fence a cluster with tablespaces using the plugin", func() {
			By("verifying expected PVCs for tablespaces before hibernate", func() {
				eventuallyHasExpectedNumberOfPVCs(6, namespace)
			})

			By("fencing the cluster", func() {
				err := testUtils.FencingOn(env, "*", namespace, clusterName, testUtils.UsingPlugin)
				Expect(err).ToNot(HaveOccurred())
			})

			By("check all instances become not ready", func() {
				Eventually(func() (bool, error) {
					podList, err := env.GetClusterPodList(namespace, clusterName)
					if err != nil {
						return false, err
					}
					var hasReadyPod bool
					for _, pod := range podList.Items {
						for _, podInfo := range pod.Status.ContainerStatuses {
							if podInfo.Name == specs.PostgresContainerName {
								if podInfo.Ready {
									hasReadyPod = true
								}
							}
						}
					}
					return hasReadyPod, nil
				}, 120, 5).Should(BeFalse())
			})

			By("un-fencing the cluster", func() {
				err := testUtils.FencingOff(env, "*", namespace, clusterName, testUtils.UsingPlugin)
				Expect(err).ToNot(HaveOccurred())
			})

			By("all instances become ready", func() {
				Eventually(func() (bool, error) {
					podList, err := env.GetClusterPodList(namespace, clusterName)
					if err != nil {
						return false, err
					}
					var hasReadyPod bool
					for _, pod := range podList.Items {
						for _, podInfo := range pod.Status.ContainerStatuses {
							if podInfo.Name == specs.PostgresContainerName {
								if podInfo.Ready {
									hasReadyPod = true
								}
							}
						}
					}
					return hasReadyPod, nil
				}, 120, 5).Should(BeTrue())
			})

			By("verify tablespaces and PVC are there", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeTrue())

				AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[testUtils.PodRollout])
				AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[testUtils.PodRollout])
				AssertDatabaseContainsTablespaces(cluster, testTimeouts[testUtils.PodRollout])
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReady], env)
			})

			By("verifying all PVCs for tablespaces are recreated", func() {
				eventuallyHasExpectedNumberOfPVCs(6, namespace)
			})
		})
	})
	Context("on a plain cluster with primaryUpdateMethod=switchover", Ordered, func() {
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		clusterManifest := fixturesDir + "/tablespaces/cluster-without-tablespaces.yaml.template"
		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})
			clusterSetup(clusterManifest)
		})
		It("can update cluster adding tablespaces", func() {
			By("patch cluster with primaryUpdateMethod=switchover", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeFalse())

				updated := cluster.DeepCopy()
				updated.Spec.PrimaryUpdateMethod = apiv1.PrimaryUpdateMethodSwitchover
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})
			By("waiting for the cluster to be ready", func() {
				AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReady], env)
			})
			By("adding tablespaces to the spec and patching", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Expect(cluster.ContainsTablespaces()).To(BeFalse())

				updated := cluster.DeepCopy()
				updated.Spec.Tablespaces = map[string]apiv1.TablespaceConfiguration{
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
				Expect(cluster.ContainsTablespaces()).To(BeTrue())
			})
		})

		It("can verify tablespaces and PVC were created", func() {
			cluster, err := env.GetCluster(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Expect(cluster.ContainsTablespaces()).To(BeTrue())

			AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[testUtils.PodRollout])
			AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[testUtils.PodRollout])
			AssertDatabaseContainsTablespaces(cluster, testTimeouts[testUtils.PodRollout])
			AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReady], env)
		})
	})
})

func addTablespaces(cluster *apiv1.Cluster, tbsMap map[string]apiv1.TablespaceConfiguration) {
	updated := cluster.DeepCopy()
	if updated.Spec.Tablespaces == nil {
		updated.Spec.Tablespaces = map[string]apiv1.TablespaceConfiguration{}
	}

	for tbsName, configuration := range tbsMap {
		updated.Spec.Tablespaces[tbsName] = configuration
	}
	err := env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
	Expect(err).ToNot(HaveOccurred())
}

func AssertClusterHasMountPointsAndVolumesForTablespaces(
	cluster *apiv1.Cluster,
	numTablespaces int,
	timeout int,
) {
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
			g.Expect(cluster.ContainsTablespaces()).To(BeTrue())
			g.Expect(cluster.Spec.Tablespaces).To(HaveLen(numTablespaces))
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
				roleLabel := pvc.Labels[utils.PvcRoleLabelName]
				if roleLabel != string(utils.PVCRolePgTablespace) {
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
			// minio may in the same namespace with cluster pod
			pvcList, err := env.GetClusterPodList(namespace, clusterName)
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
			instances, err := env.GetClusterPodList(namespace, clusterName)
			g.Expect(err).ShouldNot(HaveOccurred())
			var tbsListing string
			for _, instance := range instances.Items {
				var stdErr string
				var err error
				tbsListing, stdErr, err = env.ExecQueryInInstancePod(
					testUtils.PodLocator{
						Namespace: namespace,
						PodName:   instance.Name,
					}, testUtils.DatabaseName("app"),
					"SELECT oid, spcname FROM pg_tablespace;",
				)
				g.Expect(stdErr).To(BeEmpty())
				g.Expect(err).ShouldNot(HaveOccurred())
				for tbsName := range cluster.Spec.Tablespaces {
					g.Expect(tbsListing).To(ContainSubstring(tbsName))
				}
			}
			GinkgoWriter.Printf("Tablespaces in DB:\n%s\n", tbsListing)
		}, timeout).Should(Succeed())
	})
}

func assertCanHibernateClusterWithTablespaces(
	namespace string,
	clusterName string,
	method testUtils.HibernationMethod,
	keptPVCs int,
) {
	By("verifying expected PVCs for tablespaces before hibernate", func() {
		eventuallyHasExpectedNumberOfPVCs(6, namespace)
	})

	By("hibernate the cluster", func() {
		err := testUtils.HibernateOn(env, namespace, clusterName, method)
		Expect(err).ToNot(HaveOccurred())
	})

	By(fmt.Sprintf("verifying cluster %v pods are removed", clusterName), func() {
		Eventually(func(g Gomega) {
			podList, _ := env.GetClusterPodList(namespace, clusterName)
			g.Expect(podList.Items).Should(BeEmpty())
		}, 300).Should(Succeed())
	})

	By("verifying expected number of PVCs for tablespaces are kept in hibernation", func() {
		eventuallyHasExpectedNumberOfPVCs(keptPVCs, namespace)
	})

	By("hibernate off the cluster", func() {
		err := testUtils.HibernateOff(env, namespace, clusterName, method)
		Expect(err).ToNot(HaveOccurred())
	})

	By("waiting for the cluster to be ready", func() {
		AssertClusterIsReady(namespace, clusterName, testTimeouts[testUtils.ClusterIsReady], env)
	})

	By("verify tablespaces and PVC are there", func() {
		cluster, err := env.GetCluster(namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		Expect(cluster.ContainsTablespaces()).To(BeTrue())

		AssertClusterHasMountPointsAndVolumesForTablespaces(cluster, 2, testTimeouts[testUtils.PodRollout])
		AssertClusterHasPvcsAndDataDirsForTablespaces(cluster, testTimeouts[testUtils.PodRollout])
		AssertDatabaseContainsTablespaces(cluster, testTimeouts[testUtils.PodRollout])
	})

	By("verifying all PVCs for tablespaces are recreated", func() {
		eventuallyHasExpectedNumberOfPVCs(6, namespace)
	})
}

func eventuallyHasExpectedNumberOfPVCs(pvcCount int, namespace string) {
	By(fmt.Sprintf("checking cluster eventually has %d PVCs for tablespaces", pvcCount))
	Eventually(func(g Gomega) {
		pvcList, err := env.GetPVCList(namespace)
		g.Expect(err).ShouldNot(HaveOccurred())
		tbsPvc := 0
		for _, pvc := range pvcList.Items {
			roleLabel := pvc.Labels[utils.PvcRoleLabelName]
			if roleLabel != string(utils.PVCRolePgTablespace) {
				continue
			}
			tbsPvc++
		}
		g.Expect(tbsPvc).Should(Equal(pvcCount))
	}, testTimeouts[testUtils.ClusterIsReady]).Should(Succeed())
}

func eventuallyHasCompletedBackups(namespace string, numBackups int) {
	Eventually(func(g Gomega) {
		backups, err := env.GetBackupList(namespace)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(backups.Items).To(HaveLen(numBackups))

		completedBackups := 0
		for _, backup := range backups.Items {
			if string(backup.Status.Phase) == "completed" {
				completedBackups++
			}
		}
		g.Expect(completedBackups).To(Equal(numBackups))
	}, 120).Should(Succeed())
}

func latestBaseBackupContainsExpectedTars(
	clusterName string,
	namespace string,
	numBackups int,
	expectedTars int,
) {
	Eventually(func(g Gomega) {
		// we list the backup.info files to get the listing of base backups
		// directories in minio
		backupInfoFiles := filepath.Join("*", clusterName, "base", "*", "*.info")
		ls, err := testUtils.ListFilesOnMinio(namespace, minioClientName, backupInfoFiles)
		g.Expect(err).ShouldNot(HaveOccurred())
		frags := strings.Split(ls, "\n")
		slices.Sort(frags)
		report := fmt.Sprintf("directories:\n%s\n", strings.Join(frags, "\n"))
		g.Expect(frags).To(HaveLen(numBackups), report)
		latestBaseBackup := filepath.Dir(frags[numBackups-1])
		tarsInLastBackup := strings.TrimPrefix(filepath.Join(latestBaseBackup, "*.tar"), "minio/")
		listing, err := testUtils.ListFilesOnMinio(namespace, minioClientName, tarsInLastBackup)
		g.Expect(err).ShouldNot(HaveOccurred())
		report = report + fmt.Sprintf("tar listing:\n%s\n", listing)
		numTars, err := testUtils.CountFilesOnMinio(namespace, minioClientName, tarsInLastBackup)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(numTars).To(Equal(expectedTars), report)
	}, 120).Should(Succeed())
}
