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

package environment

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	"github.com/go-logr/logr"
	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"

	// Import the client auth plugin package to allow use gke or ake to run tests
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	. "github.com/onsi/gomega" // nolint
)

const (
	// RetryTimeout retry timeout (in seconds) when a client api call or kubectl cli request get failed
	RetryTimeout = 60
)

// TestingEnvironment struct for operator testing
type TestingEnvironment struct {
	RestClientConfig   *rest.Config
	Client             client.Client
	Interface          kubernetes.Interface
	APIExtensionClient apiextensionsclientset.Interface
	Ctx                context.Context
	Scheme             *runtime.Scheme
	Log                logr.Logger
	PostgresVersion    uint64
	createdNamespaces  *uniqueStringSlice
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

	if err := monitoringv1.AddToScheme(env.Scheme); err != nil {
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
	imageReference := reference.New(postgresImage)
	postgresImageVersion, err := version.FromTag(imageReference.Tag)
	if err != nil {
		return nil, err
	}
	env.PostgresVersion = postgresImageVersion.Major()

	env.Client, err = client.New(env.RestClientConfig, client.Options{Scheme: env.Scheme})
	if err != nil {
		return nil, err
	}

	clientDiscovery, err := utils.GetDiscoveryClient()
	if err != nil {
		return nil, fmt.Errorf("could not get the discovery client: %w", err)
	}

	err = utils.DetectSecurityContextConstraints(clientDiscovery)
	if err != nil {
		return nil, fmt.Errorf("could not detect SeccompProfile support: %w", err)
	}

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
	}, RetryTimeout, objects.PollingTime).Should(BeNil())
	return stdOut, stdErr, err
}

// CreateUniqueTestNamespace creates a namespace by using the passed prefix.
// Return the namespace name and any errors encountered.
// The namespace is automatically cleaned up at the end of the test.
func (env TestingEnvironment) CreateUniqueTestNamespace(
	ctx context.Context,
	crudClient client.Client,
	namespacePrefix string,
	opts ...client.CreateOption,
) (string, error) {
	name := env.createdNamespaces.generateUniqueName(namespacePrefix)

	return name, namespaces.CreateTestNamespace(ctx, crudClient, name, opts...)
}
