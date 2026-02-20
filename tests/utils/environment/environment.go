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

package environment

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	"github.com/go-logr/logr"
	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	storageutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/storage"

	// Import the client auth plugin package to allow use gke or ake to run tests
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	. "github.com/onsi/ginkgo/v2" // nolint
	. "github.com/onsi/gomega"    // nolint
)

const (
	// RetryTimeout retry timeout (in seconds) when a client api call or kubectl cli request get failed
	RetryTimeout = 60

	// StandardSuffix is the suffix for standard images
	StandardSuffix = "standard-trixie"

	// MinimalSuffix is the suffix for minimal images
	MinimalSuffix = "minimal-trixie"

	// SystemSuffix is the suffix for system images (includes barman-cloud tools)
	SystemSuffix = "system-trixie"

	// PostGISSuffix is the suffix for PostGIS images
	PostGISSuffix = "3-standard-trixie"

	// Official CloudNativePG image repositories
	defaultPostgresImageRepository = "ghcr.io/cloudnative-pg/postgresql"
	defaultPostGISImageRepository  = "ghcr.io/cloudnative-pg/postgis"
)

// TestingEnvironment struct for operator testing
type TestingEnvironment struct {
	RestClientConfig           *rest.Config
	Client                     client.Client
	Interface                  kubernetes.Interface
	APIExtensionClient         apiextensionsclientset.Interface
	Ctx                        context.Context
	Scheme                     *runtime.Scheme
	Log                        logr.Logger
	PostgresImageName          string
	PostgresImageTag           string
	PostgresVersion            uint64
	PostgresImageRepository    string
	PostGISImageRepository     string
	DefaultStorageClass        string
	CSIStorageClass            string
	DefaultVolumeSnapshotClass string
	createdNamespaces          *uniqueStringSlice
}

type uniqueStringSlice struct {
	values []string
	mu     sync.RWMutex
}

func (a *uniqueStringSlice) generateUniqueName(prefix string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	process := GinkgoParallelProcess()

	for {
		potentialUniqueName := fmt.Sprintf("%s-%d-%d", prefix, process, funk.RandomInt(0, 9999))
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

	if err := volumesnapshotv1.AddToScheme(env.Scheme); err != nil {
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

	// Fetching postgres image.
	if postgresImageFromUser, exist := os.LookupEnv("POSTGRES_IMG"); exist {
		postgresImage = postgresImageFromUser
	}
	imageReference := reference.New(postgresImage)
	env.PostgresImageName = imageReference.Name
	env.PostgresImageTag = imageReference.Tag

	// Set PostgreSQL image repository (can be overridden via env variable)
	env.PostgresImageRepository = defaultPostgresImageRepository
	if postgresRepoFromUser, exist := os.LookupEnv("POSTGRES_IMG_REPOSITORY"); exist {
		env.PostgresImageRepository = postgresRepoFromUser
	}

	// Set PostGIS image repository (can be overridden via env variable)
	env.PostGISImageRepository = defaultPostGISImageRepository
	if postgisRepoFromUser, exist := os.LookupEnv("POSTGIS_IMG_REPOSITORY"); exist {
		env.PostGISImageRepository = postgisRepoFromUser
	}

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

	// Detect storage class configuration from the cluster, allowing
	// environment variable overrides. This follows the same pattern
	// used for POSTGRES_IMG above.
	if val, ok := os.LookupEnv("E2E_DEFAULT_STORAGE_CLASS"); ok && val != "" {
		env.DefaultStorageClass = val
	} else {
		env.DefaultStorageClass, err = storageutils.GetDefaultStorageClassName(env.Ctx, env.Interface)
		if err != nil {
			return nil, fmt.Errorf("detecting default storage class: %w", err)
		}
	}

	if val, ok := os.LookupEnv("E2E_CSI_STORAGE_CLASS"); ok && val != "" {
		env.CSIStorageClass = val
	} else {
		env.CSIStorageClass, err = storageutils.GetCSIStorageClassName(env.Ctx, env.Interface)
		if err != nil {
			return nil, fmt.Errorf("detecting CSI storage class: %w", err)
		}
	}

	if val, ok := os.LookupEnv("E2E_DEFAULT_VOLUMESNAPSHOT_CLASS"); ok && val != "" {
		env.DefaultVolumeSnapshotClass = val
	} else {
		env.DefaultVolumeSnapshotClass, err = storageutils.GetDefaultVolumeSnapshotClassName(
			env.Ctx, env.Interface, env.CSIStorageClass)
		if err != nil {
			return nil, fmt.Errorf("detecting default volume snapshot class: %w", err)
		}
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
	}, RetryTimeout, objects.PollingTime).Should(Succeed())
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

// StandardImageName returns the full image name for a standard Postgres image.
// Example: ghcr.io/cloudnative-pg/postgresql:17-standard-trixie
func (env *TestingEnvironment) StandardImageName(tag string) string {
	return fmt.Sprintf("%s:%s-%s", env.PostgresImageName, tag, StandardSuffix)
}

// MinimalImageName returns the full image name for a minimal Postgres image.
// Example: ghcr.io/cloudnative-pg/postgresql:17-minimal-trixie
func (env *TestingEnvironment) MinimalImageName(tag string) string {
	return fmt.Sprintf("%s:%s-%s", env.PostgresImageName, tag, MinimalSuffix)
}

// PostGISImageName returns the full image name for the official CloudNativePG PostGIS image.
// Example: ghcr.io/cloudnative-pg/postgis:17-3-standard-trixie
func (env *TestingEnvironment) PostGISImageName(tag string) string {
	return fmt.Sprintf("%s:%s-%s", env.PostGISImageRepository, tag, PostGISSuffix)
}

// OfficialStandardImageName returns the full image name for the official CloudNativePG standard Postgres image.
// This is used for major upgrade tests where source images must come from the official registry.
// Example: ghcr.io/cloudnative-pg/postgresql:16-standard-trixie
func (env *TestingEnvironment) OfficialStandardImageName(tag string) string {
	return fmt.Sprintf("%s:%s-%s", env.PostgresImageRepository, tag, StandardSuffix)
}

// OfficialMinimalImageName returns the full image name for the official CloudNativePG minimal Postgres image.
// This is used for major upgrade tests where source images must come from the official registry.
// Example: ghcr.io/cloudnative-pg/postgresql:16-minimal-trixie
func (env *TestingEnvironment) OfficialMinimalImageName(tag string) string {
	return fmt.Sprintf("%s:%s-%s", env.PostgresImageRepository, tag, MinimalSuffix)
}

// SystemImageName returns the full image name for a system Postgres image.
// System images include barman-cloud tools for backup and recovery.
// Example: ghcr.io/cloudnative-pg/postgresql:17-system-trixie
func (env *TestingEnvironment) SystemImageName(tag string) string {
	return fmt.Sprintf("%s:%s-%s", env.PostgresImageName, tag, SystemSuffix)
}

// OfficialSystemImageName returns the full image name for the official CloudNativePG system Postgres image.
// This is used for major upgrade tests where source images must come from the official registry.
// System images include barman-cloud tools for backup and recovery.
// Example: ghcr.io/cloudnative-pg/postgresql:16-system-trixie
func (env *TestingEnvironment) OfficialSystemImageName(tag string) string {
	return fmt.Sprintf("%s:%s-%s", env.PostgresImageRepository, tag, SystemSuffix)
}
