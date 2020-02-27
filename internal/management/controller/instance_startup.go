/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package controller

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
)

// VerifyPgDataCoherence check if this cluster exist in k8s and panic if this
// pod belongs to a primary but the cluster status is not coherent with that
func (r *InstanceReconciler) VerifyPgDataCoherence(ctx context.Context) error {
	r.log.Info("Checking PGDATA coherence")

	cluster, err := r.client.
		Resource(apiv1alpha1.ClusterGVK).
		Namespace(r.instance.Namespace).
		Get(r.instance.ClusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	isPrimary, err := r.instance.IsPrimary()
	if err != nil {
		return err
	}

	if isPrimary {
		currentPrimary, err := getCurrentPrimary(cluster)
		if err != nil {
			return err
		}

		targetPrimary, err := getTargetPrimary(cluster)
		if err != nil {
			return err
		}

		isCurrentPrimary := r.instance.PodName == currentPrimary
		isTargetPrimary := r.instance.PodName == targetPrimary

		if !isCurrentPrimary && !isTargetPrimary {
			r.log.Info("Safety measure failed. This PGDATA belongs to "+
				"a primary instance, but this instance is neither primary "+
				"nor target primary",
				"currentPrimary", currentPrimary,
				"targetPrimary", targetPrimary,
				"podName", r.instance.PodName)
			return errors.Errorf("This PGDATA belongs to a primary but " +
				"this instance is neither the current primary nor the target primary. " +
				"Aborting")
		}

		if currentPrimary == "" {
			err = setCurrentPrimary(cluster, r.instance.PodName)
			if err != nil {
				return err
			}

			_, err = r.client.
				Resource(apiv1alpha1.ClusterGVK).
				Namespace(r.instance.Namespace).
				UpdateStatus(cluster, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	return nil
}
