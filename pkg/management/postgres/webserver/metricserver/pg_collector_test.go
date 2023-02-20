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

package metricserver

import (
	"fmt"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/internal/management/cache"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres"
	postgresconf "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ensure timestamp metric it's set properly", func() {
	instance := postgres.NewInstance()
	exporter := NewExporter(instance)

	It("fails if there's no cluster in the cache", func() {
		exporter.collectFromPrimaryFirstPointOnTimeRecovery()
		exporter.collectFromPrimaryLastAvailableBackupTimestamp()
		exporter.collectFromPrimaryLastFailedBackupTimestamp()

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.FirstRecoverabilityPoint)
		registry.MustRegister(exporter.Metrics.LastAvailableBackupTimestamp)
		registry.MustRegister(exporter.Metrics.LastFailedBackupTimestamp)

		metrics, _ := registry.Gather()
		for _, metric := range metrics {
			m := metric.GetMetric()
			Expect(m[0].GetGauge().GetValue()).To(BeEquivalentTo(0))
		}
	})

	It("should set the proper time from the cluster to the metric", func() {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster-example",
			},
			Status: apiv1.ClusterStatus{
				FirstRecoverabilityPoint: "2023-02-16T22:44:56Z",
				LastSuccessfulBackup:     "2023-02-16T22:44:56Z",
				LastFailedBackup:         "2023-02-16T22:44:56Z",
			},
		}
		cache.Store(cache.ClusterKey, cluster)

		exporter.collectFromPrimaryFirstPointOnTimeRecovery()
		exporter.collectFromPrimaryLastAvailableBackupTimestamp()
		exporter.collectFromPrimaryLastFailedBackupTimestamp()

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.FirstRecoverabilityPoint)
		registry.MustRegister(exporter.Metrics.LastAvailableBackupTimestamp)
		registry.MustRegister(exporter.Metrics.LastFailedBackupTimestamp)

		metrics, _ := registry.Gather()
		for _, metric := range metrics {
			m := metric.GetMetric()
			t := time.Unix(int64(m[0].GetGauge().GetValue()), 0).UTC()
			Expect(t.Year()).To(BeEquivalentTo(2023))
			Expect(t.Month()).To(BeEquivalentTo(2))
			Expect(t.Day()).To(BeEquivalentTo(16))
			Expect(t.Hour()).To(BeEquivalentTo(22))
			Expect(t.Minute()).To(BeEquivalentTo(44))
			Expect(t.Second()).To(BeEquivalentTo(56))

		}
	})

	It("It correctly parse the sync replicas", func() {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		rows := sqlmock.NewRows([]string{"synchronous_standby_names"}).
			AddRow("ANY 2 ( \"cluster-example-2\",\"cluster-example-3\")")
		mock.ExpectQuery(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).WillReturnRows(rows)

		exporter.collectFromPrimarySynchronousStandbysNumber(db)

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.SyncReplicas)
		metrics, _ := registry.Gather()

		for _, metric := range metrics {
			m := metric.GetMetric()
			Expect(m[0].GetGauge().GetValue()).To(BeEquivalentTo(2))
		}
	})

	It("register -1 in case it can't parse the sync replicas string", func() {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())

		rows := sqlmock.NewRows([]string{"synchronous_standby_names"}).
			AddRow("( \"cluster-example-2\",\"cluster-example-3\")")
		mock.ExpectQuery(fmt.Sprintf("SHOW %s", postgresconf.SynchronousStandbyNames)).WillReturnRows(rows)

		exporter.collectFromPrimarySynchronousStandbysNumber(db)

		registry := prometheus.NewRegistry()
		registry.MustRegister(exporter.Metrics.SyncReplicas)
		metrics, _ := registry.Gather()

		for _, metric := range metrics {
			m := metric.GetMetric()
			Expect(m[0].GetGauge().GetValue()).To(BeEquivalentTo(-1))
		}
	})
})
