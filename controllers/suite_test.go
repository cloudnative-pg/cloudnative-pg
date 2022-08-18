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

package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
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
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	// +kubebuilder:scaffold:imports
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var (
	cfg               *rest.Config
	k8sClient         client.Client
	testEnv           *envtest.Environment
	poolerReconciler  *PoolerReconciler
	clusterReconciler *ClusterReconciler
	scheme            *runtime.Scheme
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controllers Suite")
}

var _ = BeforeSuite(func() {
	By("bootstrapping test environment")

	if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
		By("using existing config for test environment")
		testEnv = &envtest.Environment{}
	} else {
		By("bootstrapping test environment")
		testEnv = &envtest.Environment{
			CRDDirectoryPaths: []string{filepath.Join("..", "config", "crd", "bases")},
		}
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	scheme = runtime.NewScheme()

	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(apiv1.AddToScheme(scheme))

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})

	Expect(err).To(BeNil())

	clusterReconciler = &ClusterReconciler{
		Client:   k8sClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(120),
	}

	poolerReconciler = &PoolerReconciler{
		Client:   k8sClient,
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(120),
	}
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).ToNot(HaveOccurred())
})

func newFakePooler(cluster *apiv1.Cluster) *apiv1.Pooler {
	pooler := &apiv1.Pooler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pooler-" + rand.String(10),
			Namespace: cluster.Namespace,
		},
		Spec: apiv1.PoolerSpec{
			Cluster: apiv1.LocalObjectReference{
				Name: cluster.Name,
			},
			Type:      "rw",
			Instances: 1,
			PgBouncer: &apiv1.PgBouncerSpec{
				PoolMode: apiv1.PgBouncerPoolModeSession,
			},
		},
	}

	err := k8sClient.Create(context.Background(), pooler)
	Expect(err).To(BeNil())

	// upstream issue, go client cleans typemeta: https://github.com/kubernetes/client-go/issues/308
	pooler.TypeMeta = metav1.TypeMeta{
		Kind:       apiv1.PoolerKind,
		APIVersion: apiv1.GroupVersion.String(),
	}

	return pooler
}

func newFakeCNPGCluster(namespace string) *apiv1.Cluster {
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
	Expect(err).To(BeNil())

	// upstream issue, go client cleans typemeta: https://github.com/kubernetes/client-go/issues/308
	cluster.TypeMeta = metav1.TypeMeta{
		Kind:       apiv1.ClusterKind,
		APIVersion: apiv1.GroupVersion.String(),
	}

	return cluster
}

func newFakeNamespace() string {
	name := rand.String(10)

	namespace := &corev1.Namespace{
		TypeMeta: metav1.TypeMeta{},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}
	err := k8sClient.Create(context.Background(), namespace)
	Expect(err).To(BeNil())

	return name
}

func getPoolerDeployment(ctx context.Context, pooler *apiv1.Pooler) *appsv1.Deployment {
	deployment := &appsv1.Deployment{}
	err := k8sClient.Get(
		ctx,
		types.NamespacedName{Name: pooler.Name, Namespace: pooler.Namespace},
		deployment,
	)
	Expect(err).To(BeNil())

	return deployment
}

func generateFakeClusterPods(c client.Client, cluster *apiv1.Cluster, markAsReady bool) []corev1.Pod {
	var idx int
	var pods []corev1.Pod
	for idx < cluster.Spec.Instances {
		idx++
		pod := specs.PodWithExistingStorage(*cluster, idx)
		SetClusterOwnerAnnotationsAndLabels(&pod.ObjectMeta, cluster)

		err := c.Create(context.Background(), pod)
		Expect(err).To(BeNil())

		// we overwrite local status, needed for certain tests. The status returned from fake api server will be always
		// 'Pending'
		if markAsReady {
			pod.Status = corev1.PodStatus{
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

func generateFakeClusterPodsWithDefaultClient(cluster *apiv1.Cluster, markAsReady bool) []corev1.Pod {
	return generateFakeClusterPods(k8sClient, cluster, markAsReady)
}

func generateFakeInitDBJobs(c client.Client, cluster *apiv1.Cluster) []batchv1.Job {
	var idx int
	var jobs []batchv1.Job
	for idx < cluster.Spec.Instances {
		idx++
		job := specs.CreatePrimaryJobViaInitdb(*cluster, idx)
		SetClusterOwnerAnnotationsAndLabels(&job.ObjectMeta, cluster)

		err := c.Create(context.Background(), job)
		Expect(err).To(BeNil())
		jobs = append(jobs, *job)
	}
	return jobs
}

func generateFakeInitDBJobsWithDefaultClient(cluster *apiv1.Cluster) []batchv1.Job {
	return generateFakeInitDBJobs(k8sClient, cluster)
}

func generateFakePVC(c client.Client, cluster *apiv1.Cluster) []corev1.PersistentVolumeClaim {
	var idx int
	var pvcs []corev1.PersistentVolumeClaim
	for idx < cluster.Spec.Instances {
		idx++

		pvc, err := specs.CreatePVC(cluster.Spec.StorageConfiguration, *cluster, idx, utils.PVCRolePgData)
		Expect(err).To(BeNil())
		SetClusterOwnerAnnotationsAndLabels(&pvc.ObjectMeta, cluster)

		err = c.Create(context.Background(), pvc)
		Expect(err).To(BeNil())
		pvcs = append(pvcs, *pvc)
	}
	return pvcs
}

func generateFakePVCWithDefaultClient(cluster *apiv1.Cluster) []corev1.PersistentVolumeClaim {
	return generateFakePVC(k8sClient, cluster)
}

func createManagerWithReconcilers(ctx context.Context) (*ClusterReconciler, *PoolerReconciler, manager.Manager) {
	mgr, err := controllerruntime.NewManager(cfg, controllerruntime.Options{
		Scheme:             scheme,
		LeaderElection:     false,
		MetricsBindAddress: "0",
		Port:               testEnv.WebhookInstallOptions.LocalServingPort,
		Host:               testEnv.WebhookInstallOptions.LocalServingHost,
		CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
	})
	Expect(err).To(BeNil())

	clusterRec := &ClusterReconciler{
		Client:   mgr.GetClient(),
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(120),
	}

	err = clusterRec.SetupWithManager(ctx, mgr)
	Expect(err).To(BeNil())

	poolerRec := &PoolerReconciler{
		Client:   mgr.GetClient(),
		Scheme:   scheme,
		Recorder: record.NewFakeRecorder(120),
	}

	err = poolerRec.SetupWithManager(ctx, mgr)
	Expect(err).To(BeNil())

	return clusterRec, poolerRec, mgr
}

// generateFakeCASecret follows the conventions established by cert.GenerateCASecret
func generateFakeCASecret(c client.Client, name, namespace, domain string) (*corev1.Secret, *certs.KeyPair) {
	keyPair, err := certs.CreateRootCA(domain, namespace)
	Expect(err).To(BeNil())
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
	Expect(err).To(BeNil())

	return secret, keyPair
}

func generateFakeCASecretWithDefaultClient(name, namespace, domain string) (*corev1.Secret, *certs.KeyPair) {
	return generateFakeCASecret(k8sClient, name, namespace, domain)
}

func expectResourceExists(c client.Client, name, namespace string, resource client.Object) {
	err := c.Get(
		context.Background(),
		types.NamespacedName{Name: name, Namespace: namespace},
		resource,
	)
	Expect(err).ToNot(HaveOccurred())
}

func expectResourceExistsWithDefaultClient(name, namespace string, resource client.Object) {
	expectResourceExists(k8sClient, name, namespace, resource)
}

func expectResourceDoesntExist(c client.Client, name, namespace string, resource client.Object) {
	err := c.Get(
		context.Background(),
		types.NamespacedName{Name: name, Namespace: namespace},
		resource,
	)
	Expect(apierrors.IsNotFound(err)).To(BeTrue())
}

func expectResourceDoesntExistWithDefaultClient(name, namespace string, resource client.Object) {
	expectResourceDoesntExist(k8sClient, name, namespace, resource)
}

// withManager bootstraps a manager.Manager inside a ginkgo.It statement
func withManager(callback func(context.Context, *ClusterReconciler, *PoolerReconciler, manager.Manager)) {
	ctx, ctxCancel := context.WithCancel(context.TODO())

	crReconciler, poolerReconciler, mgr := createManagerWithReconcilers(ctx)

	wg := sync.WaitGroup{}

	By("starting the manager", func() {
		wg.Add(1)
		go func() {
			defer GinkgoRecover()
			defer wg.Done()
			err := mgr.Start(ctx)
			Expect(err).ShouldNot(HaveOccurred())
		}()
	})

	callback(ctx, crReconciler, poolerReconciler, mgr)

	By("stopping the manager", func() {
		ctxCancel()
		wg.Wait()
	})
}

func assertRefreshManagerCache(ctx context.Context, manager manager.Manager) {
	By("waiting the cache to sync", func() {
		syncDone := manager.GetCache().WaitForCacheSync(ctx)
		Expect(syncDone).To(BeTrue())
	})
}
