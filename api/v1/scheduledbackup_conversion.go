/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1

import (
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"github.com/EnterpriseDB/cloud-native-postgresql/api/v1alpha1"
)

// ConvertTo converts this Cluster to the Hub version (v1alpha1).
func (src *ScheduledBackup) ConvertTo(dstRaw conversion.Hub) error { //nolint:golint
	dst := dstRaw.(*v1alpha1.ScheduledBackup)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Cluster = src.Spec.Cluster
	dst.Spec.Schedule = src.Spec.Schedule
	dst.Spec.Suspend = src.Spec.Suspend

	// status
	dst.Status.LastCheckTime = src.Status.LastCheckTime
	dst.Status.LastScheduleTime = src.Status.LastScheduleTime

	return nil
}

// ConvertFrom converts from the Hub version (v1alpha1) to this version.
func (dst *ScheduledBackup) ConvertFrom(srcRaw conversion.Hub) error { //nolint:golint
	src := srcRaw.(*v1alpha1.ScheduledBackup)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Cluster = src.Spec.Cluster
	dst.Spec.Schedule = src.Spec.Schedule
	dst.Spec.Suspend = src.Spec.Suspend

	// status
	dst.Status.LastCheckTime = src.Status.LastCheckTime
	dst.Status.LastScheduleTime = src.Status.LastScheduleTime

	return nil
}
