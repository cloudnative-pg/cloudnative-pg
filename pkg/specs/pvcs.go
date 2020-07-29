/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package specs

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/api/v1alpha1"
)

const (
	// PvcUnusableAnnotation masks PVC when whey are not usable and permanently
	// failed
	PvcUnusableAnnotation = "k8s.2ndq.io/pvcUnusable"
)

// CreatePVC create spec of a PVC, given its name and the storage configuration
func CreatePVC(
	storageConfiguration v1alpha1.StorageConfiguration,
	name string,
	namespace string,
	nodeSerial int32,
) *corev1.PersistentVolumeClaim {
	pvcName := fmt.Sprintf("%s-%v", name, nodeSerial)

	result := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Annotations: map[string]string{
				ClusterSerialAnnotationName: strconv.Itoa(int(nodeSerial)),
			},
		},
	}

	// If the customer supplied a spec, let's use it
	if storageConfiguration.PersistentVolumeClaimTemplate != nil {
		storageConfiguration.PersistentVolumeClaimTemplate.DeepCopyInto(&result.Spec)
	}

	// If the customer specified a storage class, let's use it
	if storageConfiguration.StorageClass != nil {
		result.Spec.StorageClassName = storageConfiguration.StorageClass
	}

	// If the customer specified a storage requirement, let's use it
	if storageConfiguration.Size != nil {
		result.Spec.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				"storage": *storageConfiguration.Size,
			},
		}
	}

	if len(result.Spec.AccessModes) == 0 {
		result.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}

	return result
}
