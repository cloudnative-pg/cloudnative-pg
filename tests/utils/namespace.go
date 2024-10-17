/*
Copyright The CloudNativePG Contributors

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
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
)

// GetOperatorLogs collects the operator logs
func (env TestingEnvironment) GetOperatorLogs(buf *bytes.Buffer) error {
	operatorPod, err := env.GetOperatorPod()
	if err != nil {
		return err
	}

	streamPodLog := logs.StreamingRequest{
		Pod: &operatorPod,
		Options: &corev1.PodLogOptions{
			Timestamps: false,
			Follow:     false,
		},
		Client: env.Interface,
	}
	return streamPodLog.Stream(env.Ctx, buf)
}

// CleanupNamespace does cleanup duty related to the tear-down of a namespace,
// and is intended to be called in a DeferCleanup clause
func (env TestingEnvironment) CleanupNamespace(
	namespace string,
	testName string,
	testFailed bool,
) error {
	if testFailed {
		env.DumpNamespaceObjects(namespace, "out/"+testName+".log")
	}

	if len(namespace) == 0 {
		return fmt.Errorf("namespace is empty")
	}
	exists, _ := fileutils.FileExists(path.Join(env.SternLogDir, namespace))
	if exists && !testFailed {
		err := fileutils.RemoveDirectory(path.Join(env.SternLogDir, namespace))
		if err != nil {
			return err
		}
	}

	return env.DeleteNamespace(namespace)
}

// CreateUniqueTestNamespace creates a namespace by using the passed prefix.
// Return the namespace name and any errors encountered.
// The namespace is automatically cleaned up at the end of the test.
func (env TestingEnvironment) CreateUniqueTestNamespace(
	namespacePrefix string,
	opts ...client.CreateOption,
) (string, error) {
	name := env.createdNamespaces.generateUniqueName(namespacePrefix)

	return name, env.CreateTestNamespace(name, opts...)
}

// CreateTestNamespace creates a namespace creates a namespace.
// Prefer CreateUniqueTestNamespace instead, unless you need a
// specific namespace name. If so, make sure there is no collision
// potential.
// The namespace is automatically cleaned up at the end of the test.
func (env TestingEnvironment) CreateTestNamespace(
	name string,
	opts ...client.CreateOption,
) error {
	err := env.CreateNamespace(name, opts...)
	if err != nil {
		return err
	}

	ginkgo.DeferCleanup(func() error {
		return env.CleanupNamespace(
			name,
			ginkgo.CurrentSpecReport().LeafNodeText,
			ginkgo.CurrentSpecReport().Failed(),
		)
	})

	return nil
}

// CreateNamespace creates a namespace.
func (env TestingEnvironment) CreateNamespace(name string, opts ...client.CreateOption) error {
	// Exit immediately if the name is empty
	if name == "" {
		return errors.New("cannot create namespace with empty name")
	}

	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Namespace",
	})
	_, err := CreateObject(&env, u, opts...)
	return err
}

// EnsureNamespace checks for the presence of a namespace, and if it does not
// exist, creates it
func (env TestingEnvironment) EnsureNamespace(namespace string) error {
	var nsList corev1.NamespaceList
	err := GetObjectList(&env, &nsList)
	if err != nil {
		return err
	}
	for _, ns := range nsList.Items {
		if ns.Name == namespace {
			return nil
		}
	}
	return env.CreateNamespace(namespace)
}

// DeleteNamespace deletes a namespace if existent
func (env TestingEnvironment) DeleteNamespace(name string, opts ...client.DeleteOption) error {
	// Exit immediately if the name is empty
	if name == "" {
		return errors.New("cannot delete namespace with empty name")
	}

	// Exit immediately if the namespace is listed in PreserveNamespaces
	for _, v := range env.PreserveNamespaces {
		if strings.HasPrefix(name, v) {
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
		if strings.HasPrefix(name, v) {
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(env.Ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	err := env.DeleteNamespace(name, client.PropagationPolicy("Background"))
	if err != nil {
		return err
	}

	pods, err := env.GetPodList(name)
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		err = env.DeletePod(name, pod.Name, client.GracePeriodSeconds(1), client.PropagationPolicy("Background"))
		if err != nil && !apierrs.IsNotFound(err) {
			return err
		}
	}

	return wait.PollUntilContextCancel(ctx, time.Second, true,
		func(ctx context.Context) (bool, error) {
			err := env.Client.Get(ctx, client.ObjectKey{Name: name}, &corev1.Namespace{})
			if err != nil && apierrs.IsNotFound(err) {
				return true, nil
			}
			return false, err
		},
	)
}
