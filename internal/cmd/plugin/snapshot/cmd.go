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

package snapshot

import (
	"context"
	"fmt"
	"time"

	storagesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/fence"
	"github.com/cloudnative-pg/cloudnative-pg/internal/plugin/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	pkgres "github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

var snapshotBackoff = wait.Backoff{
	Steps:    4,
	Duration: 10 * time.Second,
	Factor:   5.0,
	Jitter:   0.1,
}

// NewCmd implements the `snapshot` subcommand
func NewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot <cluster-name>",
		Short: "Take a snapshot of a CloudNativePG cluster",
		Long: `This command will take a snapshot of an existing CNPG cluster as a set of VolumeSnapshot
resources. The created resources can be used later to create a new CloudNativePG cluster
containing the snapshotted data.

The command will:

1. Select a PostgreSQL replica Pod and fence it
2. Take a snapshot of the PVCs
3. Unfence the replica Pod

Fencing the Pod will result in a temporary out-of-service of the selected replica.
The other replicas will continue working.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clusterName := args[0]

			snapshotClassName, _ := cmd.Flags().GetString("volume-snapshot-class-name")

			snapshotCmd, err := newSnapshotCommand(cmd.Context(), clusterName, snapshotClassName)
			if err != nil {
				return err
			}

			return snapshotCmd.execute()
		},
	}

	cmd.Flags().StringP(
		"volume-snapshot-class-name",
		"c",
		"",
		`The VolumeSnapshotClass name to be used for the snapshot
(defaults to empty, which will make use of the default VolumeSnapshotClass)`)

	return cmd
}

type snapshotCommand struct {
	ctx               context.Context
	cluster           *apiv1.Cluster
	targetPod         *corev1.Pod
	pvcs              []corev1.PersistentVolumeClaim
	snapshotClassName string
	snapshotTime      time.Time
}

// newSnapshotCommand creates the snapshot command
func newSnapshotCommand(ctx context.Context, clusterName, snapshotClassName string) (*snapshotCommand, error) {
	var cluster apiv1.Cluster

	cmd := &snapshotCommand{
		ctx:               ctx,
		cluster:           &cluster,
		snapshotClassName: snapshotClassName,
	}

	// Get the Cluster object
	err := plugin.Client.Get(
		ctx,
		client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName},
		&cluster)
	if err != nil {
		return nil, fmt.Errorf("could not get cluster: %v", err)
	}

	// Get the target Pod
	managedInstances, primaryInstance, err := resources.GetInstancePods(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("could not get cluster pods: %w", err)
	}
	if primaryInstance.Name == "" {
		return nil, fmt.Errorf("no primary instance found, cannot proceed")
	}

	// Get the replica Pod to be fenced
	for i := len(managedInstances) - 1; i >= 0; i-- {
		if managedInstances[i].Name != primaryInstance.Name {
			cmd.targetPod = managedInstances[i].DeepCopy()
			break
		}
	}

	if cmd.targetPod == nil {
		return nil, fmt.Errorf("no replicas found, cannot proceed")
	}

	// Get the PVCs that will be snapshotted
	cmd.pvcs, err = resources.GetInstancePVCs(ctx, clusterName, cmd.targetPod.Name)
	if err != nil {
		return nil, fmt.Errorf("cannot get PVCs: %w", err)
	}

	return cmd, nil
}

// execute executes the snapshot command
func (cmd *snapshotCommand) execute() error {
	if err := cmd.checkPreconditionsStep(); err != nil {
		return err
	}

	if err := cmd.fencePodStep(); err != nil {
		return err
	}
	defer cmd.rollbackFencePod()

	if err := cmd.waitPodToBeFencedStep(); err != nil {
		return err
	}

	if err := cmd.snapshotPVCGroupStep(); err != nil {
		return err
	}

	return cmd.waitSnapshotToBeReadyStep()
}

// printAdvancement prints an advancement status on the procedure
func (cmd *snapshotCommand) printAdvancement(msg string, args ...interface{}) {
	fmt.Printf(msg, args...)
	fmt.Println()
}

// checkPreconditionsStep checks if the preconditions for the execution of this step are
// met or not. If they are not met, it will return an error
func (cmd *snapshotCommand) checkPreconditionsStep() error {
	// We should refuse to hibernate a cluster that was fenced already
	fencedInstances, err := utils.GetFencedInstances(cmd.cluster.Annotations)
	if err != nil {
		return fmt.Errorf("could not check if cluster is fenced: %v", err)
	}

	if fencedInstances.Len() > 0 {
		return fmt.Errorf("cannot hibernate a cluster that has fenced instances")
	}

	return nil
}

// fencePodStep fence the target Pod
func (cmd *snapshotCommand) fencePodStep() error {
	return fence.ApplyFenceFunc(
		cmd.ctx,
		plugin.Client,
		cmd.cluster.Name,
		plugin.Namespace,
		cmd.targetPod.Name,
		utils.AddFencedInstance,
	)
}

// rollbackFencePod removes the fencing status from the cluster
func (cmd *snapshotCommand) rollbackFencePod() {
	contextLogger := log.FromContext(cmd.ctx)

	cmd.printAdvancement("unfencing pod %s", cmd.targetPod.Name)
	err := fence.ApplyFenceFunc(
		cmd.ctx,
		plugin.Client,
		cmd.cluster.Name,
		plugin.Namespace,
		utils.FenceAllServers,
		utils.RemoveFencedInstance,
	)
	if err != nil {
		contextLogger.Error(
			err, "Rolling back from pod fencing failed",
			"targetPod", cmd.targetPod.Name,
		)
	}
}

// waitPodToBeFencedStep waits for the target Pod to be shut down
func (cmd *snapshotCommand) waitPodToBeFencedStep() error {
	cmd.printAdvancement("waiting for %s to be fenced", cmd.targetPod.Name)

	return retry.OnError(snapshotBackoff, pkgres.RetryAlways, func() error {
		running, err := resources.IsInstanceRunning(cmd.ctx, *cmd.targetPod)
		if err != nil {
			return fmt.Errorf("error checking instance status (%v): %w", cmd.targetPod.Name, err)
		}
		if running {
			return fmt.Errorf("instance still running (%v)", cmd.targetPod.Name)
		}
		return nil
	})
}

// snapshotPVCGroup creates a volumeSnapshot resource for every PVC
// used by the Pod
func (cmd *snapshotCommand) snapshotPVCGroupStep() error {
	cmd.snapshotTime = time.Now()

	for i := range cmd.pvcs {
		if err := cmd.createSnapshot(&cmd.pvcs[i]); err != nil {
			return err
		}
	}

	return nil
}

// waitSnapshotToBeReadyStep waits for every PVC snapshot to be ready to use
func (cmd *snapshotCommand) waitSnapshotToBeReadyStep() error {
	for i := range cmd.pvcs {
		name := cmd.getSnapshotName(cmd.pvcs[i].Name)
		if err := cmd.waitSnapshot(name); err != nil {
			return err
		}
	}

	return nil
}

// createSnapshot creates a VolumeSnapshot resource for the given PVC and
// add it to the command status
func (cmd *snapshotCommand) createSnapshot(pvc *corev1.PersistentVolumeClaim) error {
	name := cmd.getSnapshotName(pvc.Name)

	var snapshotClassName *string
	if cmd.snapshotClassName != "" {
		snapshotClassName = &cmd.snapshotClassName
	}

	snapshot := storagesnapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: pvc.Namespace,
		},
		Spec: storagesnapshotv1.VolumeSnapshotSpec{
			Source: storagesnapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: &pvc.Name,
			},
			VolumeSnapshotClassName: snapshotClassName,
		},
	}

	err := plugin.Client.Create(cmd.ctx, &snapshot)
	if err != nil {
		return fmt.Errorf("while creating VolumeSnapshot %s: %w", snapshot.Name, err)
	}

	return nil
}

// waitSnapshot waits for a certain snapshot to be ready to use
func (cmd *snapshotCommand) waitSnapshot(name string) error {
	cmd.printAdvancement("waiting for VolumeSnapshot %s to be ready to use", name)

	return retry.OnError(snapshotBackoff, pkgres.RetryAlways, func() error {
		var snapshot storagesnapshotv1.VolumeSnapshot

		err := plugin.Client.Get(
			cmd.ctx,
			client.ObjectKey{
				Namespace: cmd.cluster.Namespace,
				Name:      name,
			},
			&snapshot,
		)
		if err != nil {
			return fmt.Errorf("snapshot %s is not available: %w", name, err)
		}

		if snapshot.Status == nil || snapshot.Status.ReadyToUse == nil || !*snapshot.Status.ReadyToUse {
			return fmt.Errorf("snapshot %s is not ready to use", name)
		}

		return nil
	})
}

// getSnapshotName gets the snapshot name for a certain PVC
func (cmd *snapshotCommand) getSnapshotName(pvcName string) string {
	return fmt.Sprintf("%s-%v", pvcName, cmd.snapshotTime.Unix())
}
