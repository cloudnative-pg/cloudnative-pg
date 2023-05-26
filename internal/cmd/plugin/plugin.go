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

// Package plugin contains the common behaviors of the kubectl-cnpg subcommand
package plugin

import (
	"context"
	"fmt"
	"os"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

var (
	// Namespace to operate in
	Namespace string

	// NamespaceExplicitlyPassed indicates if the namespace was passed manually
	NamespaceExplicitlyPassed bool

	// Config is the Kubernetes configuration used
	Config *rest.Config

	// Client is the controller-runtime client
	Client client.Client
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

	return nil
}

func createClient(cfg *rest.Config) error {
	var err error
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = apiv1.AddToScheme(scheme)
	_ = storagesnapshotv1.AddToScheme(scheme)

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
