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

// Package pgcreatesubscriber implements the pg_createsubscriber bootstrap
// method: a physical clone of an external server converted into a primary
// that keeps following the source through logical replication
package pgcreatesubscriber

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/log"
	"github.com/spf13/cobra"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/istio"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/linkerd"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/configfile"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/external"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/system"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// standbyStreamingTimeout bounds the wait for the cloned standby to stream
// from the source before the conversion creates anything on it
const standbyStreamingTimeout = 5 * time.Minute

// converter holds the state of the bootstrap job
type converter struct {
	info   *postgres.InitInfo
	client ctrl.Client
}

// NewCmd creates the "pgcreatesubscriber" subcommand
func NewCmd() *cobra.Command {
	var clusterName string
	var namespace string
	var pgData string
	var pgWal string

	cmd := &cobra.Command{
		Use: "pgcreatesubscriber",
		PreRunE: func(cmd *cobra.Command, _ []string) error {
			return management.WaitForGetCluster(cmd.Context(), ctrl.ObjectKey{
				Name:      clusterName,
				Namespace: namespace,
			})
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			client, err := management.NewControllerRuntimeClient()
			if err != nil {
				return err
			}

			env := converter{
				info: &postgres.InitInfo{
					ClusterName: clusterName,
					Namespace:   namespace,
					PgData:      pgData,
					PgWal:       pgWal,
				},
				client: client,
			}
			if err := env.bootstrap(ctx); err != nil {
				log.FromContext(ctx).Error(err, "Unable to bootstrap cluster via pg_createsubscriber")
				return err
			}
			return nil
		},
		PostRunE: func(cmd *cobra.Command, _ []string) error {
			if err := istio.TryInvokeQuitEndpoint(cmd.Context()); err != nil {
				return err
			}

			return linkerd.TryInvokeShutdownEndpoint(cmd.Context())
		},
	}

	cmd.Flags().StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	cmd.Flags().StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	cmd.Flags().StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	cmd.Flags().StringVar(&pgWal, "pg-wal", "", "the PGWAL to be created")

	return cmd
}

func (env *converter) bootstrap(ctx context.Context) error {
	contextLogger := log.FromContext(ctx)

	var cluster apiv1.Cluster
	if err := env.client.Get(ctx,
		ctrl.ObjectKey{Namespace: env.info.Namespace, Name: env.info.ClusterName}, &cluster); err != nil {
		return err
	}
	if err := system.SetCoredumpFilter(cluster.GetCoredumpFilter()); err != nil {
		return err
	}

	// A0: a retried job must not convert an already converted primary again.
	// This check comes before EnsureTargetDirectoriesDoNotExist, which would
	// move the converted PGDATA away.
	marker, err := postgres.ReadCreateSubscriberMarker(env.info.PgData)
	if err != nil {
		return err
	}
	if marker != nil {
		contextLogger.Info("PGDATA already converted, only handing it over", "marker", marker)
		return env.handOver(ctx, &cluster)
	}

	plan, cloneConnString, err := env.buildPlan(ctx, &cluster)
	if err != nil {
		return err
	}
	major, err := cluster.GetPostgresqlMajorVersion()
	if err != nil {
		return err
	}

	// A1: drop what a previous attempt left on the source
	if err := plan.CleanupSourceObjects(ctx); err != nil {
		return fmt.Errorf("while cleaning up the source: %w", err)
	}

	// A2
	if err := env.sourcePreflight(ctx, plan, major); err != nil {
		return err
	}

	// A3
	if err := env.info.EnsureTargetDirectoriesDoNotExist(ctx); err != nil {
		return err
	}

	// B1: physical clone keeping the WAL on the source through a slot
	if err := postgres.ClonePgDataWithSlot(
		ctx, cloneConnString, env.info.PgData, env.info.PgWal, plan.PhysicalSlot); err != nil {
		return fmt.Errorf("while cloning pgdata: %w", err)
	}
	checkFile := filepath.Join(env.info.PgData, constants.CheckEmptyWalArchiveFile)
	if err := fileutils.CreateEmptyFile(checkFile); err != nil {
		return fmt.Errorf("could not create %v file: %w", checkFile, err)
	}

	// B2: CNPG configuration, local-only access, standby of the source
	if err := env.info.WriteInitialPostgresqlConf(ctx, &cluster); err != nil {
		return err
	}
	if err := env.info.WriteRestoreHbaConf(ctx); err != nil {
		return err
	}
	if _, err := postgres.UpdateReplicaConfiguration(
		env.info.PgData, plan.SourceConnString(plan.Databases[0].Name), plan.PhysicalSlot); err != nil {
		return err
	}

	sourceID, err := plan.SourceSystemIdentifier(ctx)
	if err != nil {
		return err
	}

	// C
	marker = &postgres.CreateSubscriberMarker{
		Engine:                 plan.Engine,
		Major:                  major,
		SourceSystemIdentifier: sourceID,
		PhysicalSlot:           plan.PhysicalSlot,
		Databases:              plan.Databases,
	}
	switch plan.Engine {
	case postgres.CreateSubscriberEngineNative:
		err = env.convertNative(ctx, &cluster, plan, marker)
	default:
		err = env.convertEmulated(ctx, &cluster, plan, marker)
	}
	if err != nil {
		return err
	}

	// D1: nothing may keep following the source physically
	for _, file := range []string{"postgresql.auto.conf", constants.PostgresqlOverrideConfigurationFile} {
		if err := postgres.RemoveRecoverySettings(filepath.Join(env.info.PgData, file)); err != nil {
			return fmt.Errorf("while cleaning %s: %w", file, err)
		}
	}

	controlFile, err := postgres.ReadControlFile(env.info.PgData)
	if err != nil {
		return err
	}
	marker.SystemIdentifier = strconv.FormatUint(controlFile.SystemIdentifier(), 10)
	if marker.SystemIdentifier == sourceID {
		return fmt.Errorf("the system identifier is still the one of the source")
	}

	// D2
	marker.CompletedAt = time.Now().UTC()
	if err := postgres.WriteCreateSubscriberMarker(env.info.PgData, *marker); err != nil {
		return err
	}
	contextLogger.Info("Conversion into a logical subscriber completed", "marker", marker)

	// D3
	return env.handOver(ctx, &cluster)
}

// buildPlan resolves the source connection, the engine and the databases.
// It returns the plan and the connection string used by pg_basebackup.
func (env *converter) buildPlan(
	ctx context.Context,
	cluster *apiv1.Cluster,
) (*postgres.CreateSubscriberPlan, string, error) {
	spec := cluster.Spec.Bootstrap.PgCreateSubscriber
	server, ok := cluster.ExternalCluster(spec.Source)
	if !ok {
		return nil, "", fmt.Errorf("missing external cluster %q", spec.Source)
	}

	// Dumps the password into the passfile that the connection strings refer to
	baseConnString, err := external.ConfigureConnectionToServer(ctx, env.client, env.info.Namespace, &server)
	if err != nil {
		return nil, "", err
	}

	engine, err := chooseEngine(cluster)
	if err != nil {
		return nil, "", err
	}

	plan := &postgres.CreateSubscriberPlan{
		Engine:       engine,
		PhysicalSlot: cluster.ComputeSubscriberPhysicalSlotName(),
		ObjectPrefix: cluster.ComputeSubscriberObjectName(""),
		SourceConnString: func(database string) string {
			connString, _ := external.GetServerConnectionString(&server, database)
			return connString
		},
	}
	if spec.Parameters != nil && spec.Parameters.RecoveryTimeout != nil {
		plan.RecoveryTimeout = time.Duration(*spec.Parameters.RecoveryTimeout) * time.Second
	}

	names, err := cluster.GetPgCreateSubscriberDatabases()
	if err != nil {
		return nil, "", err
	}
	adminDatabase := server.ConnectionParameters["dbname"]
	if adminDatabase == "" {
		adminDatabase = "postgres"
	}
	admin, err := plan.OpenSource(adminDatabase)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		_ = admin.Close()
	}()
	if err := plan.ResolveSourceDatabases(ctx, admin, names); err != nil {
		return nil, "", err
	}
	if len(plan.Databases) == 0 {
		return nil, "", fmt.Errorf("no database to convert on the source")
	}

	// A slow source must not make the walsender drop the clone
	cloneConnString := baseConnString + " options='-c wal_sender_timeout=0s'"
	return plan, cloneConnString, nil
}

func (env *converter) sourcePreflight(ctx context.Context, plan *postgres.CreateSubscriberPlan, major int) error {
	admin, err := plan.OpenSource(plan.Databases[0].Name)
	if err != nil {
		return err
	}
	defer func() {
		_ = admin.Close()
	}()
	return plan.SourcePreflight(ctx, admin, major)
}

// chooseEngine returns the native engine on PostgreSQL 17 and later, and the
// emulated one on PostgreSQL 14. The emulated engine can be forced on 17+
// through an annotation, to compare it with the native one.
func chooseEngine(cluster *apiv1.Cluster) (string, error) {
	major, err := cluster.GetPostgresqlMajorVersion()
	if err != nil {
		return "", err
	}
	forced := cluster.Annotations[utils.PgCreateSubscriberEngineAnnotationName]
	switch {
	case major == 14:
		return postgres.CreateSubscriberEngineEmulated, nil
	case major >= 17 && forced == postgres.CreateSubscriberEngineEmulated:
		return postgres.CreateSubscriberEngineEmulated, nil
	case major >= 17:
		return postgres.CreateSubscriberEngineNative, nil
	default:
		return "", fmt.Errorf("pg_createsubscriber bootstrap does not support PostgreSQL %d", major)
	}
}

// convertEmulated reproduces the algorithm of pg_createsubscriber
// (REL_17_STABLE) for majors that don't ship the binary
func (env *converter) convertEmulated(
	ctx context.Context,
	cluster *apiv1.Cluster,
	plan *postgres.CreateSubscriberPlan,
	marker *postgres.CreateSubscriberMarker,
) error {
	contextLogger := log.FromContext(ctx)

	// C-E 1: the standby must be consistent and streaming from the very
	// server we are about to create slots on
	standby := env.temporaryInstance(cluster)
	if err := standby.WithActiveInstance(func() error {
		db, err := standby.GetSuperUserDB()
		if err != nil {
			return err
		}
		if err := postgres.WaitForStandbyStreaming(ctx, db, standbyStreamingTimeout); err != nil {
			return err
		}
		targetID, err := postgres.SystemIdentifier(ctx, db)
		if err != nil {
			return err
		}
		if targetID != marker.SourceSystemIdentifier {
			return fmt.Errorf("the clone has system identifier %s, the source %s",
				targetID, marker.SourceSystemIdentifier)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("while checking the standby: %w", err)
	}

	// C-E 2: the standby is stopped, so it can't replay past the consistent point
	consistentLSN, err := plan.CreatePublicationsAndSlots(ctx)
	if err != nil {
		return err
	}
	marker.ConsistentLSN = consistentLSN
	contextLogger.Info("Consistent point on the source", "lsn", consistentLSN)

	// C-E 3
	if _, err := configfile.UpdatePostgresConfigurationFile(
		filepath.Join(env.info.PgData, constants.PostgresqlOverrideConfigurationFile),
		map[string]string{
			"recovery_target_lsn":       consistentLSN,
			"recovery_target_inclusive": "true",
			"recovery_target_action":    "promote",
			"recovery_target_timeline":  "latest",
		}); err != nil {
		return fmt.Errorf("while writing the recovery target: %w", err)
	}

	// C-E 4 and 5: replay up to the consistent point, promote, then subscribe
	target := env.temporaryInstance(cluster)
	if err := target.WithActiveInstance(func() error {
		db, err := target.GetSuperUserDB()
		if err != nil {
			return err
		}
		if err := postgres.WaitForPromotion(ctx, db, plan.RecoveryTimeout); err != nil {
			return err
		}

		// Recovery stops after the first record starting at or beyond the
		// consistent point, which may be a commit: start the subscriptions where
		// replay really ended, so that no transaction is applied twice
		promotedLSN, err := postgres.PromotionSwitchPoint(ctx, db, env.info.PgData)
		if err != nil {
			return err
		}
		if before, err := postgres.LSNGreater(consistentLSN, promotedLSN); err != nil || before {
			return fmt.Errorf("the target promoted at %s, before the consistent point %s (%v)",
				promotedLSN, consistentLSN, err)
		}
		marker.PromotedLSN = promotedLSN
		contextLogger.Info("Target promoted", "consistentLSN", consistentLSN, "promotedLSN", promotedLSN)

		if err := plan.DropInheritedLogicalObjects(ctx, target); err != nil {
			return err
		}
		if err := plan.CreateSubscriptions(ctx, target, promotedLSN); err != nil {
			return err
		}
		return plan.VerifySubscriptions(ctx, target)
	}); err != nil {
		return fmt.Errorf("while converting the target: %w", err)
	}

	// C-E 6: the physical slot is not needed anymore. Replication slots are
	// never part of a base backup, so there is nothing else to drop on the target.
	if err := plan.DropPhysicalSlot(ctx); err != nil {
		return fmt.Errorf("while dropping the physical slot on the source: %w", err)
	}

	// C-E 7: the target must not look like the source anymore
	newID := postgres.NewSystemIdentifier(time.Now(), os.Getpid())
	if err := postgres.ChangeSystemIdentifier(ctx, env.info.PgData, newID); err != nil {
		return fmt.Errorf("while changing the system identifier: %w", err)
	}
	return nil
}

// convertNative runs pg_createsubscriber, then removes the logical objects the
// target inherited from the source
func (env *converter) convertNative(
	ctx context.Context,
	cluster *apiv1.Cluster,
	plan *postgres.CreateSubscriberPlan,
	_ *postgres.CreateSubscriberMarker,
) error {
	if err := plan.RunPgCreateSubscriber(ctx, env.info.PgData, postgres.GetSocketDir()); err != nil {
		return fmt.Errorf("pg_createsubscriber failed: %w", err)
	}

	target := env.temporaryInstance(cluster)
	if err := target.WithActiveInstance(func() error {
		if err := plan.DropInheritedLogicalObjects(ctx, target); err != nil {
			return err
		}
		return plan.VerifySubscriptions(ctx, target)
	}); err != nil {
		return err
	}

	// pg_createsubscriber drops primary_slot_name itself: this only covers a
	// retry after a crash between the two
	return plan.DropPhysicalSlot(ctx)
}

// temporaryInstance returns an instance that doesn't start logical
// replication workers: subscriptions inherited from the source must not run
func (env *converter) temporaryInstance(cluster *apiv1.Cluster) *postgres.Instance {
	instance := env.info.GetInstance(cluster)
	instance.StartupOptions = append(instance.StartupOptions, "max_logical_replication_workers=0")
	return instance
}

// handOver restarts the converted instance with the CNPG configuration, the
// same way the other bootstrap methods finish
func (env *converter) handOver(ctx context.Context, cluster *apiv1.Cluster) error {
	// The subscriptions authenticate through the passfile of the source: a
	// retried job starts from an empty filesystem and must write it again
	server, ok := cluster.ExternalCluster(cluster.Spec.Bootstrap.PgCreateSubscriber.Source)
	if !ok {
		return fmt.Errorf("missing external cluster %q", cluster.Spec.Bootstrap.PgCreateSubscriber.Source)
	}
	if _, err := external.ConfigureConnectionToServer(ctx, env.client, env.info.Namespace, &server); err != nil {
		return err
	}
	return env.info.ConfigureInstanceAfterRestore(ctx, cluster, nil)
}
