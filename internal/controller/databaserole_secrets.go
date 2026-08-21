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

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// The operator generates the password and the client certificate of a role into
// Secrets it owns, and reports in the status of the role when it finds one of
// those names taken by a Secret somebody else manages.
const (
	secretNotOwnedMessage     = "Secret %q already exists and is not owned by this DatabaseRole"
	secretNotDeletableMessage = "Secret %q is not owned by this DatabaseRole and will not be deleted automatically"
)

// getReferencedCluster returns the cluster the role belongs to, or nil when it
// does not exist: the role is reconciled again when the cluster appears.
func (r *DatabaseRoleReconciler) getReferencedCluster(
	ctx context.Context,
	role *apiv1.DatabaseRole,
) (*apiv1.Cluster, error) {
	var cluster apiv1.Cluster
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: role.Namespace,
		Name:      role.Spec.ClusterRef.Name,
	}, &cluster); apierrs.IsNotFound(err) {
		log.FromContext(ctx).Info("cluster not found, will retry when it appears",
			"cluster", role.Spec.ClusterRef.Name)
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("while getting cluster %q: %w", role.Spec.ClusterRef.Name, err)
	}
	return &cluster, nil
}

// deleteOwnedSecret deletes a Secret the operator generated for the role. It
// returns false when a Secret with that name exists and the role does not own
// it: the Secret is left alone, and the caller says so in the status.
func (r *DatabaseRoleReconciler) deleteOwnedSecret(
	ctx context.Context,
	role *apiv1.DatabaseRole,
	secretKey client.ObjectKey,
) (bool, error) {
	var secret corev1.Secret
	if err := r.Get(ctx, secretKey, &secret); apierrs.IsNotFound(err) {
		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("while getting secret %q: %w", secretKey.Name, err)
	}

	if !metav1.IsControlledBy(&secret, role) {
		log.FromContext(ctx).Warning("secret exists but is not owned by this DatabaseRole, skipping deletion",
			"secret", secretKey.Name)
		return false, nil
	}

	if err := r.Delete(ctx, &secret); err != nil && !apierrs.IsNotFound(err) {
		return false, fmt.Errorf("while deleting secret %q: %w", secretKey.Name, err)
	}
	return true, nil
}
