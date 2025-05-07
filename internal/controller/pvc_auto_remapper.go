package controller

import (
	"context"
	"time"

	"fmt"

	"github.com/avast/retry-go/v4"
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/pvcremapper"
	"github.com/cloudnative-pg/machinery/pkg/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	remapperPatchTimeoutSec  = 60
	remapperDeleteTimeoutSec = 120
	remapperCreateTimeoutSec = 60
)

var remapperTimeoutDelay = time.Second

// CreateAndWaitForReady creates a given pod object and wait for it to be ready
func (r *ClusterReconciler) waitForDeletion(
	ctx context.Context,
	obj client.Object,
) error {
	return retry.Do(
		func() error {
			err := r.Client.Get(ctx, client.ObjectKeyFromObject(obj), obj)
			if err == nil {
				return fmt.Errorf("object still exists")
			} else if errors.IsNotFound(err) {
				return nil
			}
			return err
		},
		retry.Attempts(remapperDeleteTimeoutSec),
		retry.Delay(remapperTimeoutDelay),
		retry.DelayType(retry.FixedDelay),
	)
}

func (r *ClusterReconciler) deletePVC(ctx context.Context, namespacedName types.NamespacedName) error {
	contextLogger := log.FromContext(ctx)
	var pvc corev1.PersistentVolumeClaim
	if err := r.Get(ctx, namespacedName, &pvc); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		contextLogger.Error(err, "failed to retrieve PVC")
		return err
	}
	if pvc.Status.Phase == corev1.ClaimBound {
		pvPolicy, err := r.getPVPolicy(ctx, pvc.Spec.VolumeName)
		if err != nil {
			contextLogger.Error(err, "failed to get PVC policy")
			return err
		}
		if pvPolicy != "Retain" {
			err = fmt.Errorf("cowardly refusing to remove bound PVC when PV policy is not set to Retain")
			contextLogger.Error(err, "pv policy is not Retain")
			return err
		}
	}

	// First we need to make sure the operator is not triggered by removing
	patchedPvc := pvc.DeepCopy()
	patchedPvc.OwnerReferences = nil
	//patchedPvc.Labels = nil
	//patchedPvc.Annotations = nil
	if err := r.Patch(ctx, patchedPvc, client.MergeFrom(&pvc)); err != nil {
		contextLogger.Error(err, "failed to remove owner references from PVC")
		return err
	}
	err := retry.Do(
		func() error {
			var pvc corev1.PersistentVolumeClaim
			err := r.Get(ctx, namespacedName, &pvc)
			if err != nil {
				return err
			}
			if len(pvc.OwnerReferences) > 0 {
				return fmt.Errorf("still owner references")
			}
			return nil
		},
		retry.Attempts(remapperPatchTimeoutSec),
		retry.Delay(remapperTimeoutDelay),
		retry.DelayType(retry.FixedDelay),
	)
	if err != nil {
		contextLogger.Error(err, "failed to patch PVC")
		return err
	}
	if err := r.Delete(ctx, &pvc); err != nil {
		contextLogger.Error(err, "failed to delete PVC")
		return err
	}
	if err = r.waitForDeletion(ctx, &pvc); err != nil {
		contextLogger.Error(err, "failed to wait for deletion of PVC")
		return err
	}
	return nil
}

func (r *ClusterReconciler) getPVPolicy(
	ctx context.Context,
	pvName string,
) (orgPolicy corev1.PersistentVolumeReclaimPolicy, err error) {
	var pv corev1.PersistentVolume
	err = r.Get(ctx, types.NamespacedName{Name: pvName}, &pv)
	if err != nil {
		log.FromContext(ctx).Error(err, "failed to get PV", "pv", pvName)
		return "", err
	}
	return pv.Spec.PersistentVolumeReclaimPolicy, nil
}

func (r *ClusterReconciler) setPVPolicy(
	ctx context.Context,
	pvName string,
	policy corev1.PersistentVolumeReclaimPolicy,
) (orgPolicy corev1.PersistentVolumeReclaimPolicy, err error) {
	contextLogger := log.FromContext(ctx)
	var pv corev1.PersistentVolume
	err = r.Get(ctx, types.NamespacedName{Name: pvName}, &pv)
	if err != nil {
		contextLogger.Error(err, "failed to get PV", "pv", pvName)
		return "", err
	}
	if pv.Spec.PersistentVolumeReclaimPolicy == policy {
		return "", nil
	}
	patchedPv := pv.DeepCopy()
	orgPolicy = patchedPv.Spec.PersistentVolumeReclaimPolicy
	patchedPv.Spec.PersistentVolumeReclaimPolicy = policy
	if err = r.Patch(ctx, patchedPv, client.MergeFrom(&pv)); err != nil {
		contextLogger.Error(err, "failed to patch PV reclaim policy", "pv", pvName)
	}

	err = retry.Do(
		func() error {
			if newPolicy, err := r.getPVPolicy(ctx, pvName); err != nil {
				return err
			} else if newPolicy == policy {
				return nil
			}
			return fmt.Errorf("failed to update policy on PV %s", pvName)
		},
		retry.Attempts(remapperPatchTimeoutSec),
		retry.Delay(remapperTimeoutDelay),
		retry.DelayType(retry.FixedDelay),
	)
	if err != nil {
		contextLogger.Error(err, "patching PV took too long", "pv", pvName)
		return orgPolicy, err
	}
	return orgPolicy, nil
}

func (r *ClusterReconciler) setPVClaimRef(
	ctx context.Context,
	pvName string,
	pvc *corev1.PersistentVolumeClaim,
) (err error) {
	contextLogger := log.FromContext(ctx)
	var pv corev1.PersistentVolume
	err = r.Get(ctx, types.NamespacedName{Name: pvName}, &pv)
	if err != nil {
		contextLogger.Error(err, "failed to get PV", "pv", pvName)
		return err
	}
	patchedPv := pv.DeepCopy()

	patchedPv.Spec.ClaimRef = &corev1.ObjectReference{
		APIVersion:      pvc.APIVersion,
		Kind:            pvc.Kind,
		Namespace:       pvc.Namespace,
		ResourceVersion: pvc.ResourceVersion,
		UID:             pvc.UID,
		Name:            pvc.Name,
	}

	if err = r.Patch(ctx, patchedPv, client.MergeFrom(&pv)); err != nil {
		contextLogger.Error(err, "failed to patch PV reclaim policy", "pv", pvName)
	}

	err = retry.Do(
		func() error {
			var patchedPv corev1.PersistentVolume
			err = r.Get(ctx, types.NamespacedName{Name: pvName}, &patchedPv)
			if err != nil {
				contextLogger.Error(err, "failed to get PV", "pv", pvName)
				return err
			}
			if patchedPv.Spec.ClaimRef == nil || patchedPv.Spec.ClaimRef.UID != pvc.UID {
				return fmt.Errorf("PV %s not yet updated", pvName)
			}
			return nil
		},
		retry.Attempts(remapperPatchTimeoutSec),
		retry.Delay(remapperTimeoutDelay),
		retry.DelayType(retry.FixedDelay),
	)
	if err != nil {
		contextLogger.Error(err, "patching PV took too long", "pv", pvName)
		return err
	}
	return nil
}

func (r *ClusterReconciler) deletePod(
	ctx context.Context,
	pod corev1.Pod,
) error {
	// First we need to make sure the operator is not triggered by removing
	contextLogger := log.FromContext(ctx)
	podName := types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}
	patchedPod := pod.DeepCopy()
	patchedPod.OwnerReferences = nil
	// patchedPod.Labels = nil
	// patchedPod.Annotations = nil
	err := r.Patch(ctx, patchedPod, client.MergeFrom(&pod))
	if err != nil {
		contextLogger.Error(err, "failed to remove owner references from pod", "pod", podName)
		return err
	}
	err = retry.Do(
		func() error {
			var pod corev1.Pod
			err = r.Get(ctx, podName, &pod)
			if err != nil {
				return err
			}
			if len(pod.OwnerReferences) > 0 {
				return fmt.Errorf("still owner references")
			}
			return nil
		},
		retry.Attempts(remapperPatchTimeoutSec),
		retry.Delay(remapperTimeoutDelay),
		retry.DelayType(retry.FixedDelay),
	)
	if err := r.Delete(ctx, &pod); err != nil {
		contextLogger.Error(err, "failed to delete pod", "pod", podName)
		return err
	}
	if err = r.waitForDeletion(ctx, &pod); err != nil {
		contextLogger.Error(err, "waiting for deletion took too long", "pod", podName)
		return err
	}
	return nil
}

func (r *ClusterReconciler) recreatePodWithNewVolumeBlock(
	ctx context.Context,
	instanceName types.NamespacedName,
	toRemap pvcremapper.InstancePVCs,
) error {
	contextLogger := log.FromContext(ctx)

	var orgPod corev1.Pod
	err := r.Get(ctx, instanceName, &orgPod)
	if err != nil {
		if errors.IsNotFound(err) {
			contextLogger.Info("pod not found, remapping without recreating pod", "podname", instanceName)
			return nil
		} else {
			contextLogger.Error(err, "failed to get pod info", "instance", instanceName)
			return err
		}
	}
	newPod := orgPod.DeepCopy()
	var volumes []corev1.Volume
	for _, volume := range newPod.Spec.Volumes {
		for _, ipvc := range toRemap {
			if volume.PersistentVolumeClaim != nil &&
				volume.PersistentVolumeClaim.ClaimName == ipvc.Name() {
				volume.PersistentVolumeClaim.ClaimName = ipvc.ExpectedName()
			}
		}
		volumes = append(volumes, volume)
	}
	newPod.Spec.Volumes = volumes
	newPod.ResourceVersion = ""
	contextLogger.Info("recreating pod", "podname", newPod.Name)
	if err := r.deletePod(ctx, orgPod); err != nil {
		return err
	} else if err = r.Create(ctx, newPod); err != nil {
		contextLogger.Error(err, "failed to recreate pod", "instance", instanceName)
		return err
	}
	contextLogger.Info("recreating pod succeeded", "podname", instanceName)
	return nil
}

func (r *ClusterReconciler) clonePVC(
	ctx context.Context,
	ipvc pvcremapper.InstancePVC,
) (newPvc *corev1.PersistentVolumeClaim, err error) {
	contextLogger := log.FromContext(ctx)
	source := ipvc.AsNamespacedName()
	var oldPvc corev1.PersistentVolumeClaim
	err = r.Get(ctx, source, &oldPvc)
	if err != nil {
		contextLogger.Error(err, "failed to get old PVC", "pvc", source)
		return nil, err
	}
	dest := ipvc.AsNamespacedName()
	dest.Name = ipvc.ExpectedName()
	if err := r.deletePVC(ctx, dest); err != nil {
		return nil, err
	}

	newPVC := oldPvc.DeepCopy()
	newPVC.Name = dest.Name
	newPVC.Namespace = dest.Namespace
	newPVC.Status = corev1.PersistentVolumeClaimStatus{}
	newPVC.ResourceVersion = ""
	newPVC.UID = ""
	if err = r.Create(ctx, newPVC); err != nil {
		contextLogger.Error(err, "failed to recreate PVC with new name", "pvc", dest)
		return nil, err
	}
	err = retry.Do(
		func() error { return r.Client.Get(ctx, dest, newPVC) },
		retry.Attempts(remapperCreateTimeoutSec),
		retry.Delay(remapperTimeoutDelay),
		retry.DelayType(retry.FixedDelay),
	)
	if err != nil {
		contextLogger.Error(err, "failed to get new PVC", "pvc", dest)
		return nil, err
	}
	return newPVC, nil
}

func (r *ClusterReconciler) pvcRemapping(
	ctx context.Context,
	ipvc pvcremapper.InstancePVC,
) error {
	if !ipvc.RemapRequired() {
		return nil
	}
	contextLogger := log.FromContext(ctx)
	orgPolicy, err := r.setPVPolicy(ctx, ipvc.PvName(), corev1.PersistentVolumeReclaimRetain)
	if err != nil {
		return err
	}
	contextLogger.Info("original policy", "policy", orgPolicy, "pv", ipvc.PvName())
	// Reset on exit
	if orgPolicy != "" {
		defer func() {
			if _, err := r.setPVPolicy(ctx, ipvc.PvName(), orgPolicy); err != nil {
				contextLogger.Error(err, "failed to reset policy on PV", "pv", ipvc.PvName(), "original policy", orgPolicy)
			}
		}()
	}

	newPVC, err := r.clonePVC(ctx, ipvc)
	if err != nil {
		return err
	}
	if err = r.setPVClaimRef(ctx, ipvc.PvName(), newPVC); err != nil {
		return err
	}
	contextLogger.Info("pv claimref reset", "pv", ipvc.PvName())
	source := ipvc.AsNamespacedName()
	if err = r.deletePVC(ctx, source); err != nil {
		contextLogger.Error(err, "failed to delete PVC", "pvc", source)
		return err
	}
	contextLogger.Info("pvc removed", "pvc", source)
	return nil
}

func (r *ClusterReconciler) instanceRemapping(
	ctx context.Context,
	instanceName types.NamespacedName,
	instancePvcs pvcremapper.InstancePVCs,
) error {
	contextLogger := log.FromContext(ctx)
	toRemap := instancePvcs.ForInstance(instanceName.Name).RemapRequired()
	if len(toRemap) == 0 {
		return nil
	}
	contextLogger.Info("instance pvc info",
		"instance", instanceName,
		"pvc count", len(instancePvcs),
		"pvc to remap count", len(toRemap),
	)

	if err := r.recreatePodWithNewVolumeBlock(ctx, instanceName, toRemap); err != nil {
		return err
	}

	for _, ipvc := range toRemap {
		contextLogger.Info("remapping", "pvc", ipvc.Name())
		if err := r.pvcRemapping(ctx, ipvc); err != nil {
			return err
		}
	}
	return nil
}

func (r *ClusterReconciler) autoPVCRemapping(
	ctx context.Context,
	cluster *apiv1.Cluster,
	instancesStatus postgres.PostgresqlStatusList,
	pvcRemapper pvcremapper.InstancePVCs,
) (err error) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("clusterwide pvc info",
		"pvc count", len(pvcRemapper),
		"instances", pvcRemapper.Instances(),
	)
	for instanceName := range pvcRemapper.Instances() {
		if instanceName == cluster.Status.CurrentPrimary {
			continue
		}
		instanceNamespacedName := types.NamespacedName{Namespace: cluster.Namespace, Name: instanceName}
		if err = r.instanceRemapping(ctx, instanceNamespacedName, pvcRemapper.ForInstance(instanceName)); err != nil {
			return err
		}
	}

	if cluster.Status.CurrentPrimary == "" {
		return nil
	}
	primaryNamespaceName := types.NamespacedName{Name: cluster.Status.CurrentPrimary, Namespace: cluster.Namespace}
	if err = r.instanceRemapping(ctx, primaryNamespaceName, pvcRemapper.ForInstance(cluster.Status.CurrentPrimary)); err != nil {
		return err
	}
	/*
		var primary corev1.Pod
		if err = r.Get(ctx, primaryNamespaceName, &primary); err != nil {
			contextLogger.Error(err, "failed to get pod info", "primary", primaryNamespaceName)
			return err
		}
		if _, err := r.updatePrimaryPod(ctx, cluster, &instancesStatus, primary, false, false, "remapping PVC's"); err != nil {
			contextLogger.Error(err, "switchover primary failed", "primary", primaryNamespaceName)
			return err
		}
	*/
	return nil
}
