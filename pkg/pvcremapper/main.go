package pvcremapper

import (
	"fmt"

	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type InstancePVC struct {
	namespace string
	pvcName   string
	podName   string
	role      string
	pvName    string
	bound     bool
}

func InstancePVCFromPVC(pvc corev1.PersistentVolumeClaim) (iPVC InstancePVC, err error) {
	var exists bool
	iPVC.podName, exists = pvc.Labels[utils.InstanceNameLabelName]
	if !exists {
		return InstancePVC{}, fmt.Errorf("cluster PVC %s is missing label '%s'",
			utils.InstanceNameLabelName, pvc.Name)
	}
	iPVC.role, exists = pvc.Labels[utils.PvcRoleLabelName]
	if !exists {
		return InstancePVC{}, fmt.Errorf("cluster PVC %s is missing label '%s'",
			utils.ClusterInstanceRoleLabelName, pvc.Name)
	}
	iPVC.pvcName = pvc.Name
	iPVC.namespace = pvc.ObjectMeta.Namespace
	iPVC.pvName = pvc.Spec.VolumeName
	iPVC.bound = (pvc.Status.Phase == corev1.ClaimBound)
	return iPVC, nil
}

func (ipvc InstancePVC) PvName() string {
	return ipvc.pvName
}

func (ipvc InstancePVC) Name() string {
	return ipvc.pvcName
}

func (ipvc InstancePVC) RemapRequired() bool {
	if ipvc.pvcName != ipvc.ExpectedName() {
		return true
	}
	return false
}

func (ipvc InstancePVC) ExpectedName() string {
	if ipvc.role == "PG_DATA" {
		return ipvc.podName + configuration.Current.DataVolumeSuffix

	} else if ipvc.role == "PG_WAL" {
		return ipvc.podName + configuration.Current.WalArchiveVolumeSuffix
	}
	return ipvc.pvcName
}

func (ipvc InstancePVC) AsNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      ipvc.pvcName,
		Namespace: ipvc.namespace,
	}
}

type InstancePVCs map[string]InstancePVC

func InstancePVCsFromPVCs(pvcs []corev1.PersistentVolumeClaim) (ipvcs InstancePVCs, err error) {
	ipvcs = make(InstancePVCs)
	for _, pvc := range pvcs {
		ipvc, err := InstancePVCFromPVC(pvc)
		if err != nil {
			return nil, err
		}
		ipvcs[ipvc.pvcName] = ipvc
	}
	return ipvcs, nil
}

func (ipvcs InstancePVCs) Instances() (instances map[string]bool) {
	instances = map[string]bool{}
	for _, ipvc := range ipvcs {
		instances[ipvc.podName] = true
	}
	return instances
}

func (ipvcs InstancePVCs) RemapRequired() (forInstance InstancePVCs) {
	forInstance = make(InstancePVCs)
	for key, instance := range ipvcs {
		if instance.RemapRequired() {
			forInstance[key] = instance
		}
	}
	return forInstance
}

func (ipvcs InstancePVCs) ForInstance(instanceName string) (forInstance InstancePVCs) {
	forInstance = make(InstancePVCs)
	for key, instance := range ipvcs {
		if instance.podName == instanceName {
			forInstance[key] = instance
		}
	}
	return forInstance
}
