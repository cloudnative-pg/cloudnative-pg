/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package certificate implement the kubectl-cnp certificate command
package certificate

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/cnp"
	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/certs"
)

// Params are the required information to create an user secret
type Params struct {
	Name        string
	Namespace   string
	User        string
	ClusterName string
}

// Generate generate a Kubernetes secret suitable to allow certificate authentication
// for a PostgreSQL user
func Generate(ctx context.Context, params Params, dryRun bool, format cnp.OutputFormat) error {
	secret, err := cnp.GoClient.CoreV1().Secrets(params.Namespace).Get(
		ctx, params.ClusterName+apiv1.CaSecretSuffix, metav1.GetOptions{})
	if err != nil {
		return err
	}

	caPair, err := certs.ParseCASecret(secret)
	if err != nil {
		return err
	}

	userPair, err := caPair.CreateAndSignPair(params.User, certs.CertTypeClient)
	if err != nil {
		return err
	}

	userSecret := userPair.GenerateServerSecret(params.Namespace, params.Name)
	err = cnp.Print(userSecret, format)
	if err != nil {
		return err
	}

	if dryRun {
		return nil
	}

	_, err = cnp.GoClient.CoreV1().Secrets(params.Namespace).Create(ctx, userSecret, metav1.CreateOptions{})
	if err == nil {
		fmt.Printf("secret/%v created\n", userSecret.Name)
	}
	return err
}
