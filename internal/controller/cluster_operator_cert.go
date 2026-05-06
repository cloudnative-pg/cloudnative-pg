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
	"crypto/x509"
	"fmt"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
)

// reconcileOperatorCertificateFingerprint ensures the cluster status contains the
// SHA-256 fingerprint of the operator's current in-memory client certificate.
// If the fingerprint is missing or stale, it patches the status and requests a
// short requeue so the instance manager can pick up the new value before any
// authenticated call is attempted.
func (r *ClusterReconciler) reconcileOperatorCertificateFingerprint(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (ctrl.Result, error) {
	fingerprint, err := r.operatorCertificateFingerprint()
	if err != nil {
		return ctrl.Result{}, err
	}

	if cluster.Status.OperatorCertificateFingerprint == fingerprint {
		return ctrl.Result{}, nil
	}

	orig := cluster.DeepCopy()
	cluster.Status.OperatorCertificateFingerprint = fingerprint
	if err := r.Status().Patch(ctx, cluster, client.MergeFrom(orig)); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching operator certificate fingerprint: %w", err)
	}

	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// operatorCertificateFingerprint returns the SHA-256 public key fingerprint of
// the operator's in-memory client certificate.
func (r *ClusterReconciler) operatorCertificateFingerprint() (string, error) {
	if r.OperatorClientCert == nil || len(r.OperatorClientCert.Certificate) == 0 {
		return "", fmt.Errorf("operator client certificate is not set")
	}

	cert, err := x509.ParseCertificate(r.OperatorClientCert.Certificate[0])
	if err != nil {
		return "", fmt.Errorf("parsing operator client certificate: %w", err)
	}

	return certs.PublicKeyFingerprint(cert), nil
}
