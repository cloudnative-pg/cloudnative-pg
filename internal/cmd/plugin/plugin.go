/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package plugin contains the common behaviors of the kubectl-cnp subcommand
package plugin

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

var (
	// Namespace to operate in
	Namespace string

	// Config is the Kubernetes configuration used
	Config *rest.Config

	// Client is the controller-runtime client
	Client client.Client
)

// CreateKubernetesClient creates a k8s client to be used inside the kubectl-cnp
// utility
func CreateKubernetesClient(configFlags *genericclioptions.ConfigFlags) error {
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

	Namespace, _, err = kubeconfig.Namespace()
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

	Client, err = client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return err
	}
	return nil
}
