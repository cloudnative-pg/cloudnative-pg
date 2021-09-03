/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package v1alpha1

import (
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	v1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
)

// ConvertTo converts this Cluster to the Hub version (v1).
func (src *ScheduledBackup) ConvertTo(dstRaw conversion.Hub) error { //nolint:revive
	dst := dstRaw.(*v1.ScheduledBackup)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Cluster.Name = src.Spec.Cluster.Name
	dst.Spec.Schedule = src.Spec.Schedule
	dst.Spec.Suspend = src.Spec.Suspend
	dst.Spec.Immediate = src.Spec.Immediate

	// status
	dst.Status.LastCheckTime = src.Status.LastCheckTime
	dst.Status.LastScheduleTime = src.Status.LastScheduleTime
	dst.Status.NextScheduleTime = src.Status.NextScheduleTime

	return nil
}

// ConvertFrom converts from the Hub version (v1) to this version.
func (dst *ScheduledBackup) ConvertFrom(srcRaw conversion.Hub) error { //nolint:revive
	src := srcRaw.(*v1.ScheduledBackup)

	// objectmeta
	dst.ObjectMeta = src.ObjectMeta

	// spec
	dst.Spec.Cluster.Name = src.Spec.Cluster.Name
	dst.Spec.Schedule = src.Spec.Schedule
	dst.Spec.Suspend = src.Spec.Suspend
	dst.Spec.Immediate = src.Spec.Immediate

	// status
	dst.Status.LastCheckTime = src.Status.LastCheckTime
	dst.Status.LastScheduleTime = src.Status.LastScheduleTime
	dst.Status.NextScheduleTime = src.Status.NextScheduleTime

	return nil
}
