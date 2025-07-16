package controller

import (
	"context"
	"fmt"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/configuration"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/pvcremapper"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"k8s.io/apimachinery/pkg/types"
)

func (r *ClusterReconciler) reconcileRemapping(
	ctx context.Context,
	cluster *apiv1.Cluster,
	resources *managedResources,
	instancesStatus postgres.PostgresqlStatusList,
) error {
	contextLogger := log.FromContext(ctx)
	pvcRemapper, err := pvcremapper.InstancePVCsFromPVCs(resources.pvcs.Items)
	if err != nil {
		contextLogger.Error(err, "Unexpected PVC's linked to this cluster",
			"pvc", resources.pvcs.Items,
		)
		return err
	}
	clusterName := types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace}
	if len(pvcRemapper.RemapRequired()) == 0 {
		//contextLogger.Info("no remapping required", "cluster", clusterName)
		return nil
	}
	switch configuration.Current.AutoVolumeMigration {
	case "auto":
		contextLogger.Info("remapping required, remapping pvc's", "cluster", clusterName)
		return r.autoPVCRemapping(ctx, cluster, instancesStatus, pvcRemapper)
	case "manual":
		contextLogger.Info("manual remapping required", "cluster", clusterName)
		return nil
	case "":
		contextLogger.Info("remapping is required. Please set AUTO_VOLUME_MIGRATION to manual or auto", "cluster", clusterName)
		return utils.ErrTerminateLoop
	default:
		return fmt.Errorf("invalid value for AUTO_VOLUME_MIGRATION (should be manual, or auto, is %s)",
			configuration.Current.AutoVolumeMigration,
		)
	}
}
