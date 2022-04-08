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

package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/pgbouncer/config"
)

// getSecrets loads the data needed to generate the configuration
// from Kubernetes and a Pooler resource
func getSecrets(ctx context.Context, client ctrl.Client, pooler *apiv1.Pooler) (*config.Secrets, error) {
	if pooler.Status.Secrets == nil {
		return nil, fmt.Errorf("status not populated yet")
	}

	var (
		authQuerySecret  corev1.Secret
		serverCASecret   corev1.Secret
		serverCertSecret corev1.Secret
		clientCASecret   corev1.Secret
	)

	authQuerySecretName := pooler.GetAuthQuerySecretName()
	if err := client.Get(ctx,
		types.NamespacedName{
			Name:      authQuerySecretName,
			Namespace: pooler.Namespace,
		},
		&authQuerySecret); err != nil {
		return nil, fmt.Errorf("while getting auth query secret %s: %w", authQuerySecretName, err)
	}

	if err := client.Get(ctx,
		types.NamespacedName{Name: pooler.Status.Secrets.ServerCA.Name, Namespace: pooler.Namespace},
		&serverCASecret); err != nil {
		return nil, fmt.Errorf("while getting server CA secret: %w", err)
	}

	if err := client.Get(ctx,
		types.NamespacedName{Name: pooler.Status.Secrets.ServerTLS.Name, Namespace: pooler.Namespace},
		&serverCertSecret); err != nil {
		return nil, fmt.Errorf("while getting server cert secret: %w", err)
	}

	if err := client.Get(ctx,
		types.NamespacedName{Name: pooler.Status.Secrets.ClientCA.Name, Namespace: pooler.Namespace},
		&clientCASecret); err != nil {
		return nil, fmt.Errorf("while getting client CA secret: %w", err)
	}

	return &config.Secrets{
		AuthQuery: &authQuerySecret,
		ServerCA:  &serverCASecret,
		Client:    &serverCertSecret,
		ClientCA:  &clientCASecret,
	}, nil
}
