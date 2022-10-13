package e2e

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster Hibernation with plugin", func() {
	type Mode string
	const (
		sampleFileClusterWithPGWalVolume         = fixturesDir + "/base/cluster-storage-class.yaml.template"
		sampleFileClusterWithOutPGWalVolume      = fixturesDir + "/hibernate/cluster-storage-class-without-wal.yaml.template"
		level                                    = tests.Medium
		HibernateOn                         Mode = "on"
		HibernateOff                        Mode = "off"
		HibernateStatus                     Mode = "status"
	)
	var namespace string
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	JustAfterEach(func() {
		if CurrentSpecReport().Failed() {
			env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
		}
	})

	AfterEach(func() {
		err := env.DeleteNamespace(namespace)
		Expect(err).ToNot(HaveOccurred())
	})

	Context("hibernate on", func() {
		var beforeHibernationCurrentPrimary, clusterName string
		var beforeHibernationPgWalPvcUID, beforeHibernationPgDataPvcUID types.UID
		var beforeHibernationClusterInfo *apiv1.Cluster
		var clusterManifest []byte
		var err error
		getPrimaryAndClusterManifest := func(namespace, clusterName string) {
			By("collecting current primary details", func() {
				beforeHibernationClusterInfo, err = env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				beforeHibernationCurrentPrimary = beforeHibernationClusterInfo.Status.CurrentPrimary
				// collect expected cluster manifesto info
				clusterManifest, err = json.Marshal(&beforeHibernationClusterInfo)
				Expect(err).ToNot(HaveOccurred())
			})
		}
		getPvc := func(role utils.PVCRole) corev1.PersistentVolumeClaim {
			pvcName := specs.GetPVCName(*beforeHibernationClusterInfo,
				beforeHibernationCurrentPrimary, role)
			pvcInfo := corev1.PersistentVolumeClaim{}
			err = testsUtils.GetObject(env, ctrlclient.ObjectKey{Namespace: namespace, Name: pvcName}, &pvcInfo)
			Expect(err).ToNot(HaveOccurred())
			return pvcInfo
		}
		performHibernation := func(mode Mode) {
			By("performing hibernation", func() {
				_, _, err := testsUtils.Run(fmt.Sprintf("kubectl cnpg hibernate %v %v -n %v",
					mode, clusterName, namespace))
				Expect(err).ToNot(HaveOccurred())
			})
			Eventually(func(g Gomega) {
				podList, _ := env.GetClusterPodList(namespace, clusterName)
				g.Expect(len(podList.Items)).Should(BeEquivalentTo(0))
			}, 180).Should(Succeed())
		}
		verifyClusterResources := func(namespace, clusterName string, roles []utils.PVCRole) {
			By(fmt.Sprintf("verifying cluster resources are removed "+
				"post hibernation where roles %v", roles), func() {
				By(fmt.Sprintf("verifying cluster %v is removed", clusterName), func() {
					cluster := &apiv1.Cluster{}
					err := env.Client.Get(env.Ctx,
						ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
						cluster)
					Expect(err).To(HaveOccurred())
				})

				By(fmt.Sprintf("verifying cluster %v pods are removed", clusterName), func() {
					podList, _ := env.GetClusterPodList(namespace, clusterName)
					Expect(len(podList.Items)).Should(BeEquivalentTo(0))
				})

				By(fmt.Sprintf("verifying cluster %v PVCs are removed", clusterName), func() {
					pvcList, err := env.GetPVCList(namespace)
					Expect(err).ToNot(HaveOccurred())
					Expect(len(pvcList.Items)).Should(BeEquivalentTo(len(roles)))
				})

				By(fmt.Sprintf("verifying cluster %v configMap is removed", clusterName), func() {
					configMap := corev1.ConfigMap{}
					err = env.Client.Get(env.Ctx,
						ctrlclient.ObjectKey{Namespace: namespace, Name: apiv1.DefaultMonitoringConfigMapName},
						&configMap)
					Expect(err).To(HaveOccurred())
				})

				By(fmt.Sprintf("verifying cluster %v secrets are removed", clusterName), func() {
					secretList := corev1.SecretList{}
					_ = env.Client.List(env.Ctx, &secretList, ctrlclient.InNamespace(namespace))
					Expect(len(secretList.Items)).Should(BeEquivalentTo(0))
				})

				By(fmt.Sprintf("verifying cluster %v role is removed", clusterName), func() {
					role := v1.Role{}
					err = env.Client.Get(env.Ctx,
						ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
						&role)
					Expect(err).To(HaveOccurred())
				})

				By(fmt.Sprintf("verifying cluster %v rolebinding is removed", clusterName), func() {
					roleBinding := v1.RoleBinding{}
					err = env.Client.Get(env.Ctx,
						ctrlclient.ObjectKey{Namespace: namespace, Name: clusterName},
						&roleBinding)
					Expect(err).To(HaveOccurred())
				})
			})
		}
		verifyPvc := func(role utils.PVCRole, pvcUid types.UID) {
			pvcInfo := getPvc(role)
			Expect(pvcUid).Should(BeEquivalentTo(pvcInfo.GetUID()))
			// pvc should be attached annotation with pgControlData and Cluster manifesto
			expectedAnnotationKeyPresent := []string{
				utils.HibernatePgControlDataAnnotationName,
				utils.HibernateClusterManifestAnnotationName,
			}
			testsUtils.PvcHasAnnotationKeys(pvcInfo, expectedAnnotationKeyPresent)
			expectedAnnotation := map[string]string{
				utils.HibernateClusterManifestAnnotationName: string(clusterManifest),
			}
			testsUtils.PvcHasAnnotation(pvcInfo, expectedAnnotation)
		}

		When("cluster setup with PG-WAL volume", func() {
			It("hibernation process should work", func() {
				namespace = "hibernation-on-with-pg-wal"
				clusterName, err = env.GetResourceNameFromYAML(sampleFileClusterWithPGWalVolume)
				Expect(err).ToNot(HaveOccurred())
				// Create a cluster in a namespace we'll delete after the test
				err = env.CreateNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, sampleFileClusterWithPGWalVolume, env)
				getPrimaryAndClusterManifest(namespace, clusterName)
				By("collecting pgWal pvc details of current primary", func() {
					pvcInfo := getPvc(utils.PVCRolePgWal)
					beforeHibernationPgWalPvcUID = pvcInfo.GetUID()
				})

				By("collecting pgData pvc details of current primary", func() {
					pvcInfo := getPvc(utils.PVCRolePgData)
					beforeHibernationPgDataPvcUID = pvcInfo.GetUID()
				})

				performHibernation(HibernateOn)

				// After hibernation, it will destroy all the resources generated by the cluster,
				// except the PVCs that belong to the PostgreSQL primary instance.
				verifyClusterResources(namespace, clusterName, []utils.PVCRole{utils.PVCRolePgWal, utils.PVCRolePgData})

				By("verifying primary pgWal pvc info", func() {
					verifyPvc(utils.PVCRolePgWal, beforeHibernationPgWalPvcUID)
				})

				By("verifying primary pgData pvc info", func() {
					verifyPvc(utils.PVCRolePgData, beforeHibernationPgDataPvcUID)
				})
				By("verify PVC group", func() {
					// TODO PVC group
				})
			})
		})
		When("cluster setup without PG-WAL volume", func() {
			It("hibernation process should work", func() {
				namespace = "hibernation-without-pg-wal"
				clusterName, err = env.GetResourceNameFromYAML(sampleFileClusterWithOutPGWalVolume)
				Expect(err).ToNot(HaveOccurred())
				// Create a cluster in a namespace we'll delete after the test
				err = env.CreateNamespace(namespace)
				Expect(err).ToNot(HaveOccurred())
				AssertCreateCluster(namespace, clusterName, sampleFileClusterWithOutPGWalVolume, env)
				getPrimaryAndClusterManifest(namespace, clusterName)
				By("collecting pgData pvc details of current primary", func() {
					pvcInfo := getPvc(utils.PVCRolePgData)
					beforeHibernationPgDataPvcUID = pvcInfo.GetUID()
				})

				performHibernation(HibernateOn)

				// After hibernation, it will destroy all the resources generated by the cluster,
				// except the PVCs that belong to the PostgreSQL primary instance.
				verifyClusterResources(namespace, clusterName, []utils.PVCRole{utils.PVCRolePgData})

				By("verifying primary pgData pvc info", func() {
					verifyPvc(utils.PVCRolePgData, beforeHibernationPgDataPvcUID)
				})
			})
		})
	})
})
