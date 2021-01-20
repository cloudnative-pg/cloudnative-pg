/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package specs

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
)

var (
	// ErrorInvalidSize is raised when the size specified by the
	// user is not valid and can't be specified in a PVC declaration
	ErrorInvalidSize = fmt.Errorf("invalid storage size")
)

// CreatePVC create spec of a PVC, given its name and the storage configuration
func CreatePVC(
	storageConfiguration v1alpha1.StorageConfiguration,
	name string,
	namespace string,
	nodeSerial int32,
) (*corev1.PersistentVolumeClaim, error) {
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

	// Insert the storage requirement
	parsedSize, err := resource.ParseQuantity(storageConfiguration.Size)
	if err != nil {
		return nil, ErrorInvalidSize
	}

	result.Spec.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			"storage": parsedSize,
		},
	}

	if len(result.Spec.AccessModes) == 0 {
		result.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}

	return result, nil
}
