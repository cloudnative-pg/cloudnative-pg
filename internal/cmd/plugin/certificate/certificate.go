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

// Package certificate implement the kubectl-cnp certificate command
package certificate

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
)

// Params are the required information to create an user secret
type Params struct {
	Name        string
	Namespace   string
	User        string
	ClusterName string
}

// Generate generates a Kubernetes secret suitable to allow certificate authentication
// for a PostgreSQL user
func Generate(ctx context.Context, params Params, dryRun bool, format plugin.OutputFormat) error {
	var secret corev1.Secret

	var cluster apiv1.Cluster
	err := plugin.Client.Get(ctx,
		client.ObjectKey{Namespace: params.Namespace, Name: params.ClusterName},
		&cluster)
	if err != nil {
		return err
	}

	err = plugin.Client.Get(
		ctx,
		client.ObjectKey{Namespace: params.Namespace, Name: cluster.GetClientCASecretName()},
		&secret)
	if err != nil {
		return err
	}

	caPair, err := certs.ParseCASecret(&secret)
	if err != nil {
		return err
	}

	userPair, err := caPair.CreateAndSignPair(params.User, certs.CertTypeClient, nil)
	if err != nil {
		return err
	}

	userSecret := userPair.GenerateCertificateSecret(params.Namespace, params.Name)
	err = plugin.Print(userSecret, format, os.Stdout)
	if err != nil {
		return err
	}

	if dryRun {
		return nil
	}

	err = plugin.Client.Create(ctx, userSecret)
	if err != nil {
		return err
	}

	fmt.Printf("secret/%v created\n", userSecret.Name)
	return nil
}
