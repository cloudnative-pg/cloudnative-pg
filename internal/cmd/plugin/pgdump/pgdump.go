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

package pgdump

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	// kubectlCommand is used to execute a command inside a Pod
	kubectlCommand = "kubectl"
)

// pgdumpCommand is the launcher of `pgdump` with `kubectl exec`
type pgdumpCommand struct {
	pgdumpCommandOptions

	// The list of possible pods where to launch pgdump
	podList []corev1.Pod

	// The path of kubectl
	kubectlPath string
}

// pgdumpCommandOptions are the options required to start pgdump
type pgdumpCommandOptions struct {
	// Require a connection to a replica
	replica bool

	// The cluster name
	name string

	// The namespace where we're working in
	namespace string

	// Whether we should we pass stdin to pgdump
	passStdin bool

	// Arguments to pass to pgdump
	args []string
}

// newPgdumpCommand creates a new pgdump command
func newPgdumpCommand(
	ctx context.Context,
	options pgdumpCommandOptions,
) (*pgdumpCommand, error) {
	var pods corev1.PodList
	if err := plugin.Client.List(
		ctx,
		&pods,
		client.MatchingLabels{utils.ClusterLabelName: options.name},
		client.InNamespace(plugin.Namespace),
	); err != nil {
		return nil, err
	}

	kubectlPath, err := exec.LookPath(kubectlCommand)
	if err != nil {
		return nil, fmt.Errorf("while getting kubectl path: %w", err)
	}

	return &pgdumpCommand{
		pgdumpCommandOptions: options,
		podList:            pods.Items,
		kubectlPath:        kubectlPath,
	}, nil
}

// getKubectlInvocation gets the kubectl command to be executed
func (pgdump *pgdumpCommand) getKubectlInvocation() ([]string, error) {
	result := make([]string, 0, 11+len(pgdump.args))
	result = append(result, "kubectl", "exec")

	if pgdump.passStdin {
		result = append(result, "-i")
	}
	if len(pgdump.namespace) > 0 {
		result = append(result, "-n", pgdump.namespace)
	}
	result = append(result, "-c", specs.PostgresContainerName)

	podName, err := pgdump.getPodName()
	if err != nil {
		return nil, err
	}

	result = append(result, podName)
	result = append(result, "--", "pgdump")
	result = append(result, pgdump.args...)
	return result, nil
}

// getPodName get the first Pod name with the required role
func (pgdump *pgdumpCommand) getPodName() (string, error) {
	targetPodRole := specs.ClusterRoleLabelPrimary
	if pgdump.replica {
		targetPodRole = specs.ClusterRoleLabelReplica
	}

	for i := range pgdump.podList {
		podRole, _ := utils.GetInstanceRole(pgdump.podList[i].Labels)
		if podRole == targetPodRole {
			return pgdump.podList[i].Name, nil
		}
	}

	return "", &ErrMissingPod{role: targetPodRole}
}

// exec replaces the current process with a `kubectl exec` invocation.
// This function won't return
func (pgdump *pgdumpCommand) exec() error {
	kubectlExec, err := pgdump.getKubectlInvocation()
	if err != nil {
		return err
	}

	err = syscall.Exec(pgdump.kubectlPath, kubectlExec, os.Environ()) // #nosec
	if err != nil {
		return err
	}

	return nil
}

// ErrMissingPod is raised when we can't find a Pod having the desired role
type ErrMissingPod struct {
	role string
}

// Error implements the error interface
func (err *ErrMissingPod) Error() string {
	return fmt.Sprintf("cannot find Pod with role \"%s\"", err.role)
}
