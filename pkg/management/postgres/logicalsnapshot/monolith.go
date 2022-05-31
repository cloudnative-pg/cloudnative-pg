package logicalsnapshot

import (
	"context"

	v1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/pool"
)

// Monolith executes the monolith clone type
func Monolith(
	ctx context.Context,
	cluster *v1.Cluster,
	destination *pool.ConnectionPool,
	origin *pool.ConnectionPool,
) error {
	contextLogger := log.FromContext(ctx)
	contextLogger.Info("starting monolith clone process")

	if err := cloneRoles(ctx, cluster, destination, origin); err != nil {
		return err
	}
	if err := cloneRoleInheritance(ctx, destination, origin); err != nil {
		return err
	}

	ds := databaseSnapshotter{cluster: cluster}
	databases, err := ds.getDatabaseList(ctx, origin)
	if err != nil {
		return err
	}
	if err := ds.exportDatabases(ctx, origin, databases); err != nil {
		return err
	}
	if err := ds.importDatabases(ctx, destination, databases); err != nil {
		return err
	}
	if err := ds.analyze(ctx, destination, databases); err != nil {
		return err
	}

	return nil
}
