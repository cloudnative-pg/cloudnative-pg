/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

// Package pods provides pod utilities to manage pods inside K8s
package pods

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/avast/retry-go/v5"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

// List gathers the current list of pods in a namespace
func List(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := objects.List(
		ctx, crudClient, podList, client.InNamespace(namespace),
	)
	return podList, err
}

// Delete deletes a pod if existent
func Delete(
	ctx context.Context,
	crudClient client.Client,
	namespace, name string,
	opts ...client.DeleteOption,
) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	})

	return objects.Delete(ctx, crudClient, u, opts...)
}

// CreateAndWaitForReady creates a given pod object and wait for it to be ready
func CreateAndWaitForReady(
	ctx context.Context,
	crudClient client.Client,
	pod *corev1.Pod,
	timeoutSeconds uint,
) error {
	_, err := objects.Create(ctx, crudClient, pod)
	if err != nil {
		return err
	}
	return waitForReady(ctx, crudClient, pod, timeoutSeconds)
}

// waitForReady waits for a pod to be ready
func waitForReady(
	ctx context.Context,
	crudClient client.Client,
	pod *corev1.Pod,
	timeoutSeconds uint,
) error {
	err := retry.New(
		retry.Attempts(timeoutSeconds),
		retry.Delay(time.Second),
		retry.DelayType(retry.FixedDelay)).
		Do(
			func() error {
				if err := crudClient.Get(ctx, client.ObjectKey{
					Namespace: pod.Namespace,
					Name:      pod.Name,
				}, pod); err != nil {
					return err
				}
				if !utils.IsPodReady(*pod) {
					return fmt.Errorf("pod not ready. Namespace: %v, Name: %v", pod.Namespace, pod.Name)
				}
				return nil
			},
		)
	return err
}

// Logs gathers pod logs
func Logs(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	namespace, podName string,
) (string, error) {
	req := kubeInterface.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	podLogs, err := req.Stream(ctx)
	if err != nil {
		return "", err
	}
	defer func() {
		innerErr := podLogs.Close()
		if err == nil && innerErr != nil {
			err = innerErr
		}
	}()

	// Create a buffer to hold JSON data
	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// Get gets a pod by namespace and name
func Get(
	ctx context.Context,
	crudClient client.Client,
	namespace, podName string,
) (*corev1.Pod, error) {
	wrapErr := func(err error) error {
		return fmt.Errorf("while getting pod '%s/%s': %w", namespace, podName, err)
	}
	podList, err := List(ctx, crudClient, namespace)
	if err != nil {
		return nil, wrapErr(err)
	}
	for _, pod := range podList.Items {
		if podName == pod.Name {
			return &pod, nil
		}
	}
	return nil, wrapErr(errors.New("pod not found"))
}

// HasLabels verifies that the labels of a pod contain a specified
// labels map
func HasLabels(pod corev1.Pod, labels map[string]string) bool {
	podLabels := pod.Labels
	for k, v := range labels {
		val, ok := podLabels[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// HasAnnotations verifies that the annotations of a pod contain a specified
// annotations map
func HasAnnotations(pod corev1.Pod, annotations map[string]string) bool {
	podAnnotations := pod.Annotations
	for k, v := range annotations {
		val, ok := podAnnotations[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// HasCondition verifies that a pod has a specified condition
func HasCondition(pod *corev1.Pod, conditionType corev1.PodConditionType, status corev1.ConditionStatus) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == conditionType && cond.Status == status {
			return true
		}
	}
	return false
}
