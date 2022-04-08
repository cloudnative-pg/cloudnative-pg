/*
Copyright 2019-2022 The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateNamespace creates a namespace
func (env TestingEnvironment) CreateNamespace(name string, opts ...client.CreateOption) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})
	return CreateObject(&env, u, opts...)
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

	return DeleteObject(&env, u, opts...)
}

// DeleteNamespaceAndWait deletes a namespace if existent and returns when deletion is completed
func (env TestingEnvironment) DeleteNamespaceAndWait(name string, timeoutSeconds int) error {
	// Exit immediately if the namespace is listed in PreserveNamespaces
	for _, v := range env.PreserveNamespaces {
		if v == name {
			return nil
		}
	}

	_, _, err := Run(fmt.Sprintf("kubectl delete namespace %v --wait=true --timeout %vs", name, timeoutSeconds))

	return err
}
