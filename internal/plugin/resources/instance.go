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

package resources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// https://www.postgresql.org/docs/current/app-pg-ctl.html
type pgCtlStatusExitCode int

const (
	pgCtlStatusStopped               pgCtlStatusExitCode = 3
	pgCtlStatusNoAccessibleDirectory pgCtlStatusExitCode = 4
)

// GetInstancePods gets all the pods belonging to a given cluster
// returns an array with all the instances, the primary instance and any error encountered.
func GetInstancePods(ctx context.Context, clusterName string) ([]v1.Pod, v1.Pod, error) {
	var pods v1.PodList
	if err := plugin.Client.List(ctx, &pods, client.InNamespace(plugin.Namespace)); err != nil {
		return nil, v1.Pod{}, err
	}

	var managedPods []v1.Pod
	var primaryPod v1.Pod
	for idx := range pods.Items {
		for _, owner := range pods.Items[idx].ObjectMeta.OwnerReferences {
			if owner.Kind == corev1.ClusterKind && owner.Name == clusterName {
				managedPods = append(managedPods, pods.Items[idx])
				if specs.IsPodPrimary(pods.Items[idx]) {
					primaryPod = pods.Items[idx]
				}
			}
		}
	}
	return managedPods, primaryPod, nil
}

// ExtractInstancesStatus extracts the instance status from the given pod list
func ExtractInstancesStatus(
	ctx context.Context,
	config *rest.Config,
	filteredPods []v1.Pod,
	postgresContainerName string,
) postgres.PostgresqlStatusList {
	var result postgres.PostgresqlStatusList

	for idx := range filteredPods {
		instanceStatus := getReplicaStatusFromPodViaExec(
			ctx, config, filteredPods[idx], postgresContainerName)
		result.Items = append(result.Items, instanceStatus)
	}

	return result
}

func getReplicaStatusFromPodViaExec(
	ctx context.Context,
	config *rest.Config,
	pod v1.Pod,
	postgresContainerName string,
) postgres.PostgresqlStatus {
	var result postgres.PostgresqlStatus
	timeout := time.Second * 10
	clientInterface := kubernetes.NewForConfigOrDie(config)
	stdout, _, err := utils.ExecCommand(
		ctx,
		clientInterface,
		config,
		pod,
		postgresContainerName,
		&timeout,
		"/controller/manager", "instance", "status")
	if err != nil {
		result.AddPod(pod)
		result.Error = fmt.Errorf("pod not available")
		return result
	}

	err = json.Unmarshal([]byte(stdout), &result)
	if err != nil {
		result.Error = fmt.Errorf("can't parse pod output")
	}

	result.AddPod(pod)

	return result
}

// GetInstancePVCs gets all the PVC associated with a given instance
func GetInstancePVCs(
	ctx context.Context,
	clusterName string,
	instanceName string,
) ([]v1.PersistentVolumeClaim, error) {
	cluster := &corev1.Cluster{}
	if err := plugin.Client.Get(
		ctx,
		types.NamespacedName{
			Name:      clusterName,
			Namespace: plugin.Namespace,
		},
		cluster,
	); err != nil {
		return nil, err
	}

	var pvcs []v1.PersistentVolumeClaim

	pgDataName := persistentvolumeclaim.GetName(instanceName, utils.PVCRolePgData)
	pgData, err := getPVC(ctx, pgDataName)
	if err != nil {
		return nil, err
	}
	if pgData != nil {
		pvcs = append(pvcs, *pgData)
	}

	pgWalName := persistentvolumeclaim.GetName(instanceName, utils.PVCRolePgWal)
	pgWal, err := getPVC(ctx, pgWalName)
	if err != nil {
		return nil, err
	}
	if pgWal != nil {
		pvcs = append(pvcs, *pgWal)
	}

	return pvcs, nil
}

// getPVC returns the pvc if found or any error that isn't apierrs.IsNotFound
func getPVC(ctx context.Context, name string) (*v1.PersistentVolumeClaim, error) {
	var pvc v1.PersistentVolumeClaim
	err := plugin.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: plugin.Namespace}, &pvc)
	if apierrs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pvc, nil
}

// IsInstanceRunning returns a boolean indicating if the given instance is running and any error encountered
func IsInstanceRunning(
	ctx context.Context,
	pod v1.Pod,
) (bool, error) {
	contextLogger := log.FromContext(ctx).WithName("plugin.IsInstanceRunning")
	timeout := time.Second * 10
	clientInterface := kubernetes.NewForConfigOrDie(plugin.Config)
	stdout, stderr, err := utils.ExecCommand(
		ctx,
		clientInterface,
		plugin.Config,
		pod,
		specs.PostgresContainerName,
		&timeout,
		"pg_ctl", "status")
	if err == nil {
		return true, nil
	}

	var codeExitError exec.CodeExitError
	if errors.As(err, &codeExitError) {
		switch pgCtlStatusExitCode(codeExitError.Code) {
		case pgCtlStatusStopped:
			return false, nil
		case pgCtlStatusNoAccessibleDirectory:
			return false, fmt.Errorf("could not check instance status: no accessible data directory")
		}
	}

	contextLogger.Debug("encountered an error while getting instance status",
		"stdout", stdout,
		"stderr", stderr,
		"err", err,
	)

	return false, err
}
