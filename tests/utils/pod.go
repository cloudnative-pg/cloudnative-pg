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
	"fmt"
	"io"
	"time"

	"github.com/avast/retry-go/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	utils2 "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PodCreateAndWaitForReady creates a given pod object and wait for it to be ready
func PodCreateAndWaitForReady(env *TestingEnvironment, pod *corev1.Pod, timeoutSeconds uint) error {
	_, err := CreateObject(env, pod)
	if err != nil {
		return err
	}
	return PodWaitForReady(env, pod, timeoutSeconds)
}

// PodWaitForReady waits for a pod to be ready
func PodWaitForReady(env *TestingEnvironment, pod *corev1.Pod, timeoutSeconds uint) error {
	err := retry.Do(
		func() error {
			if err := env.Client.Get(env.Ctx, client.ObjectKey{
				Namespace: pod.Namespace,
				Name:      pod.Name,
			}, pod); err != nil {
				return err
			}
			if !utils2.IsPodReady(*pod) {
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

// PodHasLabels verifies that the labels of a pod contain a specified
// labels map
func PodHasLabels(pod corev1.Pod, labels map[string]string) bool {
	podLabels := pod.Labels
	for k, v := range labels {
		val, ok := podLabels[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// PodHasAnnotations verifies that the annotations of a pod contain a specified
// annotations map
func PodHasAnnotations(pod corev1.Pod, annotations map[string]string) bool {
	podAnnotations := pod.Annotations
	for k, v := range annotations {
		val, ok := podAnnotations[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
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

	return DeleteObject(&env, u, opts...)
}

// GetPodLogs gathers pod logs
func (env TestingEnvironment) GetPodLogs(namespace string, podName string) (string, error) {
	req := env.Interface.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	podLogs, err := req.Stream(env.Ctx)
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

// GetPodList gathers the current list of pods in a namespace
func (env TestingEnvironment) GetPodList(namespace string) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	err := GetObjectList(
		&env, podList, client.InNamespace(namespace),
	)
	return podList, err
}
