package pgbouncer

import (
	"fmt"
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMap creates the ConfigMap containing Odyssey configuration
func ConfigMap(pooler *apiv1.Pooler, cluster *apiv1.Cluster) (*corev1.ConfigMap, error) {
	// Build the cluster connection host name (cluster-rw service DNS name)
	clusterHost := fmt.Sprintf("%s-rw.%s.svc.cluster.local", cluster.Name, cluster.Namespace)

	// Odyssey configuration
	odysseyConfig := fmt.Sprintf(`daemonize no

pid_file "/var/run/odyssey/odyssey.pid"

log_format "%%p %%t %%l [%%i] (%%c) %%m\n"
log_to_stdout yes
log_debug yes
log_config yes
log_session yes
log_query yes
log_stats yes

stats_interval 3

workers 1
resolvers 1

keepalive 7200

listen {
  host "0.0.0.0"
  port 6432
}

storage "default" {
  type "remote"
  host "%s"
  port 5432
}

database "app" {
  user "app" {
    authentication "clear_text"
    password "password"
    storage "default"
    pool "transaction"
  }
}`, clusterHost)

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pooler.Name + "-config",
			Namespace: pooler.Namespace,
			Labels: map[string]string{
				utils.ClusterLabelName:   cluster.Name,
				utils.PgbouncerNameLabel: pooler.Name,
				utils.PodRoleLabelName:   string(utils.PodRolePooler),
				"app":                    "odyssey",
			},
		},
		Data: map[string]string{
			"odyssey.conf": odysseyConfig,
		},
	}, nil
}
