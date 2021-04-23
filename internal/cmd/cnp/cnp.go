/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package cnp contains the common behaviors of the kubectl-cnp subcommand
package cnp

import (
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	// Namespace to operate in
	Namespace string

	// Config is the Kubernetes configuration used
	Config *rest.Config

	// DynamicClient is the kubernetes dynamic client
	DynamicClient dynamic.Interface

	// GoClient is the static client
	GoClient kubernetes.Interface
)

// CreateKubernetesClient create a k8s client to be used inside the kubectl-cnp
// utility
func CreateKubernetesClient(configFlags *genericclioptions.ConfigFlags) error {
	var err error

	kubeconfig := configFlags.ToRawKubeConfigLoader()

	Config, err = kubeconfig.ClientConfig()
	if err != nil {
		return err
	}

	DynamicClient, err = dynamic.NewForConfig(Config)
	if err != nil {
		return err
	}

	GoClient, err = kubernetes.NewForConfig(Config)
	if err != nil {
		return err
	}

	Namespace, _, err = kubeconfig.Namespace()
	if err != nil {
		return err
	}

	return nil
}
