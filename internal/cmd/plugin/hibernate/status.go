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

package hibernate

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
)

// statusOutput is a supported type of stdout for the status command
type statusOutput string

const (
	jsonStatusOutput statusOutput = "json"
	textStatusOutput statusOutput = "text"
)

// statusLevel describes if the output should communicate an ok,warning or error status
type statusLevel string

const (
	okLevel      statusLevel = "ok"
	warningLevel statusLevel = "warning"
	errorLevel   statusLevel = "error"
)

type statusCommand struct {
	outputManager statusOutputManager
	ctx           context.Context
	clusterName   string
}

func newStatusCommandJSONOutput(ctx context.Context, clusterName string, jsonFilePath string) *statusCommand {
	return &statusCommand{
		outputManager: newJSONOutputManager(ctx, jsonFilePath),
		ctx:           ctx,
		clusterName:   clusterName,
	}
}

func newStatusCommandTextOutput(ctx context.Context, clusterName string) *statusCommand {
	return &statusCommand{
		outputManager: newTextStatusOutputManager(),
		ctx:           ctx,
		clusterName:   clusterName,
	}
}

func (cmd *statusCommand) execute() error {
	isDeployed, err := cmd.isClusterDeployed()
	if err != nil {
		return err
	}
	if isDeployed {
		return cmd.clusterIsAlreadyRunningOutput()
	}

	pvcs, err := getHibernatedPVCGroup(cmd.ctx, cmd.clusterName)
	if errors.Is(err, errNoHibernatedPVCsFound) {
		return cmd.noHibernatedPVCsOutput()
	}
	if err != nil {
		return err
	}

	return cmd.clusterHibernatedOutput(pvcs)
}

func (cmd *statusCommand) clusterHibernatedOutput(pvcs []corev1.PersistentVolumeClaim) error {
	clusterFromPVC, err := getClusterFromPVCAnnotation(pvcs[0])
	if err != nil {
		return err
	}

	cmd.outputManager.addHibernationSummaryInformation(okLevel, "Cluster Hibernated", cmd.clusterName)
	cmd.outputManager.addClusterManifestInformation(&clusterFromPVC)
	cmd.outputManager.addPVCGroupInformation(pvcs)

	return cmd.outputManager.execute()
}

func (cmd *statusCommand) clusterIsAlreadyRunningOutput() error {
	cmd.outputManager.addHibernationSummaryInformation(warningLevel, "No Hibernation. Cluster Deployed.", cmd.clusterName)
	return cmd.outputManager.execute()
}

func (cmd *statusCommand) noHibernatedPVCsOutput() error {
	cmd.outputManager.addHibernationSummaryInformation(errorLevel, "No hibernated PVCs found", cmd.clusterName)
	return cmd.outputManager.execute()
}

func (cmd *statusCommand) isClusterDeployed() (bool, error) {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := plugin.Client.Get(cmd.ctx, client.ObjectKey{Namespace: plugin.Namespace, Name: cmd.clusterName}, &cluster)
	if apierrs.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("error while fetching the cluster resource: %w", err)
	}

	return true, nil
}
