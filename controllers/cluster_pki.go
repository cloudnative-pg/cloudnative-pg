/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

const (
	// CaSecretName is the name of the secret which is hosting the Operator CA
	CaSecretName = "postgresql-operator-ca-secret" // #nosec
)

// createPostgresPKI create all the PKI infrastructure that PostgreSQL need to work
// if using ssl=on
func (r *ClusterReconciler) createPostgresPKI(ctx context.Context, cluster *apiv1.Cluster) error {
	// This is the CA of cluster
	caSecret, err := r.ensureCASecret(ctx, cluster)
	if err != nil {
		return fmt.Errorf("generating CA certificate: %w", err)
	}

	// This is the certificate for the server
	serverCertificateName := client.ObjectKey{Namespace: cluster.GetNamespace(), Name: cluster.GetServerSecretName()}
	err = r.ensureLeafCertificate(
		ctx,
		cluster,
		serverCertificateName,
		cluster.GetServiceReadWriteName(),
		caSecret,
		certs.CertTypeServer,
		[]string{
			cluster.GetServiceReadWriteName(),
			fmt.Sprintf("%v.%v", cluster.GetServiceReadWriteName(), cluster.Namespace),
			fmt.Sprintf("%v.%v.svc", cluster.GetServiceReadWriteName(), cluster.Namespace),
			cluster.GetServiceReadName(),
			fmt.Sprintf("%v.%v", cluster.GetServiceReadName(), cluster.Namespace),
			fmt.Sprintf("%v.%v.svc", cluster.GetServiceReadName(), cluster.Namespace),
			cluster.GetServiceReadOnlyName(),
			fmt.Sprintf("%v.%v", cluster.GetServiceReadOnlyName(), cluster.Namespace),
			fmt.Sprintf("%v.%v.svc", cluster.GetServiceReadOnlyName(), cluster.Namespace),
		})
	if err != nil {
		return fmt.Errorf("generating server certificate: %w", err)
	}

	// Generating postgres client certificate
	replicationSecretName := client.ObjectKey{
		Namespace: cluster.GetNamespace(),
		Name:      cluster.GetReplicationSecretName(),
	}
	err = r.ensureLeafCertificate(
		ctx,
		cluster,
		replicationSecretName,
		apiv1.StreamingReplicationUser,
		caSecret,
		certs.CertTypeClient,
		nil)
	if err != nil {
		return fmt.Errorf("generating server certificate: %w", err)
	}

	return nil
}

// ensureCASecret ensure that the cluster CA really exist and is
// valid
func (r *ClusterReconciler) ensureCASecret(ctx context.Context, cluster *apiv1.Cluster) (*v1.Secret, error) {
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

	derivedCaPair, err := certs.CreateRootCA(cluster.Name, cluster.Namespace)
	if err != nil {
		return nil, fmt.Errorf("while creating the CA of the cluster: %w", err)
	}

	derivedCaSecret := derivedCaPair.GenerateCASecret(cluster.Namespace, cluster.GetCASecretName())
	utils.SetAsOwnedBy(&derivedCaSecret.ObjectMeta, cluster.ObjectMeta, cluster.TypeMeta)
	err = r.Create(ctx, derivedCaSecret)
	return derivedCaSecret, err
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

	privateKey, err := pair.ParseECPrivateKey()
	if err != nil {
		return nil, err
	}

	err = pair.RenewCertificate(privateKey, nil)
	if err != nil {
		return nil, err
	}

	secret.Data[certs.CACertKey] = pair.Certificate
	err = r.Update(ctx, secret)
	if err != nil {
		return secret, nil
	}

	return secret, err
}

// ensureLeafCertificate check if we have a certificate for PostgreSQL and generate/renew it
func (r *ClusterReconciler) ensureLeafCertificate(
	ctx context.Context,
	cluster *apiv1.Cluster,
	secretName client.ObjectKey,
	commonName string,
	caSecret *v1.Secret,
	usage certs.CertType,
	altDNSNames []string,
) error {
	// TODO: refactor and extract this to power kubectl-cnp
	var secret v1.Secret
	err := r.Get(ctx, secretName, &secret)
	if err == nil {
		return r.renewServerCertificate(ctx, caSecret, &secret)
	}

	caPair, err := certs.ParseCASecret(caSecret)
	if err != nil {
		return err
	}

	serverPair, err := caPair.CreateAndSignPair(commonName, usage, altDNSNames)
	if err != nil {
		return err
	}

	serverSecret := serverPair.GenerateServerSecret(secretName.Namespace, secretName.Name)
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
