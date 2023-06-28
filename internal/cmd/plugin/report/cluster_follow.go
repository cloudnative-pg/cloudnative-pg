package report

import (
	"context"
	"fmt"
	"os"
	"time"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils/logs"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// cluster implements the "report cluster" subcommand
// Produces a zip file containing
//   - cluster pod and job definitions
//   - cluster resource (same content as `kubectl get cluster -o yaml`)
//   - events in the cluster namespace
//   - logs from the cluster pods (optional - activated with `includeLogs`)
//   - logs from the cluster jobs (optional - activated with `includeLogs`)
func followCluster(ctx context.Context, clusterName, namespace string, format plugin.OutputFormat,
	file string, includeLogs, logTimeStamp bool, timestamp time.Time,
) error {
	var cluster cnpgv1.Cluster
	err := plugin.Client.Get(ctx,
		types.NamespacedName{Namespace: namespace, Name: clusterName},
		&cluster)
	if err != nil {
		return fmt.Errorf("could not get cluster: %w", err)
	}

	now := metav1.Now()
	streamClusterLogs := logs.ClusterStreamingRequest{
		Cluster: cluster,
		Options: &v1.PodLogOptions{
			Timestamps: logTimeStamp,
			Follow:     true,
			SinceTime:  &now,
		},
	}
	return streamClusterLogs.Stream(ctx, os.Stdout)
}
