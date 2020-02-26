/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package utils

// Look here: https://github.com/kubernetes/kubernetes/blob/release-1.17/test/e2e/framework/exec_util.go
// also here: https://github.com/kubernetes-client/python/blob/master/examples/pod_exec.py

import (
	"bytes"
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ExecCommand executes arbitrary command inside the pod, and returns his result
func ExecCommand(
	ctx context.Context,
	pod corev1.Pod,
	containerName string,
	timeout *time.Duration,
	command ...string) (string, string, error) {
	config := ctrl.GetConfigOrDie()

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", "", err
	}

	// iterate through all containers looking for the one running PostgreSQL.
	targetContainer := -1
	for i, cr := range pod.Spec.Containers {
		if cr.Name == containerName {
			targetContainer = i
			break
		}
	}

	if targetContainer < 0 {
		return "", "", fmt.Errorf("could not find %s container to exec to", containerName)
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec").
		Context(ctx)

	if timeout != nil {
		req.Timeout(*timeout)
	}

	req.VersionedParams(&corev1.PodExecOptions{
		Container: pod.Spec.Containers[targetContainer].Name,
		Command:   command,
		Stdout:    true,
		Stderr:    true,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return "", "", err
	}

	var stdout, stderr bytes.Buffer
	err = executor.Stream(remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})
	if err != nil {
		return "", "", fmt.Errorf("%v - %v", err, stderr.String())
	}

	return stdout.String(), stderr.String(), nil
}
