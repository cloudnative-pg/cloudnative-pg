/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package management contains all the features needed by the instance
// manager that runs in each Pod as PID 1
package management

import (
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1alpha1 "gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
)

var (
	// Scheme used for the instance manager
	Scheme = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(Scheme)
	_ = apiv1alpha1.AddToScheme(Scheme)
}

// NewClient create a new typed K8s client where
// the PostgreSQL CRD has already been registered
func NewClient() (client.Client, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	typedClient, err := client.New(config, client.Options{
		Scheme: Scheme,
		Mapper: nil,
	})
	if err != nil {
		return nil, err
	}

	return typedClient, nil
}
