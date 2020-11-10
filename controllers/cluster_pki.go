/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"fmt"
	"os"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/certs"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/utils"
)

const (
	// CaSecretName is the name of the secret which is hosting the Operator CA
	CaSecretName = "postgresql-operator-ca-secret" // #nosec
)

// GetOperatorNamespaceOrDie get the namespace where the operator is running from
// the environment variables or panic
func GetOperatorNamespaceOrDie() string {
	operatorNamespace := os.Getenv("OPERATOR_NAMESPACE")
	if operatorNamespace == "" {
		panic("missing OPERATOR_NAMESPACE environment variable")
	}
	return operatorNamespace
}

// createPostgresPKI create all the PKI infrastructure that PostgreSQL need to work
// if using ssl=on
func (r *ClusterReconciler) createPostgresPKI(ctx context.Context, cluster *v1alpha1.Cluster) error {
	caSecret, err := r.ensureCASecret(ctx, cluster)
	if err != nil {
		return err
	}

	return r.ensureServerCertificate(ctx, cluster, caSecret)
}

// ensureCASecret ensure that the cluster CA really exist and is
// valid
func (r *ClusterReconciler) ensureCASecret(ctx context.Context, cluster *v1alpha1.Cluster) (*v1.Secret, error) {
	var secret v1.Secret
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.GetNamespace(), Name: cluster.GetCASecretName()}, &secret)
	if err == nil {
		// Verify the validity of this CA and renew it if needed
		_, err = r.renewCASecret(ctx, &secret)
		if err != nil {
			return nil, err
		}

		return &secret, nil
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	caPair, err := r.getOperatorCAPair(ctx)
	if err != nil {
		return nil, err
	}

	derivedCaPair, err := caPair.CreateDerivedCA()
	if err != nil {
		return nil, fmt.Errorf("while creating the CA of the cluster: %w", err)
	}

	derivedCaSecret := derivedCaPair.GenerateCASecret(cluster.Namespace, cluster.GetCASecretName())
	utils.SetAsOwnedBy(&derivedCaSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	err = r.Create(ctx, derivedCaSecret)
	return derivedCaSecret, err
}

// getOperatorCAPair Get the CA Pair of the operator
func (r *ClusterReconciler) getOperatorCAPair(ctx context.Context) (*certs.KeyPair, error) {
	var secret v1.Secret

	err := r.Get(ctx, client.ObjectKey{Namespace: GetOperatorNamespaceOrDie(), Name: CaSecretName}, &secret)
	if err != nil {
		return nil, fmt.Errorf("while getting operator CA: %w", err)
	}

	caPair, err := certs.ParseCASecret(&secret)
	if err != nil {
		return nil, fmt.Errorf("while parsing operator CA: %w", err)
	}

	return caPair, nil
}

// renewCASecret check if this CA secret is valid and renew it if needed
func (r *ClusterReconciler) renewCASecret(ctx context.Context, secret *v1.Secret) (*v1.Secret, error) {
	pair, err := certs.ParseCASecret(secret)
	if err != nil {
		return nil, err
	}

	expiring, err := pair.IsExpiring()
	if err != nil {
		return nil, err
	}
	if !expiring {
		return secret, nil
	}

	operatorCaPair, err := r.getOperatorCAPair(ctx)
	if err != nil {
		return nil, err
	}

	operatorCaCert, err := operatorCaPair.ParseCertificate()
	if err != nil {
		return nil, err
	}

	operatorCaPrivateKey, err := operatorCaPair.ParseECPrivateKey()
	if err != nil {
		return nil, err
	}

	err = pair.RenewCertificate(operatorCaPrivateKey, operatorCaCert)
	if err != nil {
		return nil, err
	}

	secret.Data["ca.crt"] = pair.Certificate
	err = r.Update(ctx, secret)
	if err != nil {
		return secret, nil
	}

	return secret, err
}

// ensureServerCertificate check if we have a certificate for PostgreSQL and generate/renew it
func (r *ClusterReconciler) ensureServerCertificate(
	ctx context.Context,
	cluster *v1alpha1.Cluster, caSecret *v1.Secret) error {
	var secret v1.Secret
	err := r.Get(ctx, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.GetServerSecretName()}, &secret)
	if err == nil {
		return r.renewServerCertificate(ctx, caSecret, &secret)
	}

	caPair, err := certs.ParseCASecret(caSecret)
	if err != nil {
		return err
	}

	clusterHostname := fmt.Sprintf(
		"%v.%v.svc",
		cluster.Name,
		cluster.Namespace)
	serverPair, err := caPair.CreateAndSignPair(clusterHostname)
	if err != nil {
		return err
	}

	serverSecret := serverPair.GenerateServerSecret(cluster.Namespace, cluster.GetServerSecretName())
	utils.SetAsOwnedBy(&serverSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	return r.Create(ctx, serverSecret)
}

// renewServerCertificate renew a server certificate giving the certificate that contains the CA that sign it
func (r *ClusterReconciler) renewServerCertificate(ctx context.Context, caSecret *v1.Secret, secret *v1.Secret) error {
	hasBeenRenewed, err := certs.RenewLeafCertificate(caSecret, secret)
	if err != nil {
		return err
	}
	if hasBeenRenewed {
		return r.Update(ctx, secret)
	}

	return nil
}
