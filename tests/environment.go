/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package tests

import (
	"context"
	"os"
	"strings"

	"github.com/onsi/ginkgo"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestingEnvironment struct for operator testing
type TestingEnvironment struct {
	RestClientConfig   *rest.Config
	Client             client.Client
	Ctx                context.Context
	Scheme             *runtime.Scheme
	PreserveNamespaces []string
}

// NewTestingEnvironment creates the environment for testing
func NewTestingEnvironment() *TestingEnvironment {
	var env TestingEnvironment
	env.RestClientConfig = controllerruntime.GetConfigOrDie()
	env.Ctx = context.Background()
	env.Scheme = runtime.NewScheme()

	var err error
	env.Client, err = client.New(env.RestClientConfig, client.Options{Scheme: env.Scheme})
	if err != nil {
		ginkgo.Fail(err.Error())
	}

	if preserveNamespaces := os.Getenv("PRESERVE_NAMESPACES"); preserveNamespaces != "" {
		env.PreserveNamespaces = strings.Fields(preserveNamespaces)
	}

	return &env
}

// CreateNamespace creates a namespace
func (env TestingEnvironment) CreateNamespace(name string, opts ...client.CreateOption) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	return env.Client.Create(env.Ctx, u, opts...)
}

// DeleteNamespace deletes a namespace if existent
func (env TestingEnvironment) DeleteNamespace(name string, opts ...client.DeleteOption) error {
	// Exit immediately if if the namespace is listed in PreserveNamespaces
	for _, v := range env.PreserveNamespaces {
		if v == name {
			return nil
		}
	}

	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})

	return env.Client.Delete(env.Ctx, u, opts...)
}

// DeletePod deletes a pod if existent
func (env TestingEnvironment) DeletePod(namespace string, name string, opts ...client.DeleteOption) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	})

	return env.Client.Delete(env.Ctx, u, opts...)
}
