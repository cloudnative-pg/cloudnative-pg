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

package psql

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// kubectlCommand is used to execute a command inside a Pod
	kubectlCommand = "kubectl"
)

// Command is the launcher of `psql` with `kubectl exec`
type Command struct {
	CommandOptions

	// The list of possible pods where to launch psql
	podList []corev1.Pod

	// The path of kubectl
	kubectlPath string
}

// CommandOptions are the options required to start psql
type CommandOptions struct {
	// Require a connection to a Replica
	Replica bool

	// The cluster Name
	Name string

	// The Namespace where we're working in
	Namespace string

	// The Context to execute the command
	Context string

	// Whether we should we allocate a TTY for psql
	AllocateTTY bool

	// Whether we should we pass stdin to psql
	PassStdin bool

	// Arguments to pass to psql
	Args []string
}

// NewCommand creates a new psql command
func NewCommand(
	ctx context.Context,
	options CommandOptions,
) (*Command, error) {
	var pods corev1.PodList
	if err := plugin.Client.List(
		ctx,
		&pods,
		client.MatchingLabels{utils.ClusterLabelName: options.Name},
		client.InNamespace(plugin.Namespace),
	); err != nil {
		return nil, err
	}

	// Check if the pod list is empty
	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("cluster does not exist or is not accessible")
	}

	kubectlPath, err := exec.LookPath(kubectlCommand)
	if err != nil {
		return nil, fmt.Errorf("while getting kubectl path: %w", err)
	}

	return &Command{
		CommandOptions: options,
		podList:        pods.Items,
		kubectlPath:    kubectlPath,
	}, nil
}

// getKubectlInvocation gets the kubectl command to be executed
func (psql *Command) getKubectlInvocation() ([]string, error) {
	result := make([]string, 0, 13+len(psql.Args))
	result = append(result, "kubectl", "exec")

	if psql.Context != "" {
		result = append(result, "--context", psql.Context)
	}

	if psql.AllocateTTY {
		result = append(result, "-t")
	}
	if psql.PassStdin {
		result = append(result, "-i")
	}
	if len(psql.Namespace) > 0 {
		result = append(result, "-n", psql.Namespace)
	}
	result = append(result, "-c", specs.PostgresContainerName)

	podName, err := psql.getPodName()
	if err != nil {
		return nil, err
	}

	// Default to `postgres` if no-user has been specified
	if !slices.Contains(psql.Args, "-U") {
		psql.Args = append([]string{"-U", "postgres"}, psql.Args...)
	}

	result = append(result, podName)
	result = append(result, "--", "psql")
	result = append(result, psql.Args...)
	return result, nil
}

// getPodName get the first Pod name with the required role
func (psql *Command) getPodName() (string, error) {
	targetPodRole := specs.ClusterRoleLabelPrimary
	if psql.Replica {
		targetPodRole = specs.ClusterRoleLabelReplica
	}

	for i := range psql.podList {
		podRole, _ := utils.GetInstanceRole(psql.podList[i].Labels)
		if podRole == targetPodRole {
			return psql.podList[i].Name, nil
		}
	}

	return "", &ErrMissingPod{role: targetPodRole}
}

// Exec replaces the current process with a `kubectl Exec` invocation.
// This function won't return
func (psql *Command) Exec() error {
	kubectlExec, err := psql.getKubectlInvocation()
	if err != nil {
		return err
	}

	err = syscall.Exec(psql.kubectlPath, kubectlExec, os.Environ()) // #nosec
	if err != nil {
		return err
	}

	return nil
}

// Run starts a psql process inside the target pod
func (psql *Command) Run() error {
	kubectlExec, err := psql.getKubectlInvocation()
	if err != nil {
		return err
	}

	cmd := exec.Command(psql.kubectlPath, kubectlExec[1:]...) // nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Output starts a psql process inside the target pod
// and returns its stdout
func (psql *Command) Output() ([]byte, error) {
	kubectlExec, err := psql.getKubectlInvocation()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(psql.kubectlPath, kubectlExec[1:]...) // nolint:gosec
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

// ErrMissingPod is raised when we can't find a Pod having the desired role
type ErrMissingPod struct {
	role string
}

// Error implements the error interface
func (err *ErrMissingPod) Error() string {
	return fmt.Sprintf("cannot find Pod with role \"%s\"", err.role)
}
