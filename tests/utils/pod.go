/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"
	"time"

	"github.com/avast/retry-go"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	utils2 "github.com/EnterpriseDB/cloud-native-postgresql/pkg/utils"
)

// PodCreateAndWaitForReady creates a given pod object and wait for it to be ready
func PodCreateAndWaitForReady(env *TestingEnvironment, pod *v1.Pod, timeoutSeconds uint) error {
	err := env.Client.Create(env.Ctx, pod)
	if err != nil {
		return err
	}
	return PodWaitForReady(env, pod, timeoutSeconds)
}

// PodWaitForReady waits for a pod to be ready
func PodWaitForReady(env *TestingEnvironment, pod *v1.Pod, timeoutSeconds uint) error {
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

// GetPodsWithLabels returns a PodList of all the pods with the requested labels
// in a certain namespace
func GetPodsWithLabels(env *TestingEnvironment, namespace string, labels map[string]string) (*v1.PodList, error) {
	podList := &v1.PodList{}
	err := env.Client.List(
		env.Ctx, podList, client.InNamespace(namespace),
		client.MatchingLabels(labels),
	)
	return podList, err
}

// PodHasLabels verifies that the labels of a pod contain a specified
// labels map
func PodHasLabels(pod v1.Pod, labels map[string]string) bool {
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
func PodHasAnnotations(pod v1.Pod, annotations map[string]string) bool {
	podAnnotations := pod.Annotations
	for k, v := range annotations {
		val, ok := podAnnotations[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}
