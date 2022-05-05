package controller

import (
	"context"
	"errors"
	"os"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/manager/walrestore"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	barmanCredentials "github.com/cloudnative-pg/cloudnative-pg/pkg/management/barman/credentials"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// reconcileCacheFromCluster refreshes the reconciler internal cache using the provided cluster
func (r *InstanceReconciler) reconcileCacheFromCluster(ctx context.Context, cluster *apiv1.Cluster) *ctrl.Result {
	cache.Store(cache.ClusterKey, cluster)

	// Populate the cache with the backup configuration
	if result := r.reconcileWALArchiveSettingsCache(ctx, cluster); result != nil {
		return result
	}

	// Populate the cache with the recover configuration
	return r.reconcileWALRestoreSettingsCache(ctx, cluster)
}

func (r *InstanceReconciler) reconcileWALRestoreSettingsCache(
	ctx context.Context,
	cluster *apiv1.Cluster,
) *ctrl.Result {
	_, env, barmanConfiguration, err := walrestore.GetRecoverConfiguration(cluster, r.instance.PodName)
	if errors.Is(err, walrestore.ErrNoBackupConfigured) {
		cache.Delete(cache.WALRestoreKey)
		return nil
	}
	if err != nil {
		log.Error(err, "while getting recover configuration")
		return nil
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

	return nil
}

func (r *InstanceReconciler) reconcileWALArchiveSettingsCache(
	ctx context.Context,
	cluster *apiv1.Cluster,
) *ctrl.Result {
	if cluster.Spec.Backup == nil || cluster.Spec.Backup.BarmanObjectStore == nil {
		cache.Delete(cache.WALArchiveKey)
		return nil
	}

	// Populate the cache with the backup configuration
	envArchive, err := barmanCredentials.EnvSetBackupCloudCredentials(
		ctx,
		r.GetClient(),
		cluster.Namespace,
		cluster.Spec.Backup.BarmanObjectStore,
		os.Environ())
	if apierrors.IsForbidden(err) {
		log.Info("backup secret not yet ready, running another reconciliation loop")
		return &ctrl.Result{RequeueAfter: 5 * time.Second}
	}

	if err != nil {
		log.Error(err, "while getting backup credentials")
		return nil
	}

	cache.Store(cache.WALArchiveKey, envArchive)
	return nil
}
