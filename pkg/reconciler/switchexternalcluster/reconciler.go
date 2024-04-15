package switchexternalcluster

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/switchexternalcluster/common"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// Reconcile reconciles the cluster hibernation status.
func Reconcile(
	ctx context.Context,
	cli client.Client,
	cluster *apiv1.Cluster,
	instances postgres.PostgresqlStatusList,
) (*ctrl.Result, error) {
	if !cluster.IsReplica() {
		return nil, nil
	}

	contextLogger := log.FromContext(ctx)
	// finish transition
	if common.IsDesignatedPrimaryTransitionCompleted(cluster) {
		return &ctrl.Result{RequeueAfter: time.Second}, finishTransition(ctx, cli, cluster)
	}
	// waiting for the instance manager
	if common.IsDesignatedPrimaryTransitionRequested(cluster) {
		contextLogger.Info("waiting transition")
		return nil, nil
	}

	var hasPrimary bool
	for _, item := range instances.Items {
		if item.IsPrimary {
			hasPrimary = true
		}
	}
	// cluster is not in a reliable state or has already transitioned
	if !hasPrimary {
		return nil, nil
	}

	return startTransition(ctx, cli, cluster)
}

func startTransition(ctx context.Context, cli client.Client, cluster *apiv1.Cluster) (*ctrl.Result, error) {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("starting transition")
	err := utils.NewFencingMetadataExecutor(cli).AddFencing().ForAllInstances().Execute(
		ctx,
		client.ObjectKeyFromObject(cluster),
		cluster,
	)

	origCluster := cluster.DeepCopy()
	// if nothing is present this is the starting phase
	common.SetDesignatedPrimaryTransitionRequested(cluster)
	common.SetFenceRequest(cluster)

	if err := cli.Status().Patch(ctx, cluster, client.MergeFrom(origCluster)); err != nil {
		return nil, err
	}

	return &ctrl.Result{RequeueAfter: time.Second}, err
}

func finishTransition(ctx context.Context, cli client.Client, cluster *apiv1.Cluster) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("finishing transition")
	if meta.IsStatusConditionPresentAndEqual(cluster.Status.Conditions, common.ConditionFence, metav1.ConditionTrue) &&
		cluster.IsInstanceFenced("*") {
		if err := utils.NewFencingMetadataExecutor(cli).RemoveFencing().ForAllInstances().Execute(
			ctx,
			client.ObjectKeyFromObject(cluster),
			cluster,
		); err != nil {
			return err
		}
	}
	origCluster := cluster.DeepCopy()
	meta.RemoveStatusCondition(&cluster.Status.Conditions, common.ConditionDesignatedPrimaryTransition)
	meta.RemoveStatusCondition(&cluster.Status.Conditions, common.ConditionFence)

	return cli.Status().Patch(ctx, cluster, client.MergeFrom(origCluster))
}
