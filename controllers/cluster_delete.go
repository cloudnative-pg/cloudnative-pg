/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package controllers

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/EnterpriseDB/cloud-native-postgresql/api/v1"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/configuration"
)

// deleteDanglingMonitoringConfigMaps deletes the default monitoring configMap if no cluster in the namespace
// is using it.
func (r *ClusterReconciler) deleteDanglingMonitoringConfigMaps(ctx context.Context, namespace string) error {
	configMapName := configuration.Current.MonitoringQueriesConfigmap
	if configMapName == "" {
		// no configmap configured, we can exit.
		return nil
	}

	clustersUsingConfigMap := apiv1.ClusterList{}
	err := r.List(
		ctx,
		&clustersUsingConfigMap,
		client.InNamespace(namespace),
		// we check if there are any clusters that use the configMap in the namespace
		client.MatchingFields{disableDefaultQueriesSpecPath: "false"},
	)
	if err != nil {
		return err
	}

	configMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
	}
	if len(clustersUsingConfigMap.Items) == 0 {
		return r.Delete(ctx, &configMap)
	}

	return nil
}
