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
	"os"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	testsUtils "github.com/cloudnative-pg/cloudnative-pg/tests/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/timeouts"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rolling updates", Label(tests.LabelPostgresConfiguration), func() {
	const level = tests.Medium
	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})
	// gatherClusterInfo returns the current lists of podutils, pod UIDs and pvc UIDs in a given cluster
	gatherClusterInfo := func(namespace string, clusterName string) ([]string, []types.UID, []types.UID, error) {
		var podNames []string
		var podUIDs []types.UID
		var pvcUIDs []types.UID
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		for _, pod := range podList.Items {
			podNames = append(podNames, pod.GetName())
			podUIDs = append(podUIDs, pod.GetUID())
			pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
			pvc := &corev1.PersistentVolumeClaim{}
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      pvcName,
			}
			if err := env.Client.Get(env.Ctx, namespacedPVCName, pvc); err != nil {
				return nil, nil, nil, err
			}
			pvcUIDs = append(pvcUIDs, pvc.GetUID())
		}
		return podNames, podUIDs, pvcUIDs, nil
	}

	AssertPodsRunOnImage := func(
		namespace string, clusterName string, imageName string, expectedInstances int, timeout int,
	) {
		Eventually(func() (int32, error) {
			podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
			if err != nil {
				return 0, err
			}
			updatedPods := int32(0)
			for _, pod := range podList.Items {
				// We need to check if a pod is ready, otherwise we
				// may end up asking the status of a container that
				// doesn't exist yet
				if utils.IsPodActive(pod) && utils.IsPodReady(pod) {
					for _, data := range pod.Spec.Containers {
						if data.Name != specs.PostgresContainerName {
							continue
						}

						if data.Image == imageName {
							updatedPods++
						}
					}
				}
			}
			return updatedPods, err
		}, timeout).Should(BeEquivalentTo(expectedInstances))
	}

	// Verify that after an update all the podutils are ready and running
	// an updated image
	AssertUpdateImage := func(namespace string, clusterName string) {
		// TODO: the nodes are downloading the image sequentially,
		// slowing this down
		timeout := 900

		// Update to the latest minor
		updatedImageName := os.Getenv("POSTGRES_IMG")
		if updatedImageName == "" {
			updatedImageName = configuration.Current.PostgresImageName
		}

		// We should be able to apply the conf containing the new
		// image
		var cluster *apiv1.Cluster
		Eventually(func(g Gomega) error {
			var err error
			cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
			g.Expect(err).ToNot(HaveOccurred())

			cluster.Spec.ImageName = updatedImageName
			return env.Client.Update(env.Ctx, cluster)
		}, RetryTimeout, PollingTime).Should(Succeed())

		// All the postgres containers should have the updated image
		AssertPodsRunOnImage(namespace, clusterName, updatedImageName, cluster.Spec.Instances, timeout)

		// Setting up a cluster with three podutils is slow, usually 200-600s
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)
	}

	// Verify that the pod name changes amount to an expected number
	AssertChangedNames := func(
		namespace string, clusterName string,
		originalPodNames []string, expectedUnchangedNames int,
	) {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		matchingNames := 0
		for _, pod := range podList.Items {
			if utils.IsPodActive(pod) && utils.IsPodReady(pod) {
				for _, oldName := range originalPodNames {
					if pod.GetName() == oldName {
						matchingNames++
					}
				}
			}
		}
		Expect(matchingNames).To(BeEquivalentTo(expectedUnchangedNames))
	}

	// Verify that the pod UIDs changes are the expected number
	AssertNewPodsUID := func(
		namespace string, clusterName string,
		originalPodUID []types.UID, expectedUnchangedUIDs int,
	) {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		matchingUID := 0
		for _, pod := range podList.Items {
			if utils.IsPodActive(pod) && utils.IsPodReady(pod) {
				for _, oldUID := range originalPodUID {
					if pod.GetUID() == oldUID {
						matchingUID++
					}
				}
			}
		}
		Expect(matchingUID).To(BeEquivalentTo(expectedUnchangedUIDs))
	}

	// Verify that the PVC UIDs changes are the expected number
	AssertChangedPvcUID := func(
		namespace string, clusterName string,
		originalPVCUID []types.UID, expectedUnchangedPvcUIDs int,
	) {
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		matchingPVC := 0
		for _, pod := range podList.Items {
			pvcName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName

			pvc := &corev1.PersistentVolumeClaim{}
			namespacedPVCName := types.NamespacedName{
				Namespace: namespace,
				Name:      pvcName,
			}
			err := env.Client.Get(env.Ctx, namespacedPVCName, pvc)
			Expect(err).ToNot(HaveOccurred())
			for _, oldUID := range originalPVCUID {
				if pvc.GetUID() == oldUID {
					matchingPVC++
				}
			}
		}
		Expect(matchingPVC).To(BeEquivalentTo(expectedUnchangedPvcUIDs))
	}

	// Verify that the IPs of the podutils match the ones in the -r endpoint and
	// that the amount of podutils is the expected one
	AssertReadyEndpoint := func(namespace string, clusterName string, expectedEndpoints int) {
		readServiceName := clusterName + "-r"
		endpointSlice, err := testsUtils.GetEndpointSliceByServiceName(env.Ctx, env.Client, namespace, readServiceName)
		Expect(err).ToNot(HaveOccurred())
		podList, err := clusterutils.ListPods(env.Ctx, env.Client, namespace, clusterName)
		Expect(expectedEndpoints, err).To(BeEquivalentTo(len(podList.Items)))
		matchingIP := 0
		for _, pod := range podList.Items {
			ip := pod.Status.PodIP
			for _, endpoint := range endpointSlice.Endpoints {
				if ip == endpoint.Addresses[0] {
					matchingIP++
				}
			}
		}
		Expect(matchingIP).To(BeEquivalentTo(expectedEndpoints))
	}

	AssertRollingUpdate := func(
		namespace string, clusterName string,
		sampleFile string, primaryUpdateMethod apiv1.PrimaryUpdateMethod,
	) {
		var originalPodNames []string
		var originalPodUID []types.UID
		var originalPVCUID []types.UID

		AssertCreateCluster(namespace, clusterName, sampleFile, env)

		// Gather the number of instances in this Cluster
		cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		clusterInstances := cluster.Spec.Instances

		// Gather the original primary Pod
		originalPrimaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("Gathering info on the current state", func() {
			originalPodNames, originalPodUID, originalPVCUID, err = gatherClusterInfo(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
		})
		By("updating the cluster definition", func() {
			AssertUpdateImage(namespace, clusterName)
		})
		// Since we're using a pvc, after the update the podutils should
		// have been created with the same name using the same pvc.
		// Here we check that the names we've saved at the beginning
		// of the It are the same names of the current podutils.
		By("checking that the names of the podutils have not changed", func() {
			AssertChangedNames(namespace, clusterName, originalPodNames, clusterInstances)
		})
		// Even if they have the same names, they should have different
		// UIDs, as the podutils are new. Here we check that the UID
		// we've saved at the beginning of the It don't match the
		// current ones.
		By("checking that the podutils are new ones", func() {
			AssertNewPodsUID(namespace, clusterName, originalPodUID, 0)
		})
		// The PVC get reused, so they should have the same UID
		By("checking that the PVCs are the same", func() {
			AssertChangedPvcUID(namespace, clusterName, originalPVCUID, clusterInstances)
			AssertPvcHasLabels(namespace, clusterName)
		})
		// The operator should upgrade the primary last and the primary role
		// should go to a new TargetPrimary.
		// In case of single-instance cluster, we expect the primary to just
		// be deleted and recreated.
		By("having the current primary on the new TargetPrimary", func() {
			AssertPrimaryUpdateMethod(namespace, clusterName, originalPrimaryPod, primaryUpdateMethod)
		})
		// Check that the new podutils are included in the endpoint
		By("having each pod included in the -r service", func() {
			AssertReadyEndpoint(namespace, clusterName, clusterInstances)
		})
	}

	newImageCatalog := func(namespace string, name string, major uint64, image string) *apiv1.ImageCatalog {
		imgCat := &apiv1.ImageCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: apiv1.ImageCatalogSpec{
				Images: []apiv1.CatalogImage{
					{
						Image: image,
						Major: int(major),
					},
				},
			},
		}

		return imgCat
	}

	newImageCatalogCluster := func(
		namespace string, name string, major uint64, instances int, storageClass string,
	) *apiv1.Cluster {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      name,
			},
			Spec: apiv1.ClusterSpec{
				Instances: instances,
				ImageCatalogRef: &apiv1.ImageCatalogRef{
					TypedLocalObjectReference: corev1.TypedLocalObjectReference{
						APIGroup: &apiv1.SchemeGroupVersion.Group,
						Name:     name,
						Kind:     "ImageCatalog",
					},
					Major: int(major),
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{
						"log_checkpoints":             "on",
						"log_lock_waits":              "on",
						"log_min_duration_statement":  "1000",
						"log_statement":               "ddl",
						"log_temp_files":              "1024",
						"log_autovacuum_min_duration": "1s",
						"log_replication_commands":    "on",
					},
				},
				PrimaryUpdateStrategy: "unsupervised",
				PrimaryUpdateMethod:   "switchover",
				Bootstrap: &apiv1.BootstrapConfiguration{InitDB: &apiv1.BootstrapInitDB{
					Database: "app",
					Owner:    "app",
				}},
				StorageConfiguration: apiv1.StorageConfiguration{
					Size:         "1Gi",
					StorageClass: &storageClass,
				},
				WalStorage: &apiv1.StorageConfiguration{
					Size:         "1Gi",
					StorageClass: &storageClass,
				},
			},
		}

		return cluster
	}

	newClusterImageCatalog := func(name string, major uint64, image string) *apiv1.ClusterImageCatalog {
		imgCat := &apiv1.ClusterImageCatalog{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Spec: apiv1.ImageCatalogSpec{
				Images: []apiv1.CatalogImage{
					{
						Image: image,
						Major: int(major),
					},
				},
			},
		}

		return imgCat
	}

	AssertRollingUpdateWithImageCatalog := func(
		cluster *apiv1.Cluster, catalog apiv1.GenericImageCatalog, updatedImageName string,
		primaryUpdateMethod apiv1.PrimaryUpdateMethod,
	) {
		var originalPodNames []string
		var originalPodUID []types.UID
		var originalPVCUID []types.UID

		namespace := cluster.Namespace
		clusterName := cluster.Name
		err := env.Client.Create(env.Ctx, catalog)
		Expect(err).ToNot(HaveOccurred())
		err = env.Client.Create(env.Ctx, cluster)
		Expect(err).ToNot(HaveOccurred())
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)

		// Gather the number of instances in this Cluster
		cluster, err = clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())
		clusterInstances := cluster.Spec.Instances

		// Gather the original primary Pod
		originalPrimaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
		Expect(err).ToNot(HaveOccurred())

		By("Gathering info on the current state", func() {
			originalPodNames, originalPodUID, originalPVCUID, err = gatherClusterInfo(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
		})
		By("updating the catalog", func() {
			// Update to the latest minor
			catalog.GetSpec().Images[0].Image = updatedImageName
			err := env.Client.Update(env.Ctx, catalog)
			Expect(err).ToNot(HaveOccurred())
		})
		AssertPodsRunOnImage(namespace, clusterName, updatedImageName, cluster.Spec.Instances, 900)
		AssertClusterIsReady(namespace, clusterName, testTimeouts[timeouts.ClusterIsReady], env)

		// Since we're using a pvc, after the update the podutils should
		// have been created with the same name using the same pvc.
		// Here we check that the names we've saved at the beginning
		// of the It are the same names of the current podutils.
		By("checking that the names of the podutils have not changed", func() {
			AssertChangedNames(namespace, clusterName, originalPodNames, clusterInstances)
		})
		// Even if they have the same names, they should have different
		// UIDs, as the podutils are new. Here we check that the UID
		// we've saved at the beginning of the It don't match the
		// current ones.
		By("checking that the podutils are new ones", func() {
			AssertNewPodsUID(namespace, clusterName, originalPodUID, 0)
		})
		// The PVC get reused, so they should have the same UID
		By("checking that the PVCs are the same", func() {
			AssertChangedPvcUID(namespace, clusterName, originalPVCUID, clusterInstances)
			AssertPvcHasLabels(namespace, clusterName)
		})
		// The operator should upgrade the primary last and the primary role
		// should go to a new TargetPrimary.
		// In case of single-instance cluster, we expect the primary to just
		// be deleted and recreated.
		By("having the current primary on the new TargetPrimary", func() {
			AssertPrimaryUpdateMethod(namespace, clusterName, originalPrimaryPod, primaryUpdateMethod)
		})
		// Check that the new podutils are included in the endpoint
		By("having each pod included in the -r service", func() {
			AssertReadyEndpoint(namespace, clusterName, clusterInstances)
		})
	}

	Context("Image name", func() {
		Context("Three Instances", func() {
			const (
				namespacePrefix = "cluster-rolling-e2e-three-instances"
				sampleFile      = fixturesDir + "/rolling_updates/cluster-three-instances.yaml.template"
			)
			It("can do a rolling update", func() {
				// We set up a cluster with a previous release of the same PG major
				// The yaml has been previously generated from a template and
				// the image name has to be tagged as foo:MAJ.MIN. We'll update
				// it to foo:MAJ, representing the latest minor.
				// Create a cluster in a namespace we'll delete after the test
				namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
				Expect(err).ToNot(HaveOccurred())
				AssertRollingUpdate(namespace, clusterName, sampleFile, apiv1.PrimaryUpdateMethodSwitchover)
			})
		})

		Context("Single Instance", func() {
			const (
				namespacePrefix = "cluster-rolling-e2e-single-instance"
				sampleFile      = fixturesDir + "/rolling_updates/cluster-single-instance.yaml.template"
			)
			It("can do a rolling updates on a single instance", func() {
				// We set up a cluster with a previous release of the same PG major
				// The yaml has been previously generated from a template and
				// the image name has to be tagged as foo:MAJ.MIN. We'll update
				// it to foo:MAJ, representing the latest minor.
				// Create a cluster in a namespace we'll delete after the test
				namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
				Expect(err).ToNot(HaveOccurred())
				AssertRollingUpdate(namespace, clusterName, sampleFile, apiv1.PrimaryUpdateMethodRestart)
			})
		})

		Context("primaryUpdateMethod set to restart", func() {
			const (
				namespacePrefix = "cluster-rolling-with-primary-update-method"
				sampleFile      = fixturesDir + "/rolling_updates/cluster-using-primary-update-method.yaml.template"
			)
			It("can do rolling update", func() {
				namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
				Expect(err).ToNot(HaveOccurred())
				clusterName, err := yaml.GetResourceNameFromYAML(env.Scheme, sampleFile)
				Expect(err).ToNot(HaveOccurred())
				AssertRollingUpdate(namespace, clusterName, sampleFile, apiv1.PrimaryUpdateMethodRestart)
			})
		})
	})

	Context("Image Catalogs", func() {
		var storageClass string
		var preRollingImg string
		var updatedImageName string
		var pgVersion version.Data
		BeforeEach(func() {
			storageClass = os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
			preRollingImg = os.Getenv("E2E_PRE_ROLLING_UPDATE_IMG")
			updatedImageName = os.Getenv("POSTGRES_IMG")
			if updatedImageName == "" {
				updatedImageName = configuration.Current.PostgresImageName
			}

			// We automate the extraction of the major version from the image, because we don't want to keep maintaining
			// the major version in the test
			var err error
			pgVersion, err = version.FromTag(reference.New(preRollingImg).Tag)
			if err != nil {
				Expect(err).ToNot(HaveOccurred())
			}
		})

		Context("ImageCatalog", func() {
			const (
				clusterName = "image-catalog"
			)
			Context("Three Instances", func() {
				const (
					namespacePrefix = "imagecatalog-cluster-rolling-e2e-three-instances"
				)
				It("can do a rolling update", func() {
					// We set up a cluster with a previous release of the same PG major
					// The yaml has been previously generated from a template and
					// the image name has to be tagged as foo:MAJ.MIN. We'll update
					// it to foo:MAJ, representing the latest minor.
					// Create a cluster in a namespace we'll delete after the test
					namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
					Expect(err).ToNot(HaveOccurred())

					// Create a new image catalog and a new cluster
					catalog := newImageCatalog(namespace, clusterName, pgVersion.Major(), preRollingImg)
					cluster := newImageCatalogCluster(namespace, clusterName, pgVersion.Major(), 3, storageClass)

					AssertRollingUpdateWithImageCatalog(cluster, catalog, updatedImageName, apiv1.PrimaryUpdateMethodSwitchover)
				})
			})
			Context("Single Instance", func() {
				const (
					namespacePrefix = "imagecatalog-cluster-rolling-e2e-single-instance"
				)
				It("can do a rolling updates on a single instance", func() {
					// We set up a cluster with a previous release of the same PG major
					// The yaml has been previously generated from a template and
					// the image name has to be tagged as foo:MAJ.MIN. We'll update
					// it to foo:MAJ, representing the latest minor.
					// Create a cluster in a namespace we'll delete after the test
					namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
					Expect(err).ToNot(HaveOccurred())

					catalog := newImageCatalog(namespace, clusterName, pgVersion.Major(), preRollingImg)
					cluster := newImageCatalogCluster(namespace, clusterName, pgVersion.Major(), 1, storageClass)
					AssertRollingUpdateWithImageCatalog(cluster, catalog, updatedImageName, apiv1.PrimaryUpdateMethodRestart)
				})
			})
		})
		Context("ClusterImageCatalog", Serial, func() {
			const (
				clusterName = "cluster-image-catalog"
			)
			var catalog *apiv1.ClusterImageCatalog
			BeforeEach(func() {
				catalog = newClusterImageCatalog(clusterName, pgVersion.Major(), preRollingImg)
			})
			AfterEach(func() {
				err := env.Client.Delete(env.Ctx, catalog)
				Expect(err).ToNot(HaveOccurred())

				// Wait until we really deleted it
				Eventually(func() error {
					return env.Client.Get(env.Ctx, ctrl.ObjectKey{Name: catalog.Name}, catalog)
				}, 30).Should(MatchError(apierrs.IsNotFound, string(metav1.StatusReasonNotFound)))
			})
			Context("Three Instances", func() {
				const (
					namespacePrefix = "clusterimagecatalog-cluster-rolling-e2e-three-instances"
				)
				It("can do a rolling update", func() {
					// We set up a cluster with a previous release of the same PG major
					// The yaml has been previously generated from a template and
					// the image name has to be tagged as foo:MAJ.MIN. We'll update
					// it to foo:MAJ, representing the latest minor.
					// Create a cluster in a namespace we'll delete after the test
					namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
					Expect(err).ToNot(HaveOccurred())

					cluster := newImageCatalogCluster(namespace, clusterName, pgVersion.Major(), 3, storageClass)
					cluster.Spec.ImageCatalogRef.Kind = "ClusterImageCatalog"
					AssertRollingUpdateWithImageCatalog(cluster, catalog, updatedImageName, apiv1.PrimaryUpdateMethodSwitchover)
				})
			})
			Context("Single Instance", func() {
				const (
					namespacePrefix = "clusterimagecatalog-cluster-rolling-e2e-single-instance"
				)
				It("can do a rolling updates on a single instance", func() {
					// We set up a cluster with a previous release of the same PG major
					// The yaml has been previously generated from a template and
					// the image name has to be tagged as foo:MAJ.MIN. We'll update
					// it to foo:MAJ, representing the latest minor.
					// Create a cluster in a namespace we'll delete after the test
					namespace, err := env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
					Expect(err).ToNot(HaveOccurred())

					cluster := newImageCatalogCluster(namespace, clusterName, pgVersion.Major(), 1, storageClass)
					cluster.Spec.ImageCatalogRef.Kind = "ClusterImageCatalog"
					AssertRollingUpdateWithImageCatalog(cluster, catalog, updatedImageName, apiv1.PrimaryUpdateMethodRestart)
				})
			})
		})
	})
})
