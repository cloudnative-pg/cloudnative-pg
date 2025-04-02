package controller

import (
	"context"
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/pvcremapper"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *ClusterReconciler) cleanPVC(ctx context.Context, namespacedName types.NamespacedName) (gone bool, err error) {
	var pvc corev1.PersistentVolumeClaim
	err = r.Get(ctx, namespacedName, &pvc)
	if err != nil {
		if errors.IsNotFound(err) {
			return true, nil
		}
		return false, fmt.Errorf("unexpectedly failed to retrieve PVC %s: %w",
			namespacedName.String(),
			err,
		)
	}
	if pvc.Status.Phase == corev1.ClaimBound {
		return false, fmt.Errorf("cowardly refusing to remove bound PVC")
	}
	err = r.Delete(ctx, &pvc)
	if err != nil {
		return false, fmt.Errorf("unexpectedly failed to delete PVC %s: %w",
			namespacedName.String(),
			err,
		)
	}
	return true, nil
}

func (r *ClusterReconciler) getPod(ctx context.Context, namespacedName types.NamespacedName) (pod *corev1.Pod, err error) {
	err = r.Get(ctx, namespacedName, pod)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve pod %s: %w",
			namespacedName.String(),
			err,
		)
	}
	return pod, nil
}

func (r *ClusterReconciler) clonePVC(ctx context.Context, ipvc pvcremapper.InstancePVC) error {
	if !ipvc.RemapRequired() {
		return nil
	}
	nsName := ipvc.AsNamespacedName()
	var oldPvc corev1.PersistentVolumeClaim
	err := r.Get(ctx, nsName, &oldPvc)
	if err != nil {
		return fmt.Errorf("unexpectedly failed to get PVC %s: %w",
			nsName.String(),
			err,
		)
	}
	nsName.Name = ipvc.ExpectedName()
	if gone, err := r.cleanPVC(ctx, nsName); err != nil {
		return err
	} else if !gone {
		// Seems like new PVC was already in use, not removing, but leaving as it is
		return nil
	}

	oldPvc.Name = ipvc.ExpectedName()
	oldPvc.Status = corev1.PersistentVolumeClaimStatus{}
	oldPvc.ResourceVersion = ""
	err = r.Create(ctx, &oldPvc)
	if err != nil {
		return fmt.Errorf("unexpectedly failed to create PVC %s: %w",
			ipvc.Name(),
			err,
		)
	}
	return nil
}

func (r *ClusterReconciler) setPVPolicy(
	ctx context.Context,
	pvName string,
	policy corev1.PersistentVolumeReclaimPolicy,
) (orgPolicy corev1.PersistentVolumeReclaimPolicy, err error) {
	if pvName == "" {
		return "", nil
	}
	var pv corev1.PersistentVolume
	err = r.Get(ctx, types.NamespacedName{Name: pvName}, &pv)
	if err != nil {
		return "", fmt.Errorf("unexpectedly failed to get PV %s: %w",
			pvName,
			err,
		)
	}
	if pv.Spec.PersistentVolumeReclaimPolicy == policy {
		return policy, nil
	}
	patchedPv := pv.DeepCopy()
	orgPolicy = patchedPv.Spec.PersistentVolumeReclaimPolicy
	patchedPv.Spec.PersistentVolumeReclaimPolicy = policy

	return orgPolicy, r.Patch(ctx, patchedPv, client.MergeFrom(&pv))
}

func (r *ClusterReconciler) remap(
	ctx context.Context,
	pod *corev1.Pod,
	toRemap pvcremapper.InstancePVCs,
) (done bool, err error) {
	var policies = make(map[string]bool)
	toRemap = toRemap.ForInstance(pod.Name)
	if len(toRemap) == 0 {
		return true, nil
	}
	if role, exists := pod.Labels[utils.ClusterInstanceRoleLabelName]; exists && role == specs.ClusterRoleLabelPrimary {
		return false, nil
	}
	for _, ipvc := range toRemap {
		if err := r.clonePVC(ctx, ipvc); err != nil {
			return false, err
		}
		if _, exists := policies[ipvc.PvName()]; !exists {
			orgPolicy, err := r.setPVPolicy(ctx, ipvc.PvName(), corev1.PersistentVolumeReclaimRetain)
			if err != nil {
				return false, fmt.Errorf("unexpectedly failed to patch PV %s: %w",
					ipvc.PvName(),
					err,
				)
			}
			// Prevent to set a second time
			policies[ipvc.PvName()] = true
			// Reset on exit
			if orgPolicy != "" {
				defer r.setPVPolicy(ctx, ipvc.PvName(), orgPolicy)
			}
		}
	}
	var volumes []corev1.Volume
	for _, volume := range pod.Spec.Volumes {
		for _, ipvc := range toRemap {
			if volume.PersistentVolumeClaim != nil &&
				volume.PersistentVolumeClaim.ClaimName == ipvc.Name() {
				volume.PersistentVolumeClaim.ClaimName = ipvc.ExpectedName()
			}
			volumes = append(volumes, volume)
		}
	}
	pod.Spec.Volumes = volumes
	pod.ResourceVersion = ""
	err = r.Delete(ctx, pod)
	if err != nil {
		return false, err
	}
	defer r.Create(ctx, pod)

	for _, ipvc := range toRemap {
		if gone, err := r.cleanPVC(ctx, ipvc.AsNamespacedName()); err != nil {
			return false, err
		} else if !gone {
			return false, fmt.Errorf("old PVC %s should be cleaned but wasn't",
				ipvc.AsNamespacedName().String(),
			)
		}
	}
	return true, nil
}

func (r *ClusterReconciler) reconcileRemapping(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
	instancesStatus postgres.PostgresqlStatusList,
) error {
	var primaryPod *corev1.Pod
	contextLogger := log.FromContext(ctx)
	pvcRemapper, err := pvcremapper.InstancePVCsFromPVCs(resources.pvcs.Items)
	if err != nil {
		contextLogger.Error(err, "Unexpected PVC's linked to this cluster, autoremapping disabled",
			"pvc", resources.pvcs.Items,
		)
	}
	contextLogger.Info("clusterwide pvc info",
		"pvc count", len(pvcRemapper),
		"instances", pvcRemapper.Instances(),
	)
	for instanceName := range pvcRemapper.Instances() {
		instancePvcs := pvcRemapper.ForInstance(instanceName).RemapRequired()
		contextLogger.Info("clusterwide instance info",
			"pvc count", len(instancePvcs),
			"pvc's to remap", len(instancePvcs.RemapRequired()),
		)
		instancePvcsToRemap := instancePvcs.RemapRequired()
		if len(instancePvcsToRemap) != 0 {
			pod, err := r.getPod(ctx,
				types.NamespacedName{Name: instanceName, Namespace: cluster.ObjectMeta.Namespace},
			)
			if err != nil {
				return fmt.Errorf(
					"failed to get pod info for instance %s: %w",
					instanceName,
					err,
				)
			}
			contextLogger.Info("remapping is required",
				"instance", instanceName,
			)
			done, err := r.remap(
				ctx,
				pod,
				instancePvcsToRemap,
			)
			if err != nil {
				return fmt.Errorf(
					"remapping failed for instance %s: %w",
					instanceName,
					err,
				)
			}
			if !done {
				primaryPod = pod
			}
		}
	}

	if primaryPod == nil {
		return nil
	}
	if done, err := r.updatePrimaryPod(ctx, cluster, &instancesStatus, *primaryPod, true, true, "remapping PVC's"); err != nil {
		return fmt.Errorf(
			"remapping failed for instance %s: %w",
			primaryPod.Name,
			err,
		)
	} else if !done {
		return fmt.Errorf(
			"primary pod %s is not migrated to new storage: %w",
			primaryPod.Name,
			err,
		)
	}
	return nil

}
