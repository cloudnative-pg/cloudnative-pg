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

package utils

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/thoas/go-funk"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	// Import the client auth plugin package to allow use gke or ake to run tests
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	. "github.com/onsi/gomega" // nolint
)

const (
	// RetryTimeout retry timeout (in seconds) when a client api call or kubectl cli request get failed
	RetryTimeout = 60
	// RetryAttempts maximum number of attempts when it fails in `retry`. Mainly used in `RunUncheckedRetry`
	RetryAttempts = 5
	// PollingTime polling interval (in seconds) between retries
	PollingTime = 5
)

// TestingEnvironment struct for operator testing
type TestingEnvironment struct {
	RestClientConfig   *rest.Config
	Client             client.Client
	Interface          kubernetes.Interface
	APIExtensionClient apiextensionsclientset.Interface
	Ctx                context.Context
	Scheme             *runtime.Scheme
	PreserveNamespaces []string
	Log                logr.Logger
	PostgresVersion    int
	createdNamespaces  *uniqueStringSlice
	AzureConfiguration AzureConfiguration
}

type uniqueStringSlice struct {
	values []string
	mu     sync.RWMutex
}

func (a *uniqueStringSlice) generateUniqueName(prefix string) string {
	a.mu.Lock()
	defer a.mu.Unlock()

	for {
		potentialUniqueName := fmt.Sprintf("%s-%d", prefix, funk.RandomInt(0, 9999))
		if !slices.Contains(a.values, potentialUniqueName) {
			a.values = append(a.values, potentialUniqueName)
			return potentialUniqueName
		}
	}
}

// NewTestingEnvironment creates the environment for testing
func NewTestingEnvironment() (*TestingEnvironment, error) {
	var env TestingEnvironment
	var err error
	env.RestClientConfig = ctrl.GetConfigOrDie()
	env.Interface = kubernetes.NewForConfigOrDie(env.RestClientConfig)
	env.APIExtensionClient = apiextensionsclientset.NewForConfigOrDie(env.RestClientConfig)
	env.Ctx = context.Background()
	env.Scheme = runtime.NewScheme()

	if err := storagesnapshotv1.AddToScheme(env.Scheme); err != nil {
		return nil, err
	}

	flags := log.NewFlags(zap.Options{
		Development: true,
	})
	log.SetLogLevel(log.DebugLevelString)
	flags.ConfigureLogging()
	env.Log = log.GetLogger().WithName("e2e").GetLogger()
	log.SetLogger(env.Log)

	env.createdNamespaces = &uniqueStringSlice{}

	postgresImage := versions.DefaultImageName

	// Fetching postgres image version.
	if postgresImageFromUser, exist := os.LookupEnv("POSTGRES_IMG"); exist {
		postgresImage = postgresImageFromUser
	}
	imageReference := utils.NewReference(postgresImage)
	postgresImageVersion, err := postgres.GetPostgresVersionFromTag(imageReference.Tag)
	if err != nil {
		return nil, err
	}
	env.PostgresVersion = postgresImageVersion / 10000

	env.Client, err = client.New(env.RestClientConfig, client.Options{Scheme: env.Scheme})
	if err != nil {
		return nil, err
	}

	if preserveNamespaces := os.Getenv("PRESERVE_NAMESPACES"); preserveNamespaces != "" {
		env.PreserveNamespaces = strings.Fields(preserveNamespaces)
	}

	clientDiscovery, err := utils.GetDiscoveryClient()
	if err != nil {
		return nil, fmt.Errorf("could not get the discovery client: %w", err)
	}

	err = utils.DetectSeccompSupport(clientDiscovery)
	if err != nil {
		return nil, fmt.Errorf("could not detect SeccompProfile support: %w", err)
	}

	err = utils.DetectSecurityContextConstraints(clientDiscovery)
	if err != nil {
		return nil, fmt.Errorf("could not detect SeccompProfile support: %w", err)
	}

	env.AzureConfiguration = newAzureConfigurationFromEnv()

	return &env, nil
}

// EventuallyExecCommand wraps the utils.ExecCommand pre-setting values constant during
// tests, wrapping it with an Eventually clause
func (env TestingEnvironment) EventuallyExecCommand(
	ctx context.Context,
	pod corev1.Pod,
	containerName string,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	var stdOut, stdErr string
	var err error
	Eventually(func() error {
		stdOut, stdErr, err = utils.ExecCommand(ctx, env.Interface, env.RestClientConfig,
			pod, containerName, timeout, command...)
		if err != nil {
			return err
		}
		return nil
	}, RetryTimeout, PollingTime).Should(BeNil())
	return stdOut, stdErr, err
}

// ExecCommand wraps the utils.ExecCommand pre-setting values constant during
// tests
func (env TestingEnvironment) ExecCommand(
	ctx context.Context,
	pod corev1.Pod,
	containerName string,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	return utils.ExecCommand(ctx, env.Interface, env.RestClientConfig,
		pod, containerName, timeout, command...)
}

// ExecCommandWithPsqlClient wraps the utils.ExecCommand pre-setting values and
// run query on psql client pod with rw service as host.
func (env TestingEnvironment) ExecCommandWithPsqlClient(
	namespace,
	clusterName string,
	pod *corev1.Pod,
	secretSuffix string,
	dbname string,
	query string,
) (string, string, error) {
	timeout := time.Second * 10
	username, password, err := GetCredentials(clusterName, namespace, secretSuffix, &env)
	if err != nil {
		return "", "", err
	}
	rwService, err := GetRwServiceObject(namespace, clusterName, &env)
	if err != nil {
		return "", "", err
	}
	host := CreateServiceFQDN(namespace, rwService.GetName())
	dsn := CreateDSN(host, username, dbname, password, Prefer, 5432)
	return utils.ExecCommand(env.Ctx, env.Interface, env.RestClientConfig,
		*pod, specs.PostgresContainerName, &timeout, "psql", dsn, "-tAc", query)
}

// GetPVCList gathers the current list of PVCs in a namespace
func (env TestingEnvironment) GetPVCList(namespace string) (*corev1.PersistentVolumeClaimList, error) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	err := env.Client.List(
		env.Ctx, pvcList, client.InNamespace(namespace),
	)
	return pvcList, err
}

// GetSnapshotList gathers the current list of VolumeSnapshots in a namespace
func (env TestingEnvironment) GetSnapshotList(namespace string) (*storagesnapshotv1.VolumeSnapshotList, error) {
	list := &storagesnapshotv1.VolumeSnapshotList{}
	err := env.Client.List(env.Ctx, list, client.InNamespace(namespace))

	return list, err
}

// GetJobList gathers the current list of jobs in a namespace
func (env TestingEnvironment) GetJobList(namespace string) (*batchv1.JobList, error) {
	jobList := &batchv1.JobList{}
	err := env.Client.List(
		env.Ctx, jobList, client.InNamespace(namespace),
	)
	return jobList, err
}

// GetServiceAccountList gathers the current list of jobs in a namespace
func (env TestingEnvironment) GetServiceAccountList(namespace string) (*corev1.ServiceAccountList, error) {
	serviceAccountList := &corev1.ServiceAccountList{}
	err := env.Client.List(
		env.Ctx, serviceAccountList, client.InNamespace(namespace),
	)
	return serviceAccountList, err
}

// GetEventList gathers the current list of events in a namespace
func (env TestingEnvironment) GetEventList(namespace string) (*eventsv1.EventList, error) {
	eventList := &eventsv1.EventList{}
	err := env.Client.List(
		env.Ctx, eventList, client.InNamespace(namespace),
	)
	return eventList, err
}

// GetNodeList gathers the current list of Nodes
func (env TestingEnvironment) GetNodeList() (*corev1.NodeList, error) {
	nodeList := &corev1.NodeList{}
	err := env.Client.List(env.Ctx, nodeList, client.InNamespace(""))
	return nodeList, err
}

// GetBackupList gathers the current list of backup in namespace
func (env TestingEnvironment) GetBackupList(namespace string) (*apiv1.BackupList, error) {
	backupList := &apiv1.BackupList{}
	err := env.Client.List(
		env.Ctx, backupList, client.InNamespace(namespace),
	)
	return backupList, err
}

// GetScheduledBackupList gathers the current list of scheduledBackup in namespace
func (env TestingEnvironment) GetScheduledBackupList(namespace string) (*apiv1.ScheduledBackupList, error) {
	scheduledBackupList := &apiv1.ScheduledBackupList{}
	err := env.Client.List(
		env.Ctx, scheduledBackupList, client.InNamespace(namespace),
	)
	return scheduledBackupList, err
}

// GetResourceNamespacedNameFromYAML returns the NamespacedName representing a resource in a YAML file
func (env TestingEnvironment) GetResourceNamespacedNameFromYAML(path string) (types.NamespacedName, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return types.NamespacedName{}, err
	}
	decoder := serializer.NewCodecFactory(env.Scheme).UniversalDeserializer()
	obj, _, err := decoder.Decode(data, nil, nil)
	if err != nil {
		return types.NamespacedName{}, err
	}
	objectMeta, err := meta.Accessor(obj)
	if err != nil {
		return types.NamespacedName{}, err
	}
	return types.NamespacedName{Namespace: objectMeta.GetNamespace(), Name: objectMeta.GetName()}, nil
}

// GetResourceNameFromYAML returns the name of a resource in a YAML file
func (env TestingEnvironment) GetResourceNameFromYAML(path string) (string, error) {
	namespacedName, err := env.GetResourceNamespacedNameFromYAML(path)
	if err != nil {
		return "", err
	}
	return namespacedName.Name, err
}

// GetResourceNamespaceFromYAML returns the namespace of a resource in a YAML file
func (env TestingEnvironment) GetResourceNamespaceFromYAML(path string) (string, error) {
	namespacedName, err := env.GetResourceNamespacedNameFromYAML(path)
	if err != nil {
		return "", err
	}
	return namespacedName.Namespace, err
}

// GetPoolerList gathers the current list of poolers in a namespace
func (env TestingEnvironment) GetPoolerList(namespace string) (*apiv1.PoolerList, error) {
	poolerList := &apiv1.PoolerList{}

	err := env.Client.List(
		env.Ctx, poolerList, client.InNamespace(namespace))

	return poolerList, err
}

// DumpPoolerResourcesInfo logs the JSON for the pooler resources in a namespace, its pods, Deployment,
// services and endpoints
func (env TestingEnvironment) DumpPoolerResourcesInfo(namespace, currentTestName string) {
	poolerList, err := env.GetPoolerList(namespace)
	if err != nil {
		return
	}
	if len(poolerList.Items) > 0 {
		for _, pooler := range poolerList.Items {
			// it will create a filename along with pooler name and currentTest name
			fileName := "out/" + fmt.Sprintf("%v-%v.log", currentTestName, pooler.GetName())
			f, err := os.Create(filepath.Clean(fileName))
			if err != nil {
				fmt.Println(err)
				return
			}
			w := bufio.NewWriter(f)

			// dump pooler info
			out, _ := json.MarshalIndent(pooler, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v pooler\n", namespace, pooler.Name)
			_, _ = fmt.Fprintln(w, string(out))

			// pooler name used as resources name like Service, Deployment, EndPoints name info
			poolerName := pooler.GetName()
			namespacedName := types.NamespacedName{
				Namespace: namespace,
				Name:      poolerName,
			}

			// dump pooler endpoints info
			endpoint := &corev1.Endpoints{}
			_ = env.Client.Get(env.Ctx, namespacedName, endpoint)
			out, _ = json.MarshalIndent(endpoint, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v endpoint\n", namespace, endpoint.Name)
			_, _ = fmt.Fprintln(w, string(out))

			// dump pooler Service info
			service := &corev1.Service{}
			_ = env.Client.Get(env.Ctx, namespacedName, service)
			out, _ = json.MarshalIndent(service, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v Service\n", namespace, service.Name)
			_, _ = fmt.Fprintln(w, string(out))

			// dump pooler pods info
			podList := &corev1.PodList{}
			_ = env.Client.List(env.Ctx, podList, client.InNamespace(namespace),
				client.MatchingLabels{utils.PgbouncerNameLabel: poolerName})
			for _, pod := range podList.Items {
				out, _ = json.MarshalIndent(pod, "", "    ")
				_, _ = fmt.Fprintf(w, "Dumping %v/%v pod\n", namespace, pod.Name)
				_, _ = fmt.Fprintln(w, string(out))
			}

			// dump Deployment info
			deployment := &appsv1.Deployment{}
			_ = env.Client.Get(env.Ctx, namespacedName, deployment)
			out, _ = json.MarshalIndent(deployment, "", "    ")
			_, _ = fmt.Fprintf(w, "Dumping %v/%v Deployment\n", namespace, deployment.Name)
			_, _ = fmt.Fprintln(w, string(out))
		}
	} else {
		return
	}
}

// TailClusterLogs streams the cluster pod logs to a single output io.Writer,
// starting from the current time, and watching for any new pods, and any new logs,
// until the  context is cancelled or there are no pods left.
//
// If `parseTimestamps` is true, the log line will have the timestamp in
// human-readable prepended. NOTE: this will make log-lines NON-JSON
func (env TestingEnvironment) TailClusterLogs(
	cluster *apiv1.Cluster,
	writer io.Writer,
	parseTimestamps bool,
) error {
	now := metav1.Now()
	streamClusterLogs := logs.ClusterStreamingRequest{
		Cluster: cluster,
		Options: &corev1.PodLogOptions{
			Timestamps: parseTimestamps,
			Follow:     true,
			SinceTime:  &now,
		},
		FollowWaiting: logs.DefaultFollowWaiting,
		Client:        env.Interface,
	}
	return streamClusterLogs.SingleStream(env.Ctx, writer)
}
