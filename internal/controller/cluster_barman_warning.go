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

package controller

import (
	"context"
	"slices"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

const (
	// nativeBarmanCloudWarningInterval is how often the leader repeats the warning
	nativeBarmanCloudWarningInterval = 24 * time.Hour

	// nativeBarmanCloudEventInterval paces the events, as the event broadcaster
	// drops the ones exceeding its queue of 1000, including the reconcilers' ones
	nativeBarmanCloudEventInterval = 100 * time.Millisecond

	// nativeBarmanCloudClusterKey is a synthetic index key, not a real Cluster spec field;
	// it is populated by nativeBarmanCloudIndexFunc.
	nativeBarmanCloudClusterKey = ".spec.nativeBarmanCloud"
)

// nativeBarmanCloudIndexFunc indexes the Clusters that set
// spec.backup.barmanObjectStore or an external cluster barmanObjectStore
func nativeBarmanCloudIndexFunc(rawObj client.Object) []string {
	cluster := rawObj.(*apiv1.Cluster)
	if cluster.Spec.Backup != nil && cluster.Spec.Backup.BarmanObjectStore != nil ||
		slices.ContainsFunc(cluster.Spec.ExternalClusters, func(externalCluster apiv1.ExternalCluster) bool {
			return externalCluster.BarmanObjectStore != nil
		}) {
		return []string{"true"}
	}

	return nil
}

// NewNativeBarmanCloudWarning returns a Runnable that warns about every Cluster
// using the native Barman Cloud support, which is removed in CloudNativePG
// 1.32.0. The manager starts it each time this operator becomes the leader,
// and it repeats the warning every 24 hours, so that clusters which are never
// re-applied get the warning too.
func NewNativeBarmanCloudWarning(mgr ctrl.Manager) manager.Runnable {
	recorder := mgr.GetEventRecorderFor("cloudnative-pg") //nolint:staticcheck
	return manager.RunnableFunc(func(ctx context.Context) error {
		ticker := time.NewTicker(nativeBarmanCloudWarningInterval)
		defer ticker.Stop()

		for {
			warnNativeBarmanCloudClusters(ctx, mgr.GetClient(), recorder)
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
	})
}

// warnNativeBarmanCloudClusters logs a warning and records an event, one every
// nativeBarmanCloudEventInterval, for every Cluster that sets
// spec.backup.barmanObjectStore or an external cluster barmanObjectStore.
// A failure is only logged, as the warning is best-effort.
func warnNativeBarmanCloudClusters(ctx context.Context, cli client.Client, recorder record.EventRecorder) {
	const message = "Native Barman Cloud support is deprecated and will be removed in " +
		"CloudNativePG 1.32.0. Please migrate to the Barman Cloud Plugin."
	contextLogger := log.FromContext(ctx)

	var clusters apiv1.ClusterList
	if err := cli.List(ctx, &clusters, client.MatchingFields{nativeBarmanCloudClusterKey: "true"}); err != nil {
		contextLogger.Error(err, "unable to list the clusters using the native Barman Cloud support")
		return
	}

	pace := time.NewTicker(nativeBarmanCloudEventInterval)
	defer pace.Stop()

	for i := range clusters.Items {
		select {
		case <-ctx.Done():
			return
		case <-pace.C:
		}

		cluster := &clusters.Items[i]
		contextLogger.Warning(message, "namespace", cluster.Namespace, "name", cluster.Name)
		recorder.Event(cluster, "Warning", "NativeBarmanCloudDeprecated", message)
	}
}
