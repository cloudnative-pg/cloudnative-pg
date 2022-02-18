/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"

	"github.com/google/shlex"
	"github.com/onsi/ginkgo/v2"
	corev1 "k8s.io/api/core/v1"
)

// PodCommandResult contains the pod a command has been run and the output of the command
type PodCommandResult struct {
	Pod    corev1.Pod
	Output string
}

// RunUnchecked executes a command and process the information
func RunUnchecked(command string) (stdout string, stderr string, err error) {
	tokens, err := shlex.Split(command)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("Error parsing command `%v`: %v\n", command, err)
		return "", "", err
	}

	var outBuffer, errBuffer bytes.Buffer
	cmd := exec.Command(tokens[0], tokens[1:]...) // #nosec G204
	cmd.Stdout, cmd.Stderr = &outBuffer, &errBuffer
	err = cmd.Run()
	stdout = outBuffer.String()
	stderr = errBuffer.String()
	return
}

// Run executes a command and prints the output when terminates with an error
func Run(command string) (stdout string, stderr string, err error) {
	stdout, stderr, err = RunUnchecked(command)

	var exerr *exec.ExitError
	if errors.As(err, &exerr) {
		ginkgo.GinkgoWriter.Printf("RunCheck: %v\nExitCode: %v\n Out:\n%v\nErr:\n%v\n",
			command, exerr.ExitCode(), stdout, stderr)
	}
	return
}

// RunOnPodList executes a command on a list of pod and returns the outputs
func RunOnPodList(namespace, command string, podList *corev1.PodList) ([]PodCommandResult, error) {
	podCommandResults := make([]PodCommandResult, 0, len(podList.Items))
	for _, pod := range podList.Items {
		podName := pod.GetName()

		out, _, err := Run(fmt.Sprintf(
			"kubectl exec -n %v %v -- %v",
			namespace,
			podName,
			command))
		if err != nil {
			return nil, err
		}

		podCommandResults = append(podCommandResults, PodCommandResult{
			Pod:    pod,
			Output: out,
		})
	}

	return podCommandResults, nil
}
