/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"os"

	barmanCredentials "github.com/cloudnative-pg/barman-cloud/pkg/credentials"
	"github.com/cloudnative-pg/machinery/pkg/log"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walrestore"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
)

// updateCacheFromCluster will update the internal cache with the cluster
//
// returns true if the update was not total, and should be retried
func (r *InstanceReconciler) updateCacheFromCluster(ctx context.Context, cluster *apiv1.Cluster) shoudRequeue {
	var missingPermissions shoudRequeue

	// Populate the cache with the backup configuration
	if r.shouldUpdateWALArchiveSettingsCache(ctx, cluster) {
		missingPermissions = true
	}

	// Populate the cache with the recover configuration
	r.updateWALRestoreSettingsCache(ctx, cluster)
	return missingPermissions
}

func (r *InstanceReconciler) updateWALRestoreSettingsCache(ctx context.Context, cluster *apiv1.Cluster) {
	_, env, barmanConfiguration, err := walrestore.GetRecoverConfiguration(cluster, r.instance.GetPodName())
	if errors.Is(err, walrestore.ErrNoBackupConfigured) {
		cache.Delete(cache.WALRestoreKey)
		return
	}
	if err != nil {
		log.Error(err, "while getting recover configuration")
		return
	}
	env = append(env, os.Environ()...)

	envRestore, err := barmanCredentials.EnvSetBackupCloudCredentials(
		ctx,
		r.GetClient(),
		cluster.Namespace,
		barmanConfiguration,
		env,
	)
	if err != nil {
		log.Error(err, "while getting recover credentials")
	}
	cache.Store(cache.WALRestoreKey, envRestore)
}

// shouldUpdateWALArchiveSettingsCache updates the cache with the backup credentials
//
// returns true if and only if the update should run again, because:
// the backup credentials exist but don't have permission,
func (r *InstanceReconciler) shouldUpdateWALArchiveSettingsCache(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (shouldRetry bool) {
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		cache.Delete(cache.WALArchiveKey)
		return false
	}

	// Populate the cache with the backup configuration
	envArchive, err := barmanCredentials.EnvSetBackupCloudCredentials(
		ctx,
		r.GetClient(),
		cluster.Namespace,
		cluster.Spec.Backup.BarmanObjectStore,
		os.Environ())
	if apierrors.IsForbidden(err) {
		log.Info("backup credentials don't yet have access permissions. Will retry reconciliation loop")
		return true
	}

	if err != nil {
		log.Error(err, "while getting backup credentials")
		return false
	}

	cache.Store(cache.WALArchiveKey, envArchive)
	return false
}
