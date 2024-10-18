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

// Package pods provides pod utilities to manage pods inside K8s
package pods

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/avast/retry-go/v4"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

// GetPodList gathers the current list of pods in a namespace
func GetPodList(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*v1.PodList, error) {
	podList := &v1.PodList{}
	err := objects.GetObjectList(
		ctx, crudClient, podList, client.InNamespace(namespace),
	)
	return podList, err
}

// DeletePod deletes a pod if existent
func DeletePod(
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

	return objects.DeleteObject(ctx, crudClient, u, opts...)
}

// CreateAndWaitForReady creates a given pod object and wait for it to be ready
func CreateAndWaitForReady(
	ctx context.Context,
	crudClient client.Client,
	pod *v1.Pod,
	timeoutSeconds uint,
) error {
	_, err := objects.CreateObject(ctx, crudClient, pod)
	if err != nil {
		return err
	}
	return podWaitForReady(ctx, crudClient, pod, timeoutSeconds)
}

// podWaitForReady waits for a pod to be ready
func podWaitForReady(
	ctx context.Context,
	crudClient client.Client,
	pod *v1.Pod,
	timeoutSeconds uint,
) error {
	err := retry.Do(
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
		retry.Attempts(timeoutSeconds),
		retry.Delay(time.Second),
		retry.DelayType(retry.FixedDelay),
	)
	return err
}

// GetPodLogs gathers pod logs
func GetPodLogs(
	ctx context.Context,
	kubeInterface kubernetes.Interface,
	namespace, podName string,
) (string, error) {
	req := kubeInterface.CoreV1().Pods(namespace).GetLogs(podName, &v1.PodLogOptions{})
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

// GetPod gets a pod by namespace and name
func GetPod(
	ctx context.Context,
	crudClient client.Client,
	namespace, podName string,
) (*v1.Pod, error) {
	wrapErr := func(err error) error {
		return fmt.Errorf("while getting pod '%s/%s': %w", namespace, podName, err)
	}
	podList, err := GetPodList(ctx, crudClient, namespace)
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
