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

// Package storage provides functions to manage enything related to storage
package storage

import (
	"context"
	"fmt"
	"os"

	volumesnapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

// GetStorageAllowExpansion returns the boolean value of the 'AllowVolumeExpansion' value of the storage class
func GetStorageAllowExpansion(
	ctx context.Context,
	crudClient client.Client,
	defaultStorageClass string,
) (*bool, error) {
	storageClass := &storagev1.StorageClass{}
	err := objects.Get(ctx, crudClient, client.ObjectKey{Name: defaultStorageClass}, storageClass)
	return storageClass.AllowVolumeExpansion, err
}

// IsWalStorageEnabled returns true if 'WalStorage' is being used
func IsWalStorageEnabled(
	ctx context.Context,
	crudClient client.Client,
	namespace, clusterName string,
) (bool, error) {
	cluster, err := clusterutils.Get(ctx, crudClient, namespace, clusterName)
	if cluster == nil || cluster.Spec.WalStorage == nil {
		return false, err
	}
	return true, err
}

// PvcHasLabels returns true if a PVC contains a given map of labels
func PvcHasLabels(
	pvc corev1.PersistentVolumeClaim,
	labels map[string]string,
) bool {
	pvcLabels := pvc.Labels
	for k, v := range labels {
		val, ok := pvcLabels[k]
		if !ok || (v != val) {
			return false
		}
	}
	return true
}

// ObjectHasAnnotations returns true if the object has the passed annotations
func ObjectHasAnnotations(
	object client.Object,
	annotations []string,
) bool {
	objectAnnotations := object.GetAnnotations()
	for _, v := range annotations {
		_, ok := objectAnnotations[v]
		if !ok {
			return false
		}
	}
	return true
}

// ObjectMatchesAnnotations returns true if the object has the passed annotations key/value
func ObjectMatchesAnnotations(
	object client.Object,
	annotations map[string]string,
) bool {
	objectAnnotations := object.GetAnnotations()
	for k, v := range annotations {
		value, ok := objectAnnotations[k]
		if !ok && (v != value) {
			return false
		}
	}
	return true
}

// EnvVarsForSnapshots represents the environment variables to use to track snapshots
// and apply them in test fixture templates
type EnvVarsForSnapshots struct {
	DataSnapshot             string
	WalSnapshot              string
	TablespaceSnapshotPrefix string
}

// SetSnapshotNameAsEnv sets the names of a PG_DATA, a PG_WAL and a list of PG_TABLESPACE snapshots from a
// given snapshotList as env variables
func SetSnapshotNameAsEnv(
	snapshotList *volumesnapshotv1.VolumeSnapshotList,
	backup *apiv1.Backup,
	envVars EnvVarsForSnapshots,
) error {
	if len(snapshotList.Items) != len(backup.Status.BackupSnapshotStatus.Elements) {
		return fmt.Errorf("could not find the expected number of snapshots")
	}

	for _, item := range snapshotList.Items {
		switch utils.PVCRole(item.Annotations[utils.PvcRoleLabelName]) {
		case utils.PVCRolePgData:
			err := os.Setenv(envVars.DataSnapshot, item.Name)
			if err != nil {
				return err
			}
		case utils.PVCRolePgWal:
			err := os.Setenv(envVars.WalSnapshot, item.Name)
			if err != nil {
				return err
			}
		case utils.PVCRolePgTablespace:
			tbsName := item.Labels[utils.TablespaceNameLabelName]
			err := os.Setenv(envVars.TablespaceSnapshotPrefix+"_"+tbsName, item.Name)
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("unrecognized PVC snapshot role: %s, name: %s",
				item.Annotations[utils.PvcRoleLabelName],
				item.Name,
			)
		}
	}
	return nil
}

// GetPVCList gathers the current list of PVCs in a namespace
func GetPVCList(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*corev1.PersistentVolumeClaimList, error) {
	pvcList := &corev1.PersistentVolumeClaimList{}
	err := crudClient.List(
		ctx, pvcList, client.InNamespace(namespace),
	)
	return pvcList, err
}

// GetSnapshotList gathers the current list of VolumeSnapshots in a namespace
func GetSnapshotList(
	ctx context.Context,
	crudClient client.Client,
	namespace string,
) (*volumesnapshotv1.VolumeSnapshotList, error) {
	list := &volumesnapshotv1.VolumeSnapshotList{}
	err := crudClient.List(ctx, list, client.InNamespace(namespace))

	return list, err
}

const (
	// IsDefaultClassAnnotation is the annotation used to mark the default StorageClass
	IsDefaultClassAnnotation = "storageclass.kubernetes.io/is-default-class"

	// DefaultSnapshotClassAnnotation is the annotation on a StorageClass that
	// names the associated VolumeSnapshotClass for CSI snapshots
	DefaultSnapshotClassAnnotation = "storage.kubernetes.io/default-snapshot-class"
)

// GetDefaultStorageClassName returns the name of the cluster's default
// StorageClass (annotated with is-default-class=true). It returns an error
// if zero or more than one default is found.
func GetDefaultStorageClassName(ctx context.Context, iface kubernetes.Interface) (string, error) {
	scList, err := iface.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing storage classes: %w", err)
	}

	var matches []string
	for i := range scList.Items {
		if scList.Items[i].Annotations[IsDefaultClassAnnotation] == "true" {
			matches = append(matches, scList.Items[i].Name)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no default storage class found in the cluster")
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple default storage classes found: %v", matches)
	}
}

// GetCSIStorageClassName returns the name of the StorageClass annotated with
// the default-snapshot-class annotation. It returns an empty string (no error)
// if no such StorageClass exists, and an error if more than one is found.
func GetCSIStorageClassName(ctx context.Context, iface kubernetes.Interface) (string, error) {
	scList, err := iface.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("listing storage classes: %w", err)
	}

	var matches []string
	for i := range scList.Items {
		if scList.Items[i].Annotations[DefaultSnapshotClassAnnotation] != "" {
			matches = append(matches, scList.Items[i].Name)
		}
	}

	switch len(matches) {
	case 0:
		return "", nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("multiple storage classes with %s annotation: %v",
			DefaultSnapshotClassAnnotation, matches)
	}
}

// GetDefaultVolumeSnapshotClassName returns the value of the
// default-snapshot-class annotation from the named StorageClass. It returns
// an empty string (no error) if csiStorageClass is empty or the annotation
// is absent.
func GetDefaultVolumeSnapshotClassName(
	ctx context.Context,
	iface kubernetes.Interface,
	csiStorageClass string,
) (string, error) {
	if csiStorageClass == "" {
		return "", nil
	}

	sc, err := iface.StorageV1().StorageClasses().Get(ctx, csiStorageClass, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("getting storage class %q: %w", csiStorageClass, err)
	}

	return sc.Annotations[DefaultSnapshotClassAnnotation], nil
}
