/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/postgres"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/specs"
)

var (
	// ErrorCannotFindPostgresContainer is returned when the rolling update logic cannot find
	// the container to update
	ErrorCannotFindPostgresContainer = errors.New("cannot find 'postgres' container")

	// ErrorInconsistentClusterStatus is raised when the current cluster has no primary nor
	// the sufficient number of nodes to issue a switchover
	ErrorInconsistentClusterStatus = errors.New("inconsistent cluster status")
)

// updateCluster update a Cluster to a new image, if needed
func (r *ClusterReconciler) upgradeCluster(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
	podList v1.PodList, clusterStatus postgres.PostgresqlStatusList,
) error {
	targetImageName := cluster.Spec.ImageName

	// Sort sortedPodList in reverse order
	sortedPodList := podList.Items
	sort.Slice(sortedPodList, func(i, j int) bool {
		return sortedPodList[i].Name > sortedPodList[j].Name
	})

	masterIdx := -1
	for idx, pod := range sortedPodList {
		usedImageName, err := specs.GetPostgreSQLImageName(pod)
		if err != nil {
			r.Log.Error(err,
				"podName", pod.Name,
				"clusterName", cluster.Name,
				"namespace", cluster.Namespace)
			continue
		}

		if usedImageName != targetImageName {
			if cluster.Status.CurrentPrimary == pod.Name {
				// This is the master, and we cannot upgrade it on the fly
				masterIdx = idx
			} else {
				return r.upgradePod(ctx, cluster, &pod)
			}
		}
	}

	if masterIdx == -1 {
		// The master has been updated too, let's declare the
		// rolling update done

		r.Log.Info("Rolling update done",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace,
			"from", cluster.Status.ImageName,
			"to", cluster.Spec.ImageName)
		cluster.Status.ImageName = cluster.Spec.ImageName
		if err := r.Status().Update(ctx, cluster); !apierrors.IsConflict(err) {
			return err
		}

		return nil
	}

	// We still need to upgrade the master server, let's see
	// if the user prefer to do it manually
	if cluster.GetMasterUpdateStrategy() == v1alpha1.MasterUpdateStrategyWait {
		r.Log.Info("Waiting for the user to issue a switchover to complete the rolling update",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace,
			"masterPod", sortedPodList[masterIdx].Name)
		return nil
	}

	// Ok, the user wants us to automatically update all
	// the server, so let's switch over
	if len(clusterStatus.Items) < 2 || clusterStatus.Items[1].IsPrimary {
		return ErrorInconsistentClusterStatus
	}

	// Let's switch over to this server
	r.Log.Info("Switching over to a replica to complete the rolling update",
		"clusterName", cluster.Name,
		"namespace", cluster.Namespace,
		"oldPrimary", cluster.Status.TargetPrimary,
		"newPrimary", clusterStatus.Items[1].PodName,
		"status", clusterStatus)
	return r.setPrimaryInstance(ctx, cluster, clusterStatus.Items[1].PodName)
}

// updatePod update an instance to a newer image version
func (r *ClusterReconciler) upgradePod(ctx context.Context, cluster *v1alpha1.Cluster, pod *v1.Pod) error {
	patch := client.MergeFrom(pod.DeepCopy())

	oldImage := ""
	for idx := len(pod.Spec.Containers) - 1; idx >= 0; idx-- {
		if pod.Spec.Containers[idx].Name == specs.PostgresContainerName {
			oldImage = pod.Spec.Containers[idx].Image
			pod.Spec.Containers[idx].Image = cluster.Spec.ImageName
			break
		}
	}

	if oldImage == "" {
		r.Log.Info("Cannot find PostgreSQL container on pod",
			"clusterName", cluster.Name,
			"namespace", cluster.Namespace,
			"podName", pod.Name)
		return ErrorCannotFindPostgresContainer
	}

	currentTime := time.Now()

	// Upgrading Pod
	r.Log.Info("Upgrading Pod",
		"clusterName", cluster.Name,
		"podName", pod.Name,
		"namespace", cluster.Namespace,
		"to", cluster.Spec.ImageName,
		"from", oldImage)

	err := r.Patch(ctx, pod, patch)
	if err != nil {
		return err
	}

	// The following operation is really important to distinguish
	// between instances waiting an upgrade and the other ones. This
	// is the reason why in this case it's retried without waiting
	// for another reconciliation loop.
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var updatedCluster v1alpha1.Cluster

		// Get the latest version of the cluster
		err := r.Get(ctx, client.ObjectKey{
			Name:      cluster.Name,
			Namespace: cluster.Namespace,
		}, &updatedCluster)
		if err != nil {
			return err
		}

		// Record current time and operation into
		// the cluster status
		if cluster.Status.RollingUpdateStatus == nil {
			cluster.Status.RollingUpdateStatus = make(map[string]v1alpha1.RollingUpdateStatus)
		}

		delete(cluster.Status.RollingUpdateStatus, pod.Name)
		cluster.Status.RollingUpdateStatus[pod.Name] = v1alpha1.RollingUpdateStatus{
			ImageName: cluster.Spec.ImageName,
			StartedAt: metav1.Time{Time: currentTime},
		}

		// Update the cluster status to remember that
		// we are upgrading a certain instance
		return r.Status().Update(ctx, cluster)
	})
	if err != nil {
		err = fmt.Errorf("marking pod as upgraded: %v", err)
	}
	return err
}
