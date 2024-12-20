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

// Package exec provides functions to execute commands inside pods or from local
package exec

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	pkgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"

	. "github.com/onsi/gomega" // nolint
)

// ContainerLocator contains the necessary data to find a container on a pod
type ContainerLocator struct {
	Namespace     string
	PodName       string
	ContainerName string
}

// CommandInContainer executes commands in a given instance pod, in the
// postgres container
func CommandInContainer(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	container ContainerLocator,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	wrapErr := func(err error) error {
		return fmt.Errorf("while executing command in pod '%s/%s': %w",
			container.Namespace, container.PodName, err)
	}
	pod, err := pods.GetPod(ctx, crudClient, container.Namespace, container.PodName)
	if err != nil {
		return "", "", wrapErr(err)
	}
	if !pkgutils.IsPodReady(*pod) {
		return "", "", fmt.Errorf("pod not ready. Namespace: %v, Name: %v", pod.Namespace, pod.Name)
	}
	return Command(ctx, kubeInterface, restConfig, *pod, container.ContainerName, timeout, command...)
}

// PodLocator contains the necessary data to find a pod
type PodLocator struct {
	Namespace string
	PodName   string
}

// CommandInInstancePod executes commands in a given instance pod, in the
// postgres container
func CommandInInstancePod(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	podLocator PodLocator,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	return CommandInContainer(
		ctx, crudClient, kubeInterface, restConfig,
		ContainerLocator{
			Namespace:     podLocator.Namespace,
			PodName:       podLocator.PodName,
			ContainerName: specs.PostgresContainerName,
		}, timeout, command...)
}

// DatabaseName is a special type for the database argument in an Exec call
type DatabaseName string

// QueryInInstancePod executes a query in an instance pod, by connecting to the pod
// and the postgres container, and using a local connection with the postgres user
func QueryInInstancePod(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	podLocator PodLocator,
	dbname DatabaseName,
	query string,
) (string, string, error) {
	timeout := time.Second * 10
	return CommandInInstancePod(
		ctx, crudClient, kubeInterface, restConfig,
		PodLocator{
			Namespace: podLocator.Namespace,
			PodName:   podLocator.PodName,
		}, &timeout, "psql", "-U", "postgres", string(dbname), "-tAc", query)
}

// EventuallyExecQueryInInstancePod wraps QueryInInstancePod with an Eventually clause
func EventuallyExecQueryInInstancePod(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	podLocator PodLocator,
	dbname DatabaseName,
	query string,
	retryTimeout int,
	pollingTime int,
) (string, string, error) {
	var stdOut, stdErr string
	var err error

	Eventually(func() error {
		stdOut, stdErr, err = QueryInInstancePod(
			ctx, crudClient, kubeInterface, restConfig,
			PodLocator{
				Namespace: podLocator.Namespace,
				PodName:   podLocator.PodName,
			}, dbname, query)
		if err != nil {
			return err
		}
		return nil
	}, retryTimeout, pollingTime).Should(BeNil())

	return stdOut, stdErr, err
}

// Command wraps the utils.ExecCommand pre-setting values constant during
// tests
func Command(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	pod v1.Pod,
	containerName string,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	return pkgutils.ExecCommand(ctx, kubeInterface, restConfig,
		pod, containerName, timeout, command...)
}
