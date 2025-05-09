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

package controller

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/persistentvolumeclaim"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// reconcileRestoredCluster ensures that we own again any orphan resources when cluster gets reconciled for
// the first time
func (r *ClusterReconciler) reconcileRestoredCluster(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)

	// No need to check this on a cluster which has been already deployed
	if cluster.Status.LatestGeneratedNode != 0 {
		return nil, nil
	}

	// Get the list of PVCs belonging to this cluster but not owned by it
	pvcs, err := getOrphanPVCs(ctx, r.Client, cluster)
	if err != nil {
		return nil, err
	}
	if len(pvcs) == 0 {
		contextLogger.Info("no orphan PVCs found, skipping the restored cluster reconciliation")
		return nil, nil
	}
	contextLogger.Info("found orphan pvcs, trying to restore the cluster", "pvcs", pvcs)

	if res, err := ensureClusterRestoreCanStart(ctx, r.Client, cluster); res != nil || err != nil {
		return res, err
	}

	if err := ensureOrphanPodsAreDeleted(ctx, r.Client, cluster); err != nil {
		return nil, fmt.Errorf("encountered an error while deleting an orphan pod: %w", err)
	}

	highestSerial, primarySerial, err := getNodeSerialsFromPVCs(pvcs)
	if err != nil {
		return nil, err
	}

	if primarySerial == 0 {
		contextLogger.Info("no primary serial found, assigning the highest serial as the primary")
		primarySerial = highestSerial
	}

	contextLogger.Debug("proceeding to remove the fencing annotation if present")
	if err := ensureClusterIsNotFenced(ctx, r.Client, cluster); err != nil {
		return nil, err
	}

	contextLogger.Debug("proceeding to remove orphan services if present")
	if err := ensureOrphanServicesAreNotPresent(ctx, r.Client, cluster); err != nil {
		return nil, err
	}

	contextLogger.Debug("proceeding to restore the cluster status")
	if err := restoreClusterStatus(ctx, r.Client, cluster, highestSerial, primarySerial); err != nil {
		return nil, err
	}

	contextLogger.Debug("restored the cluster status, proceeding to restore the orphan PVCS")
	return nil, restoreOrphanPVCs(ctx, r.Client, cluster, pvcs)
}

func ensureOrphanServicesAreNotPresent(ctx context.Context, cli client.Client, cluster *apiv1.Cluster) error {
	if err := ensureOrphanServiceIsNotPresent(
		ctx,
		cli,
		client.ObjectKey{Name: cluster.GetServiceReadWriteName(), Namespace: cluster.Namespace},
		cluster.Name,
	); err != nil {
		return err
	}

	if cluster.IsReadOnlyServiceEnabled() {
		if err := ensureOrphanServiceIsNotPresent(
			ctx,
			cli,
			client.ObjectKey{Name: cluster.GetServiceReadOnlyName(), Namespace: cluster.Namespace},
			cluster.Name,
		); err != nil {
			return err
		}
	}

	if cluster.IsReadServiceEnabled() {
		if err := ensureOrphanServiceIsNotPresent(
			ctx,
			cli,
			client.ObjectKey{Name: cluster.GetServiceReadName(), Namespace: cluster.Namespace},
			cluster.Name,
		); err != nil {
			return err
		}
	}

	managedServices, err := specs.BuildManagedServices(*cluster)
	if err != nil {
		return err
	}
	for idx := range managedServices {
		if err := ensureOrphanServiceIsNotPresent(
			ctx,
			cli,
			client.ObjectKey{Name: managedServices[idx].Name, Namespace: cluster.Namespace},
			cluster.Name,
		); err != nil {
			return err
		}
	}

	return nil
}

func ensureOrphanServiceIsNotPresent(
	ctx context.Context,
	cli client.Client,
	objKey client.ObjectKey,
	clusterName string,
) error {
	contextLogger := log.FromContext(ctx).WithName("ensure_orphan_service_is_not_present")
	var svc corev1.Service
	err := cli.Get(ctx, objKey, &svc)
	if apierrs.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if owner, _ := IsOwnedByCluster(&svc); owner == clusterName {
		return nil
	}

	if len(svc.OwnerReferences) > 0 {
		contextLogger.Error(err, "while trying to ensure orphan services are deleted",
			"serviceName", svc.Name, "ownerReferences", svc.OwnerReferences,
		)
		return errors.New("service has owner references and it is not orphan")
	}

	if err := cli.Delete(ctx, &svc); err != nil && !apierrs.IsNotFound(err) {
		return err
	}

	return nil
}

// ensureClusterRestoreCanStart is a function where the plugins can inject their custom logic to tell the
// restore process to wait before starting the process
// nolint: revive
func ensureClusterRestoreCanStart(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
) (*ctrl.Result, error) {
	return nil, nil
}

func ensureClusterIsNotFenced(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
) error {
	return utils.NewFencingMetadataExecutor(cli).
		RemoveFencing().
		ForAllInstances().
		Execute(ctx, client.ObjectKeyFromObject(cluster), cluster)
}

// restoreClusterStatus bootstraps the status needed to make the restored cluster work
func restoreClusterStatus(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	latestNodeSerial int,
	targetPrimaryNodeSerial int,
) error {
	clusterOrig := cluster.DeepCopy()
	cluster.Status.LatestGeneratedNode = latestNodeSerial
	cluster.Status.TargetPrimary = specs.GetInstanceName(cluster.Name, targetPrimaryNodeSerial)
	return c.Status().Patch(ctx, cluster, client.MergeFrom(clusterOrig))
}

func getOrphanPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
) ([]corev1.PersistentVolumeClaim, error) {
	contextLogger := log.FromContext(ctx).WithValues("step", "get_orphan_pvcs")

	var pvcList corev1.PersistentVolumeClaimList
	if err := c.List(
		ctx,
		&pvcList,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels{utils.ClusterLabelName: cluster.Name},
	); err != nil {
		return nil, err
	}

	orphanPVCs := make([]corev1.PersistentVolumeClaim, 0, len(pvcList.Items))
	for _, pvc := range pvcList.Items {
		if len(pvc.OwnerReferences) != 0 {
			contextLogger.Warning("skipping pvc because it has owner metadata",
				"pvcName", pvc.Name)
			continue
		}
		if _, ok := pvc.Annotations[utils.ClusterSerialAnnotationName]; !ok {
			contextLogger.Warning("skipping pvc because it doesn't have serial annotation",
				"pvcName", pvc.Name)
			continue
		}

		orphanPVCs = append(orphanPVCs, pvc)
	}

	return orphanPVCs, nil
}

func ensureOrphanPodsAreDeleted(ctx context.Context, c client.Client, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx).WithName("orphan_pod_cleaner")

	var podList corev1.PodList
	if err := c.List(
		ctx,
		&podList,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels{utils.ClusterLabelName: cluster.Name},
	); err != nil {
		return err
	}

	orphanPodList := make([]corev1.Pod, 0, podList.Size())
	orphanPodNames := make([]string, 0, podList.Size())
	for idx := range podList.Items {
		pod := podList.Items[idx]
		if len(pod.OwnerReferences) == 0 {
			orphanPodList = append(orphanPodList, pod)
			orphanPodNames = append(orphanPodNames, pod.Name)
		}
	}

	if len(orphanPodList) == 0 {
		return nil
	}

	contextLogger.Info(
		"Found one or more orphan pods, deleting them",
		"orphanPodNames", orphanPodNames,
	)

	for idx := range orphanPodList {
		pod := orphanPodList[idx]
		contextLogger.Debug("Deleting orphan pod", "podName", pod.Name)
		if err := c.Delete(ctx, &pod); err != nil && !apierrs.IsNotFound(err) {
			return err
		}
	}

	return nil
}

// getNodeSerialsFromPVCs tries to obtain the highestSerial and the primary serial from a group of PVCs
func getNodeSerialsFromPVCs(
	pvcs []corev1.PersistentVolumeClaim,
) (int, int, error) {
	var highestSerial int
	var primarySerial int
	for _, pvc := range pvcs {
		serial, err := specs.GetNodeSerial(pvc.ObjectMeta)
		if err != nil {
			return 0, 0, err
		}
		if serial > highestSerial {
			highestSerial = serial
		}

		instanceRole, _ := utils.GetInstanceRole(pvc.Labels)
		if instanceRole == specs.ClusterRoleLabelPrimary {
			primarySerial = serial
		}
	}

	return highestSerial, primarySerial, nil
}

// restoreOrphanPVCs sets the owner metadata and re-actives the orphan pvcs
func restoreOrphanPVCs(
	ctx context.Context,
	c client.Client,
	cluster *apiv1.Cluster,
	pvcs []corev1.PersistentVolumeClaim,
) error {
	for i := range pvcs {
		pvc := &pvcs[i]
		if pvc.Annotations == nil {
			pvc.Annotations = map[string]string{}
		}

		pvcOrig := pvc.DeepCopy()
		cluster.SetInheritedDataAndOwnership(&pvc.ObjectMeta)
		pvc.Annotations[utils.PVCStatusAnnotationName] = persistentvolumeclaim.StatusReady
		// we clean hibernation metadata if it exists
		delete(pvc.Annotations, utils.HibernateClusterManifestAnnotationName)
		delete(pvc.Annotations, utils.HibernatePgControlDataAnnotationName)
		delete(pvc.Annotations, utils.ClusterManifestAnnotationName)
		delete(pvc.Annotations, utils.PgControldataAnnotationName)

		if err := c.Patch(ctx, pvc, client.MergeFrom(pvcOrig)); err != nil {
			return err
		}
	}

	return nil
}
