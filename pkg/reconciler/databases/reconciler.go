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

package databases

import (
	"context"
	"errors"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/resources/instance"
)

// Reconcile is the reconciler for managed databases
func Reconcile(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	instances []corev1.Pod,
	instanceClient instance.Client,
) (*ctrl.Result, error) {
	// There's no managed section
	if cluster.Spec.Managed == nil {
		return nil, nil
	}

	// If everything is reconciler, there's nothing to do
	if cluster.Generation == cluster.Status.ManagedDatabaseStatus.ObservedGeneration {
		return nil, nil
	}

	// There's nothing we can do on a replica cluster
	if cluster.IsReplica() {
		return nil, nil
	}

	var primaryPod *corev1.Pod
	for idx := range instances {
		if instances[idx].Name == cluster.Status.CurrentPrimary {
			primaryPod = &instances[idx]
			break
		}
	}

	if primaryPod == nil {
		return nil, fmt.Errorf("could not detect the primary pod")
	}

	oldCluster := cluster.DeepCopy()
	cluster.Status.ManagedDatabaseStatus.Database = make(
		[]apiv1.DatabaseStatus,
		len(cluster.Spec.Managed.Databases),
	)

	allReady := true
	for i := range cluster.Spec.Managed.Databases {
		desiredState := cluster.Spec.Managed.Databases[i]
		cluster.Status.ManagedDatabaseStatus.Database[i] = reconcileDatabase(
			ctx,
			desiredState,
			instanceClient,
			primaryPod,
		)
		allReady = allReady && cluster.Status.ManagedDatabaseStatus.Database[i].Ready
	}

	if allReady {
		cluster.Status.ManagedDatabaseStatus.ObservedGeneration = cluster.Generation
	}

	if err := c.Status().Patch(ctx, cluster, client.MergeFrom(oldCluster)); err != nil {
		return nil, err
	}

	if !allReady {
		return &ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	return nil, nil
}

func reconcileDatabase(
	ctx context.Context,
	desiredState apiv1.DatabaseConfiguration,
	instanceClient instance.Client,
	primaryPod *corev1.Pod,
) apiv1.DatabaseStatus {
	result := apiv1.DatabaseStatus{
		Name: desiredState.Name,
	}

	dbRequest := instance.PgDatabase{
		Owner:            desiredState.Owner,
		Encoding:         desiredState.Encoding,
		IsTemplate:       desiredState.IsTemplate,
		AllowConnections: desiredState.AllowConnections,
		ConnectionLimit:  desiredState.ConnectionLimit,
		Tablespace:       desiredState.Tablespace,
	}

	_, err := instanceClient.GetDatabase(ctx, primaryPod, desiredState.Name)
	switch {
	case errors.Is(err, instance.ErrDatabaseNotFound):
		if putError := instanceClient.PutDatabase(ctx, primaryPod, desiredState.Name, dbRequest); putError != nil {
			result.Ready = false
			result.ErrorMessage = putError.Error()
		} else {
			result.Ready = true
		}

	case err == nil:
		if patchError := instanceClient.PatchDatabase(ctx, primaryPod, desiredState.Name, dbRequest); patchError != nil {
			result.Ready = false
			result.ErrorMessage = patchError.Error()
		} else {
			result.Ready = true
		}

	default:
		result.Ready = false
		result.ErrorMessage = err.Error()
	}

	return result
}
