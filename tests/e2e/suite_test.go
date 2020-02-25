/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package e2e

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	//+kubebuilder:scaffold:imports
	clusterv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

const (
	samplesDir = "../../docs/src/samples"
)

const (
	operatorNamespace = "postgresql-operator-system"
)

var restClientConfig = ctrl.GetConfigOrDie()
var client ctrlclient.Client
var ctx = context.Background()
var scheme = runtime.NewScheme()

var _ = BeforeSuite(func() {
	_ = k8sscheme.AddToScheme(scheme)
	_ = clusterv1alpha1.AddToScheme(scheme)
	//+kubebuilder:scaffold:scheme

	var err error
	client, err = ctrlclient.New(restClientConfig, ctrlclient.Options{Scheme: scheme})
	if err != nil {
		Fail(err.Error())
	}
})

// createNamespace creates a namespace
func createNamespace(ctx context.Context, name string) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	return client.Create(ctx, u)
}

// deleteNamespace deletes a namespace if existent
func deleteNamespace(ctx context.Context, name string) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	return client.Delete(ctx, u)
}

func TestE2ESuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cloud Native PostgreSQL Operator E2E")
}
