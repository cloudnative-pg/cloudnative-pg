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
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/destroy"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin/fence"
	"github.com/cloudnative-pg/cloudnative-pg/internal/plugin/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	pkgres "github.com/cloudnative-pg/cloudnative-pg/pkg/resources"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

var hibernationBackoff = wait.Backoff{
	Steps:    4,
	Duration: 10 * time.Second,
	Factor:   5.0,
	Jitter:   0.1,
}

// onCommand represent the `hibernate on` subcommand
type onCommand struct {
	ctx                   context.Context
	cluster               *apiv1.Cluster
	primaryInstanceSerial int
	force                 bool
	shouldRollback        bool

	managedInstances []corev1.Pod
	primaryInstance  corev1.Pod
	pvcs             []corev1.PersistentVolumeClaim
}

// newOnCommand creates a new `hibernate on` command
func newOnCommand(ctx context.Context, clusterName string, force bool) (*onCommand, error) {
	var cluster apiv1.Cluster

	// Get the Cluster object
	err := plugin.Client.Get(
		ctx,
		client.ObjectKey{Namespace: plugin.Namespace, Name: clusterName},
		&cluster)
	if err != nil {
		return nil, fmt.Errorf("could not get cluster: %v", err)
	}

	// Get the instances to be hibernated
	managedInstances, primaryInstance, err := resources.GetInstancePods(ctx, clusterName)
	if err != nil {
		return nil, fmt.Errorf("could not get cluster pods: %w", err)
	}
	if primaryInstance.Name == "" {
		return nil, fmt.Errorf("no primary instance found, cannot proceed with the hibernation")
	}

	// Get the PVCs that will be hibernated
	pvcs, err := resources.GetInstancePVCs(ctx, clusterName, primaryInstance.Name)
	if err != nil {
		return nil, fmt.Errorf("cannot get PVCs: %w", err)
	}

	// Get the serial ID of the primary instance
	primaryInstanceSerial, err := specs.GetNodeSerial(primaryInstance.ObjectMeta)
	if err != nil {
		return nil, fmt.Errorf("could not get the primary node: %w", err)
	}

	contextLogger := log.FromContext(ctx).WithValues(
		"clusterName", clusterName,
		"primaryInstance", primaryInstance.Name)

	return &onCommand{
		ctx:                   log.IntoContext(ctx, contextLogger),
		cluster:               &cluster,
		primaryInstanceSerial: primaryInstanceSerial,
		managedInstances:      managedInstances,
		primaryInstance:       primaryInstance,
		pvcs:                  pvcs,
		force:                 force,
		shouldRollback:        false,
	}, nil
}

// execute executes the `hibernate on` command
func (on *onCommand) execute() error {
	// Check the hibernation preconditions
	if err := on.checkPreconditionsStep(); err != nil {
		return err
	}

	on.printAdvancement("hibernation process starting...")

	if err := on.fenceClusterStep(); err != nil {
		on.shouldRollback = true
		return err
	}
	defer on.rollbackFenceClusterIfNeeded()

	on.printAdvancement("waiting for the cluster to be fenced")

	if err := on.waitInstancesToBeFencedStep(); err != nil {
		on.shouldRollback = true
		return err
	}

	on.printAdvancement("cluster is now fenced, storing primary pg_controldata output")

	if err := on.annotatePVCStep(); err != nil {
		on.shouldRollback = true
		return err
	}
	defer on.rollBackAnnotationsIfNeeded()

	on.printAdvancement("PVC annotation complete")

	if err := on.deleteResourcesStep(); err != nil {
		on.shouldRollback = true
		return err
	}

	on.printAdvancement("Hibernation completed")
	return nil
}

// checkPreconditionsStep checks if the preconditions for the execution of this step are
// met or not. If they are not met, it will return an error
func (on *onCommand) checkPreconditionsStep() error {
	contextLogger := log.FromContext(on.ctx)

	// We should refuse to hibernate a cluster that was fenced already
	fencedInstances, err := utils.GetFencedInstances(on.cluster.Annotations)
	if err != nil {
		return fmt.Errorf("could not check if cluster is fenced: %v", err)
	}

	if fencedInstances.Len() > 0 {
		if on.force {
			contextLogger.Warning("Continuing hibernation procedure even if there are fenced instances")
		} else {
			return fmt.Errorf("cannot hibernate a cluster that has fenced instances")
		}
	}

	return nil
}

func (on *onCommand) fenceClusterStep() error {
	contextLogger := log.FromContext(on.ctx)

	contextLogger.Debug("applying the fencing annotation to the cluster manifest")
	if err := fence.ApplyFenceFunc(
		on.ctx,
		plugin.Client,
		on.cluster.Name,
		plugin.Namespace,
		utils.FenceAllServers,
		utils.AddFencedInstance,
	); err != nil {
		return err
	}
	contextLogger.Debug("fencing annotation set on the cluster manifest")

	return nil
}

// rollbackFenceClusterIfNeeded removes the fencing status from the
// cluster
func (on *onCommand) rollbackFenceClusterIfNeeded() {
	if !on.shouldRollback {
		return
	}

	contextLogger := log.FromContext(on.ctx)

	fmt.Println("rolling back hibernation: removing the fencing annotation")
	err := fence.ApplyFenceFunc(
		on.ctx,
		plugin.Client,
		on.cluster.Name,
		plugin.Namespace,
		utils.FenceAllServers,
		utils.RemoveFencedInstance,
	)
	if err != nil {
		contextLogger.Error(err, "Rolling back from hibernation failed")
	}
}

// waitInstancesToBeFenced waits for all instances to be shut down
func (on *onCommand) waitInstancesToBeFencedStep() error {
	for _, instance := range on.managedInstances {
		if err := retry.OnError(hibernationBackoff, pkgres.RetryAlways, func() error {
			running, err := resources.IsInstanceRunning(on.ctx, instance)
			if err != nil {
				return fmt.Errorf("error checking instance status (%v): %w", instance.Name, err)
			}
			if running {
				return fmt.Errorf("instance still running (%v)", instance.Name)
			}
			return nil
		}); err != nil {
			return err
		}
	}

	return nil
}

// annotatePVCStep stores the pg_controldata output
// into an annotation of the primary PVC
func (on *onCommand) annotatePVCStep() error {
	controlData, err := getPGControlData(on.ctx, on.primaryInstance)
	if err != nil {
		return fmt.Errorf("could not get primary control data: %w", err)
	}
	on.printAdvancement("primary pg_controldata output fetched")

	on.printAdvancement("annotating the PVC with the cluster manifest")
	if err := annotatePVCs(on.ctx, on.pvcs, on.cluster, controlData); err != nil {
		return fmt.Errorf("could not annotate PVCs: %w", err)
	}

	return nil
}

func (on *onCommand) rollBackAnnotationsIfNeeded() {
	if !on.shouldRollback {
		return
	}

	fmt.Println("rolling back hibernation: removing pvc annotations")
	err := removePVCannotations(on.ctx, on.pvcs)
	if err != nil {
		fmt.Printf("could not remove PVC annotations: %v", err)
	}
}

func (on *onCommand) deleteResourcesStep() error {
	on.printAdvancement("destroying the primary instance while preserving the pvc")

	// from this point there is no going back
	if err := destroy.Destroy(
		on.ctx,
		on.cluster.Name,
		strconv.Itoa(on.primaryInstanceSerial), true,
	); err != nil {
		return fmt.Errorf("error destroying primary instance: %w", err)
	}
	on.printAdvancement("primary instance destroy completed")

	on.printAdvancement("deleting the cluster resource")
	if err := plugin.Client.Delete(on.ctx, on.cluster); err != nil {
		return fmt.Errorf("error while deleting cluster resource: %w", err)
	}
	on.printAdvancement("cluster resource deletion complete")

	return nil
}

func (on *onCommand) printAdvancement(msg string) {
	fmt.Println(msg)
}

func annotatePVCs(
	ctx context.Context,
	pvcs []corev1.PersistentVolumeClaim,
	cluster *apiv1.Cluster,
	pgControlData string,
) error {
	for _, pvc := range pvcs {
		if err := retry.OnError(retry.DefaultBackoff, pkgres.RetryAlways, func() error {
			var currentPVC corev1.PersistentVolumeClaim
			if err := plugin.Client.Get(
				ctx,
				types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
				&currentPVC,
			); err != nil {
				return err
			}

			if currentPVC.Annotations == nil {
				currentPVC.Annotations = map[string]string{}
			}
			origPVC := currentPVC.DeepCopy()

			_, hasHibernateAnnotation := currentPVC.Annotations[utils.HibernateClusterManifestAnnotationName]
			_, hasPgControlDataAnnotation := currentPVC.Annotations[utils.HibernatePgControlDataAnnotationName]
			if hasHibernateAnnotation || hasPgControlDataAnnotation {
				return fmt.Errorf("the PVC already contains Hibernation annotations. Erroring out")
			}

			bytes, err := json.Marshal(&cluster)
			if err != nil {
				return err
			}

			currentPVC.Annotations[utils.HibernateClusterManifestAnnotationName] = string(bytes)
			currentPVC.Annotations[utils.HibernatePgControlDataAnnotationName] = pgControlData

			return plugin.Client.Patch(ctx, &currentPVC, client.MergeFrom(origPVC))
		}); err != nil {
			return err
		}
	}

	return nil
}

func removePVCannotations(
	ctx context.Context,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	for _, pvc := range pvcs {
		if err := retry.OnError(retry.DefaultBackoff, pkgres.RetryAlways, func() error {
			var currentPVC corev1.PersistentVolumeClaim
			if err := plugin.Client.Get(
				ctx,
				types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace},
				&currentPVC,
			); err != nil {
				return err
			}

			if currentPVC.Annotations == nil {
				return nil
			}
			origPVC := currentPVC.DeepCopy()

			delete(currentPVC.Annotations, utils.HibernateClusterManifestAnnotationName)
			delete(currentPVC.Annotations, utils.HibernatePgControlDataAnnotationName)

			return plugin.Client.Patch(ctx, &currentPVC, client.MergeFrom(origPVC))
		}); err != nil {
			return err
		}
	}

	return nil
}

func getPGControlData(ctx context.Context,
	pod corev1.Pod,
) (string, error) {
	timeout := time.Second * 10
	clientInterface := kubernetes.NewForConfigOrDie(plugin.Config)
	stdout, _, err := utils.ExecCommand(
		ctx,
		clientInterface,
		plugin.Config,
		pod,
		specs.PostgresContainerName,
		&timeout,
		"pg_controldata")
	if err != nil {
		return "", err
	}

	return stdout, nil
}
