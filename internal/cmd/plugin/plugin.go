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

// Package plugin contains the common behaviors of the kubectl-cnpg subcommand
package plugin

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
)

var (
	// Namespace to operate in
	Namespace string

	// KubeContext to operate with
	KubeContext string

	// NamespaceExplicitlyPassed indicates if the namespace was passed manually
	NamespaceExplicitlyPassed bool

	// Config is the Kubernetes configuration used
	Config *rest.Config

	// Client is the controller-runtime client
	Client client.Client

	// ClientInterface contains the interface used i the plugin
	ClientInterface kubernetes.Interface
)

const (
	// GroupIDAdmin represents an ID to group up CNPG commands
	GroupIDAdmin = "admin"

	// GroupIDTroubleshooting represent an ID to group up troubleshooting
	// commands
	GroupIDTroubleshooting = "troubleshooting"

	// GroupIDCluster represents an ID to group up Postgres Cluster commands
	GroupIDCluster = "cluster"

	// GroupIDDatabase represents an ID to group up Postgres Database commands
	GroupIDDatabase = "db"

	// GroupIDMiscellaneous represents an ID to group up miscellaneous commands
	GroupIDMiscellaneous = "misc"
)

// SetupKubernetesClient creates a k8s client to be used inside the kubectl-cnpg
// utility
func SetupKubernetesClient(configFlags *genericclioptions.ConfigFlags) error {
	var err error

	kubeconfig := configFlags.ToRawKubeConfigLoader()

	Config, err = kubeconfig.ClientConfig()
	if err != nil {
		return err
	}

	err = createClient(Config)
	if err != nil {
		return err
	}

	Namespace, NamespaceExplicitlyPassed, err = kubeconfig.Namespace()
	if err != nil {
		return err
	}

	KubeContext = *configFlags.Context

	ClientInterface = kubernetes.NewForConfigOrDie(Config)

	return utils.DetectSecurityContextConstraints(ClientInterface.Discovery())
}

func createClient(cfg *rest.Config) error {
	var err error

	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = apiv1.AddToScheme(scheme)
	_ = volumesnapshotv1.AddToScheme(scheme)

	cfg.UserAgent = fmt.Sprintf("kubectl-cnpg/v%s (%s)", versions.Version, versions.Info.Commit)

	Client, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	return nil
}

// CreateAndGenerateObjects creates provided k8s object or generate manifest collectively
func CreateAndGenerateObjects(ctx context.Context, k8sObject []client.Object, option bool) error {
	for _, item := range k8sObject {
		switch option {
		case true:
			if err := Print(item, OutputFormatYAML, os.Stdout); err != nil {
				return err
			}
			fmt.Println("---")
		default:
			objectType := item.GetObjectKind().GroupVersionKind().Kind
			if err := Client.Create(ctx, item); err != nil {
				return err
			}
			fmt.Printf("%v/%v created\n", objectType, item.GetName())
		}
	}

	return nil
}

// GetPGControlData obtains the PgControldata from the passed pod by doing an exec.
// This approach should be used only in the plugin commands.
func GetPGControlData(
	ctx context.Context,
	pod corev1.Pod,
) (string, error) {
	timeout := time.Second * 10
	clientInterface := kubernetes.NewForConfigOrDie(Config)
	stdout, _, err := utils.ExecCommand(
		ctx,
		clientInterface,
		Config,
		pod,
		specs.PostgresContainerName,
		&timeout,
		"pg_controldata")
	if err != nil {
		return "", err
	}

	return stdout, nil
}

// completeClusters is mainly used inside the unit tests
func completeClusters(
	ctx context.Context,
	cli client.Client,
	namespace string,
	args []string,
	toComplete string,
) []string {
	var clusters apiv1.ClusterList

	// Since all our commands work on one cluster, if we already have one in the list
	// we just return an empty set of strings
	if len(args) == 1 {
		return []string{}
	}

	// Get the cluster lists object if error we just return empty array string
	if err := cli.List(ctx, &clusters, client.InNamespace(namespace)); err != nil {
		// We can't list the clusters, so we cannot provide any completion.
		// Unfortunately there's no way for us to provide an error message
		// notifying the user of what is happening.
		return []string{}
	}

	clustersNames := make([]string, 0, len(clusters.Items))
	for _, cluster := range clusters.Items {
		if len(toComplete) == 0 || strings.HasPrefix(cluster.Name, toComplete) {
			clustersNames = append(clustersNames, cluster.Name)
		}
	}

	return clustersNames
}

// CompleteClusters will complete the cluster name when necessary getting the list from the current namespace
func CompleteClusters(ctx context.Context, args []string, toComplete string) []string {
	return completeClusters(ctx, Client, Namespace, args, toComplete)
}

// RequiresArguments will show the help message in case no argument has been provided
func RequiresArguments(nArgs int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) < nArgs {
			_ = cmd.Help()
			os.Exit(0)
		}
		return nil
	}
}
