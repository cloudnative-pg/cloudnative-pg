/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/specs"
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
			Name:      "svcfriendly-" + rand.String(10),
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

func newFakeCNPCluster(namespace string) *apiv1.Cluster {
	name := "svcfriendly-" + rand.String(10)
	caServer := fmt.Sprintf("%s-ca-server", name)
	caClient := fmt.Sprintf("%s-ca-client", name)

	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: apiv1.ClusterSpec{
			Instances: int32(1),
			Certificates: &apiv1.CertificatesConfiguration{
				ServerCASecret: caServer,
				ClientCASecret: caClient,
			},
			StorageConfiguration: apiv1.StorageConfiguration{
				Size: "1G",
			},
		},
		Status: apiv1.ClusterStatus{
			Instances:                3,
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

	cluster.Default()

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

func generateFakeClusterPods(cluster *apiv1.Cluster, markAsReady bool) []corev1.Pod {
	var idx int32
	var pods []corev1.Pod
	for idx < cluster.Spec.Instances {
		idx++
		pod := specs.PodWithExistingStorage(*cluster, idx)
		SetClusterOwnerAnnotationsAndLabels(&pod.ObjectMeta, cluster)

		err := k8sClient.Create(context.Background(), pod)
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

func generateFakeInitDBJobs(cluster *apiv1.Cluster) []batchv1.Job {
	var idx int32
	var jobs []batchv1.Job
	for idx < cluster.Spec.Instances {
		idx++
		job := specs.CreatePrimaryJobViaInitdb(*cluster, idx)
		SetClusterOwnerAnnotationsAndLabels(&job.ObjectMeta, cluster)

		err := k8sClient.Create(context.Background(), job)
		Expect(err).To(BeNil())
		jobs = append(jobs, *job)
	}
	return jobs
}

func generateFakePVC(cluster *apiv1.Cluster) []corev1.PersistentVolumeClaim {
	var idx int32
	var pvcs []corev1.PersistentVolumeClaim
	for idx < cluster.Spec.Instances {
		idx++

		pvc, err := specs.CreatePVC(cluster.Spec.StorageConfiguration, cluster.Name, cluster.Namespace, idx)
		Expect(err).To(BeNil())
		SetClusterOwnerAnnotationsAndLabels(&pvc.ObjectMeta, cluster)

		err = k8sClient.Create(context.Background(), pvc)
		Expect(err).To(BeNil())
		pvcs = append(pvcs, *pvc)
	}
	return pvcs
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
