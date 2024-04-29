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
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	pkgutils "github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
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
			if !pkgutils.IsPodReady(*pod) {
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

// PodHasCondition verifies that a pod has a specified condition
func PodHasCondition(pod *corev1.Pod, conditionType corev1.PodConditionType, status corev1.ConditionStatus) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == conditionType && cond.Status == status {
			return true
		}
	}
	return false
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

// GetManagerVersion returns the current manager version of a given pod
func GetManagerVersion(namespace, podName string) (string, error) {
	out, _, err := RunUnchecked(fmt.Sprintf(
		"kubectl -n %v exec %v -c postgres -- /controller/manager version",
		namespace,
		podName,
	))
	if err != nil {
		return "", err
	}
	versionRegexp := regexp.MustCompile(`^Build: {Version:(\d+.*) Commit.*}$`)
	ver := versionRegexp.FindStringSubmatch(strings.TrimSpace(out))[1]
	return ver, nil
}

// GetPod gets a pod by namespace and name
func (env TestingEnvironment) GetPod(namespace, podName string) (*corev1.Pod, error) {
	wrapErr := func(err error) error {
		return fmt.Errorf("while getting pod '%s/%s': %w", namespace, podName, err)
	}
	podList, err := env.GetPodList(namespace)
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

// ContainerLocator contains the necessary data to find a container on a pod
type ContainerLocator struct {
	Namespace     string
	PodName       string
	ContainerName string
}

// ExecCommandInContainer executes commands in a given instance pod, in the
// postgres container
func (env TestingEnvironment) ExecCommandInContainer(
	container ContainerLocator,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	wrapErr := func(err error) error {
		return fmt.Errorf("while executing command in pod '%s/%s': %w",
			container.Namespace, container.PodName, err)
	}
	pod, err := env.GetPod(container.Namespace, container.PodName)
	if err != nil {
		return "", "", wrapErr(err)
	}
	if !pkgutils.IsPodReady(*pod) {
		return "", "", fmt.Errorf("pod not ready. Namespace: %v, Name: %v", pod.Namespace, pod.Name)
	}
	return env.ExecCommand(env.Ctx, *pod, container.ContainerName, timeout, command...)
}

// PodLocator contains the necessary data to find a pod
type PodLocator struct {
	Namespace string
	PodName   string
}

// ExecCommandInInstancePod executes commands in a given instance pod, in the
// postgres container
func (env TestingEnvironment) ExecCommandInInstancePod(
	podLocator PodLocator,
	timeout *time.Duration,
	command ...string,
) (string, string, error) {
	return env.ExecCommandInContainer(
		ContainerLocator{
			Namespace:     podLocator.Namespace,
			PodName:       podLocator.PodName,
			ContainerName: specs.PostgresContainerName,
		}, timeout, command...)
}

// DatabaseName is a special type for the database argument in an Exec call
type DatabaseName string

// ExecQueryInInstancePod executes a query in an instance pod, by connecting to the pod
// and the postgres container, and using a local connection with the postgres user
func (env TestingEnvironment) ExecQueryInInstancePod(
	podLocator PodLocator,
	dbname DatabaseName,
	query string,
) (string, string, error) {
	timeout := time.Second * 10
	return env.ExecCommandInInstancePod(
		PodLocator{
			Namespace: podLocator.Namespace,
			PodName:   podLocator.PodName,
		}, &timeout, "psql", "-U", "postgres", string(dbname), "-tAc", query)
}
