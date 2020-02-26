/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/
package main

import (
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	postgresqlv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	// Populate client scheme
	_ = postgresqlv1alpha1.AddToScheme(scheme)
}

// Create a kubernetes client allowing reading the cluster status
func createKubernetesClient() (client.Client, error) {
	apiClient, err := client.New(ctrl.GetConfigOrDie(), client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	return apiClient, nil
}
