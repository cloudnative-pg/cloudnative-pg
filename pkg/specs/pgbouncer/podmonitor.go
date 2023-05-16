package pgbouncer

import (
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// PoolerPodMonitorManager builds the PodMonitor for the pooler resource
type PoolerPodMonitorManager struct {
	pooler *apiv1.Pooler
}

// NewPoolerPodMonitorManager returns a new instance of PoolerPodMonitorManager
func NewPoolerPodMonitorManager(pooler *apiv1.Pooler) *PoolerPodMonitorManager {
	return &PoolerPodMonitorManager{pooler: pooler}
}

// IsPodMonitorEnabled returns a boolean indicating if the PodMonitor should exists or not
func (c PoolerPodMonitorManager) IsPodMonitorEnabled() bool {
	return c.pooler.Spec.EnablePodMonitor
}

// BuildPodMonitor builds a new PodMonitor object
func (c PoolerPodMonitorManager) BuildPodMonitor() *monitoringv1.PodMonitor {
	meta := metav1.ObjectMeta{
		Namespace: c.pooler.Namespace,
		Name:      c.pooler.Name,
		Labels: map[string]string{
			PgbouncerNameLabel: c.pooler.Name,
		},
	}

	utils.SetAsOwnedBy(&meta, c.pooler.ObjectMeta, c.pooler.TypeMeta)

	spec := monitoringv1.PodMonitorSpec{
		Selector: metav1.LabelSelector{
			MatchLabels: meta.Labels,
		},
		PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
			{
				Port: "metrics",
			},
		},
	}

	return &monitoringv1.PodMonitor{
		ObjectMeta: meta,
		Spec:       spec,
	}
}
