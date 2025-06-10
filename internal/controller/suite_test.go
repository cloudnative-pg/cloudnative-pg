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

package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
	fakediscovery "k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	// +kubebuilder:scaffold:imports
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	schemeBuilder "github.com/cloudnative-pg/cloudnative-pg/internal/scheme"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers Suite")
}

type testingEnvironment struct {
	backupReconciler  *BackupReconciler
	scheme            *runtime.Scheme
	clusterReconciler *ClusterReconciler
	poolerReconciler  *PoolerReconciler
	discoveryClient   *fakediscovery.FakeDiscovery
	client            client.WithWatch
}

func buildTestEnvironment() *testingEnvironment {
	var err error
	Expect(err).ToNot(HaveOccurred())

	scheme := schemeBuilder.BuildWithAllKnownScheme()
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&apiv1.Cluster{}, &apiv1.Backup{}, &apiv1.Pooler{}, &corev1.Service{},
			&corev1.ConfigMap{}, &corev1.Secret{}).
		WithIndex(&batchv1.Job{}, jobOwnerKey, jobOwnerIndexFunc).
<<<<<<< HEAD
		WithIndex(&apiv1.Backup{}, ".spec.cluster.name", func(rawObj client.Object) []string {
			return []string{rawObj.(*apiv1.Backup).Spec.Cluster.Name}
=======
		WithIndex(&corev1.Pod{}, podOwnerKey, func(rawObj client.Object) []string {
			pod := rawObj.(*corev1.Pod)
			if ownerName, ok := IsOwnedByCluster(pod); ok {
				return []string{ownerName}
			}
			return nil
		}).
		WithIndex(&corev1.PersistentVolumeClaim{}, pvcOwnerKey, func(rawObj client.Object) []string {
			persistentVolumeClaim := rawObj.(*corev1.PersistentVolumeClaim)
			if ownerName, ok := IsOwnedByCluster(persistentVolumeClaim); ok {
				return []string{ownerName}
			}
			return nil
>>>>>>> 46b614bad (Fixes Issue 7793: Stuck reconciliation fixes)
		}).
		Build()
	Expect(err).ToNot(HaveOccurred())

	discoveryClient := &fakediscovery.FakeDiscovery{
		Fake: &k8stesting.Fake{
			Resources: []*metav1.APIResourceList{
				{
					GroupVersion: "monitoring.coreos.com/v1",
					APIResources: []metav1.APIResource{
						{
							Name:       "podmonitors",
							Kind:       "PodMonitor",
							Namespaced: true,
						},
					},
				},
			},
		},
	}
	Expect(err).ToNot(HaveOccurred())

	clusterReconciler := &ClusterReconciler{
		Client:          k8sClient,
		Scheme:          scheme,
		Recorder:        record.NewFakeRecorder(120),
		DiscoveryClient: discoveryClient,
	}

	poolerReconciler := &PoolerReconciler{
		Client:          k8sClient,
		Scheme:          scheme,
		Recorder:        record.NewFakeRecorder(120),
		DiscoveryClient: discoveryClient,
	}

	backupReconciler := &BackupReconciler{
		Client:   k8sClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(120),
	}

	return &testingEnvironment{
		scheme:            scheme,
		client:            k8sClient,
		clusterReconciler: clusterReconciler,
		backupReconciler:  backupReconciler,
		poolerReconciler:  poolerReconciler,
		discoveryClient:   discoveryClient,
	}
}

func newFakePooler(k8sClient client.Client, cluster *apiv1.Cluster) *apiv1.Pooler {
	pooler := &apiv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "pooler-" + rand.String(10),
			Namespace:   cluster.Namespace,
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: apiv1.PoolerSpec{
			Cluster: apiv1.LocalObjectReference{
				Name: cluster.Name,
			},
			Type:      "rw",
			Instances: ptr.To(int32(1)),
			PgBouncer: &apiv1.PgBouncerSpec{
				PoolMode: apiv1.PgBouncerPoolModeSession,
			},
		},
	}

	err := k8sClient.Create(context.Background(), pooler)
	Expect(err).ToNot(HaveOccurred())

	// upstream issue, go client cleans typemeta: https://github.com/kubernetes/client-go/issues/308
	pooler.TypeMeta = metav1.TypeMeta{
		Kind:       apiv1.PoolerKind,
		APIVersion: apiv1.SchemeGroupVersion.String(),
	}

	return pooler
}

func newFakeCNPGCluster(
	k8sClient client.Client,
	namespace string,
	mutators ...func(cluster *apiv1.Cluster),
) *apiv1.Cluster {
	const instances int = 3
	name := "cluster-" + rand.String(10)
	caServer := fmt.Sprintf("%s-ca-server", name)
	caClient := fmt.Sprintf("%s-ca-client", name)

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: apiv1.ClusterSpec{
			Instances: instances,
			Certificates: &apiv1.CertificatesConfiguration{
				ServerCASecret: caServer,
				ClientCASecret: caClient,
			},
			StorageConfiguration: apiv1.StorageConfiguration{
				Size: "1G",
			},
		},
	}

	cluster.SetDefaults()

	for _, mutator := range mutators {
		mutator(cluster)
	}

	err := k8sClient.Create(context.Background(), cluster)
	Expect(err).ToNot(HaveOccurred())

	cluster.Status = apiv1.ClusterStatus{
		Instances:                instances,
		SecretsResourceVersion:   apiv1.SecretsResourceVersion{},
		ConfigMapResourceVersion: apiv1.ConfigMapResourceVersion{},
		Certificates: apiv1.CertificatesStatus{
			CertificatesConfiguration: apiv1.CertificatesConfiguration{
				ServerCASecret: caServer,
				ClientCASecret: caClient,
			},
		},
	}
	// nolint: lll
	// https://github.com/kubernetes-sigs/controller-runtime/blob/c3c1f058a9a080581e8fe99c004fcc792b2aff07/pkg/client/fake/doc.go#L30
	for _, mutator := range mutators {
		mutator(cluster)
	}

	err = k8sClient.Update(context.Background(), cluster)
	Expect(err).ToNot(HaveOccurred())
	err = k8sClient.Status().Update(context.Background(), cluster)
	Expect(err).ToNot(HaveOccurred())
	// upstream issue, go client cleans typemeta: https://github.com/kubernetes/client-go/issues/308
	cluster.TypeMeta = metav1.TypeMeta{
		Kind:       apiv1.ClusterKind,
		APIVersion: apiv1.SchemeGroupVersion.String(),
	}

	return cluster
}

func newFakeCNPGClusterWithPGWal(k8sClient client.Client, namespace string) *apiv1.Cluster {
	const instances int = 3
	name := "cluster-" + rand.String(10)
	caServer := fmt.Sprintf("%s-ca-server", name)
	caClient := fmt.Sprintf("%s-ca-client", name)

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: instances,
			Certificates: &apiv1.CertificatesConfiguration{
				ServerCASecret: caServer,
				ClientCASecret: caClient,
			},
			StorageConfiguration: apiv1.StorageConfiguration{
				Size: "1G",
			},
			WalStorage: &apiv1.StorageConfiguration{
				Size: "1G",
			},
		},
		Status: apiv1.ClusterStatus{
			Instances:                instances,
			SecretsResourceVersion:   apiv1.SecretsResourceVersion{},
			ConfigMapResourceVersion: apiv1.ConfigMapResourceVersion{},
			Certificates: apiv1.CertificatesStatus{
				CertificatesConfiguration: apiv1.CertificatesConfiguration{
					ServerCASecret: caServer,
					ClientCASecret: caClient,
				},
			},
		},
	}

	cluster.SetDefaults()

	err := k8sClient.Create(context.Background(), cluster)
	Expect(err).ToNot(HaveOccurred())

	// upstream issue, go client cleans typemeta: https://github.com/kubernetes/client-go/issues/308
	cluster.TypeMeta = metav1.TypeMeta{
		Kind:       apiv1.ClusterKind,
		APIVersion: apiv1.SchemeGroupVersion.String(),
	}

	return cluster
}

func newFakeNamespace(k8sClient client.Client) string {
	name := rand.String(10)

	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}
	err := k8sClient.Create(context.Background(), namespace)
	Expect(err).ToNot(HaveOccurred())

	return name
}

func getPoolerDeployment(ctx context.Context, k8sClient client.Client, pooler *apiv1.Pooler) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	err := k8sClient.Get(
		ctx,
		types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace},
		deployment,
	)
	Expect(err).ToNot(HaveOccurred())

	return deployment
}

func generateFakeClusterPods(
	c client.Client,
	cluster *apiv1.Cluster,
	markAsReady bool,
) []corev1.Pod {
	var idx int
	var pods []corev1.Pod
	for idx < cluster.Spec.Instances {
		idx++
		pod, _ := specs.NewInstance(context.TODO(), *cluster, idx, true)
		cluster.SetInheritedDataAndOwnership(&pod.ObjectMeta)

		err := c.Create(context.Background(), pod)
		Expect(err).ToNot(HaveOccurred())

		// we overwrite local status, needed for certain tests. The status returned from fake api server will be always
		// 'Pending'
		if markAsReady {
			pod.Status = corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.ContainersReady,
						Status: corev1.ConditionTrue,
					},
				},
			}
		}
		pods = append(pods, *pod)
	}
	return pods
}

func generateFakeClusterPodsWithDefaultClient(
	k8sClient client.Client,
	cluster *apiv1.Cluster,
	markAsReady bool,
) []corev1.Pod {
	return generateFakeClusterPods(k8sClient, cluster, markAsReady)
}

func generateFakeInitDBJobs(c client.Client, cluster *apiv1.Cluster) []batchv1.Job {
	var idx int
	var jobs []batchv1.Job
	for idx < cluster.Spec.Instances {
		idx++
		job := specs.CreatePrimaryJobViaInitdb(*cluster, idx)
		cluster.SetInheritedDataAndOwnership(&job.ObjectMeta)

		err := c.Create(context.Background(), job)
		Expect(err).ToNot(HaveOccurred())
		jobs = append(jobs, *job)
	}
	return jobs
}

func generateFakeInitDBJobsWithDefaultClient(k8sClient client.Client, cluster *apiv1.Cluster) []batchv1.Job {
	return generateFakeInitDBJobs(k8sClient, cluster)
}

func generateClusterPVC(
	c client.Client,
	cluster *apiv1.Cluster,
	status persistentvolumeclaim.PVCStatus, // nolint:unparam
) []corev1.PersistentVolumeClaim {
	var idx int
	var pvcs []corev1.PersistentVolumeClaim
	for idx < cluster.Spec.Instances {
		idx++
		pvcs = append(pvcs, newFakePVC(c, cluster, idx, status)...)
	}
	return pvcs
}

func newFakePVC(
	c client.Client,
	cluster *apiv1.Cluster,
	serial int,
	status persistentvolumeclaim.PVCStatus,
) []corev1.PersistentVolumeClaim {
	var pvcGroup []corev1.PersistentVolumeClaim
	pvc, err := persistentvolumeclaim.Build(
		cluster,
		&persistentvolumeclaim.CreateConfiguration{
			Status:     status,
			NodeSerial: serial,
			Calculator: persistentvolumeclaim.NewPgDataCalculator(),
			Storage:    cluster.Spec.StorageConfiguration,
		})
	Expect(err).ToNot(HaveOccurred())
	cluster.SetInheritedDataAndOwnership(&pvc.ObjectMeta)

	err = c.Create(context.Background(), pvc)
	Expect(err).ToNot(HaveOccurred())
	pvcGroup = append(pvcGroup, *pvc)

	if cluster.ShouldCreateWalArchiveVolume() {
		pvcWal, err := persistentvolumeclaim.Build(
			cluster,
			&persistentvolumeclaim.CreateConfiguration{
				Status:     status,
				NodeSerial: serial,
				Calculator: persistentvolumeclaim.NewPgWalCalculator(),
				Storage:    cluster.Spec.StorageConfiguration,
			},
		)
		Expect(err).ToNot(HaveOccurred())
		cluster.SetInheritedDataAndOwnership(&pvcWal.ObjectMeta)
		err = c.Create(context.Background(), pvcWal)
		Expect(err).ToNot(HaveOccurred())
		pvcGroup = append(pvcGroup, *pvcWal)
	}

	return pvcGroup
}

// generateFakeCASecret follows the conventions established by cert.GenerateCASecret
func generateFakeCASecret(c client.Client, name, namespace, domain string) (*corev1.Secret, *certs.KeyPair) {
	keyPair, err := certs.CreateRootCA(domain, namespace)
	Expect(err).ToNot(HaveOccurred())
	secret := &corev1.Secret{
		Type: corev1.SecretTypeOpaque,
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			certs.CAPrivateKeyKey: keyPair.Private,
			certs.CACertKey:       keyPair.Certificate,
		},
	}

	err = c.Create(context.Background(), secret)
	Expect(err).ToNot(HaveOccurred())

	return secret, keyPair
}

func expectResourceExists(c client.Client, name, namespace string, resource client.Object) {
	err := c.Get(
		context.Background(),
		types.NamespacedName{Name: name, Namespace: namespace},
		resource,
	)
	Expect(err).ToNot(HaveOccurred())
}

func expectResourceDoesntExist(c client.Client, name, namespace string, resource client.Object) {
	err := c.Get(
		context.Background(),
		types.NamespacedName{Name: name, Namespace: namespace},
		resource,
	)
	Expect(apierrors.IsNotFound(err)).To(BeTrue())
}

type (
	indexAdapter               func(list client.ObjectList, opts ...client.ListOption) client.ObjectList
	fakeClientWithIndexAdapter struct {
		client.Client
		indexerAdapters []indexAdapter
	}
)

func (f fakeClientWithIndexAdapter) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	var optsWithoutMatchingFields []client.ListOption
	// matchingFields rely on indexes that we don't have on the default kube client
	var matchingFields []client.ListOption // nolint:prealloc
	for _, opt := range opts {
		_, ok := opt.(client.MatchingFields)
		if !ok {
			optsWithoutMatchingFields = append(optsWithoutMatchingFields, opt)
			continue
		}
		matchingFields = append(matchingFields, opt)
	}

	err := f.Client.List(ctx, list, optsWithoutMatchingFields...)

	// we try to process the index filters
	for _, filter := range f.indexerAdapters {
		list = filter(list, matchingFields...)
	}

	return err
}

func clusterDefaultQueriesFalsePathIndexAdapter(list client.ObjectList, opts ...client.ListOption) client.ObjectList {
	var matchesFilter bool
	for _, opt := range opts {
		res, ok := opt.(client.MatchingFields)
		if !ok {
			continue
		}
		if res[disableDefaultQueriesSpecPath] == "false" {
			matchesFilter = true
		}
	}

	if !matchesFilter {
		return list
	}

	clusterList, ok := list.(*apiv1.ClusterList)
	if !ok {
		return list
	}

	var filteredClusters []apiv1.Cluster
	for _, cluster := range clusterList.Items {
		if cluster.Spec.Monitoring != nil && cluster.Spec.Monitoring.DisableDefaultQueries != nil {
			if !*cluster.Spec.Monitoring.DisableDefaultQueries {
				filteredClusters = append(filteredClusters, cluster)
			}
		}
	}

	clusterList.Items = filteredClusters
	return clusterList
}
