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

package psql

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

// psqlCommand is the launcher of `psql` with `kubectl exec`
type psqlCommand struct {
	psqlCommandOptions

	// The list of possible pods where to launch psql
	podList []corev1.Pod

	// The path of kubectl
	kubectlPath string
}

// psqlCommandOptions are the options required to start psql
type psqlCommandOptions struct {
	// Require a connection to a replica
	replica bool

	// The cluster name
	name string

	// The namespace where we're working in
	namespace string

	// Whether we should we allocate a TTY for psql
	allocateTTY bool

	// Whether we should we pass stdin to psql
	passStdin bool

	// Arguments to pass to psql
	args []string
}

// newPsqlCommand creates a new psql command
func newPsqlCommand(
	ctx context.Context,
	options psqlCommandOptions,
) (*psqlCommand, error) {
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

	return &psqlCommand{
		psqlCommandOptions: options,
		podList:            pods.Items,
		kubectlPath:        kubectlPath,
	}, nil
}

// getKubectlInvocation gets the kubectl command to be executed
func (psql *psqlCommand) getKubectlInvocation() ([]string, error) {
	result := make([]string, 0, 11+len(psql.args))
	result = append(result, "kubectl", "exec")

	if psql.allocateTTY {
		result = append(result, "-t")
	}
	if psql.passStdin {
		result = append(result, "-i")
	}
	if len(psql.namespace) > 0 {
		result = append(result, "-n", psql.namespace)
	}
	result = append(result, "-c", specs.PostgresContainerName)

	podName, err := psql.getPodName()
	if err != nil {
		return nil, err
	}

	result = append(result, podName)
	result = append(result, "--", "psql")
	result = append(result, psql.args...)
	return result, nil
}

// getPodName get the first Pod name with the required role
func (psql *psqlCommand) getPodName() (string, error) {
	targetPodRole := specs.ClusterRoleLabelPrimary
	if psql.replica {
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

// exec replaces the current process with a `kubectl exec` invocation.
// This function won't return
func (psql *psqlCommand) exec() error {
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

// ErrMissingPod is raised when we can't find a Pod having the desired role
type ErrMissingPod struct {
	role string
}

// Error implements the error interface
func (err *ErrMissingPod) Error() string {
	return fmt.Sprintf("cannot find Pod with role \"%s\"", err.role)
}
