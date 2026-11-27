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

package controller

import (
	"database/sql"
	"fmt"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("subscription sql", func() {
	const defaultPostgresMajorVersion = 17

	var (
		dbMock sqlmock.Sqlmock
		db     *sql.DB
	)

	BeforeEach(func() {
		var err error
		db, dbMock, err = sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		Expect(dbMock.ExpectationsWereMet()).To(Succeed())
	})

	It("drops the subscription successfully", func(ctx SpecContext) {
		dbMock.ExpectExec(fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", pgx.Identifier{"subscription_name"}.Sanitize())).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := executeDropSubscription(ctx, db, "subscription_name")
		Expect(err).ToNot(HaveOccurred())
	})

	It("returns an error when dropping the subscription fails", func(ctx SpecContext) {
		dbMock.ExpectExec(fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", pgx.Identifier{"subscription_name"}.Sanitize())).
			WillReturnError(fmt.Errorf("drop subscription error"))

		err := executeDropSubscription(ctx, db, "subscription_name")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("while dropping subscription: drop subscription error"))
	})

	It("sanitizes the subscription name correctly", func(ctx SpecContext) {
		dbMock.ExpectExec(fmt.Sprintf("DROP SUBSCRIPTION IF EXISTS %s", pgx.Identifier{"sanitized_name"}.Sanitize())).
			WillReturnResult(sqlmock.NewResult(1, 1))

		err := executeDropSubscription(ctx, db, "sanitized_name")
		Expect(err).ToNot(HaveOccurred())
	})

	It("generates correct SQL for creating subscription with publication and connection string", func() {
		obj := &apiv1.Subscription{
			Spec: apiv1.SubscriptionSpec{
				Name:            "test_sub",
				PublicationName: "test_pub",
			},
		}
		connString := "host=localhost user=test dbname=test"

		sql := toSubscriptionCreateSQL(obj, connString)
		Expect(sql).To(Equal(
			`CREATE SUBSCRIPTION "test_sub" CONNECTION 'host=localhost user=test dbname=test' PUBLICATION "test_pub"`))
	})

	It("generates correct SQL for creating subscription with parameters", func() {
		obj := &apiv1.Subscription{
			Spec: apiv1.SubscriptionSpec{
				Name:            "test_sub",
				PublicationName: "test_pub",
				Parameters: map[string]string{
					"param1": "value1",
					"param2": "value2",
				},
			},
		}
		connString := "host=localhost user=test dbname=test"

		sql := toSubscriptionCreateSQL(obj, connString)
		expectedElement := `CREATE SUBSCRIPTION "test_sub" ` +
			`CONNECTION 'host=localhost user=test dbname=test' ` +
			`PUBLICATION "test_pub" WITH ("param1" = 'value1', "param2" = 'value2')`
		Expect(sql).To(Equal(expectedElement))
	})

	It("returns correct SQL for creating subscription with no owner or parameters", func() {
		obj := &apiv1.Subscription{
			Spec: apiv1.SubscriptionSpec{
				Name:            "test_sub",
				PublicationName: "test_pub",
			},
		}
		connString := "host=localhost user=test dbname=test"

		sql := toSubscriptionCreateSQL(obj, connString)
		Expect(sql).To(Equal(
			`CREATE SUBSCRIPTION "test_sub" CONNECTION 'host=localhost user=test dbname=test' PUBLICATION "test_pub"`))
	})

	It("generates correct SQL for altering subscription with publication and connection string", func() {
		obj := &apiv1.Subscription{
			Spec: apiv1.SubscriptionSpec{
				Name:            "test_sub",
				PublicationName: "test_pub",
			},
		}
		connString := "host=localhost user=test dbname=test"

		sqls := toSubscriptionAlterSQL(obj, connString, defaultPostgresMajorVersion, nil)
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET PUBLICATION "test_pub"`))
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" CONNECTION 'host=localhost user=test dbname=test'`))
	})

	It("generates correct SQL for altering subscription with parameters for PostgreSQL 17", func() {
		obj := &apiv1.Subscription{
			Spec: apiv1.SubscriptionSpec{
				Name:            "test_sub",
				PublicationName: "test_pub",
				Parameters: map[string]string{
					"copy_data": "true",
					"origin":    "none",
					"failover":  "true",
					"two_phase": "true",
				},
			},
		}
		connString := "host=localhost user=test dbname=test"

		sqls := toSubscriptionAlterSQL(obj, connString, 17, nil)
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET PUBLICATION "test_pub"`))
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" CONNECTION 'host=localhost user=test dbname=test'`))
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET ("failover" = 'true', "origin" = 'none')`))
	})

	It("generates correct SQL for altering subscription with parameters for PostgreSQL 18", func() {
		obj := &apiv1.Subscription{
			Spec: apiv1.SubscriptionSpec{
				Name:            "test_sub",
				PublicationName: "test_pub",
				Parameters: map[string]string{
					"copy_data": "true",
					"origin":    "none",
					"failover":  "true",
					"two_phase": "true",
				},
			},
		}
		connString := "host=localhost user=test dbname=test"

		sqls := toSubscriptionAlterSQL(obj, connString, 18, nil)
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET PUBLICATION "test_pub"`))
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" CONNECTION 'host=localhost user=test dbname=test'`))
		Expect(sqls).To(ContainElement(
			`ALTER SUBSCRIPTION "test_sub" SET ("failover" = 'true', "origin" = 'none', "two_phase" = 'true')`))
	})

	It("returns correct SQL for altering subscription with no owner or parameters", func() {
		obj := &apiv1.Subscription{
			Spec: apiv1.SubscriptionSpec{
				Name:            "test_sub",
				PublicationName: "test_pub",
			},
		}
		connString := "host=localhost user=test dbname=test"

		sqls := toSubscriptionAlterSQL(obj, connString, defaultPostgresMajorVersion, nil)
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET PUBLICATION "test_pub"`))
		Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" CONNECTION 'host=localhost user=test dbname=test'`))
	})

	Context("when a parameter cannot be altered on an enabled subscription", func() {
		const connString = "host=localhost user=test dbname=test"

		newSubscription := func(parameters map[string]string) *apiv1.Subscription {
			return &apiv1.Subscription{
				Spec: apiv1.SubscriptionSpec{
					Name:            "test_sub",
					PublicationName: "test_pub",
					Parameters:      parameters,
				},
			}
		}

		It("does not re-send failover when it already matches the catalog", func() {
			obj := newSubscription(map[string]string{
				"failover": "true",
				"binary":   "true",
			})

			sqls := toSubscriptionAlterSQL(obj, connString, 17, map[string]string{"failover": "true"})
			Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET ("binary" = 'true')`))
			Expect(sqls).ToNot(ContainElement(ContainSubstring("failover")))
		})

		It("omits the SET entirely when failover was the only parameter left", func() {
			obj := newSubscription(map[string]string{"failover": "true"})

			sqls := toSubscriptionAlterSQL(obj, connString, 17, map[string]string{"failover": "true"})
			Expect(sqls).To(HaveLen(2))
			Expect(sqls).ToNot(ContainElement(ContainSubstring("SET (")))
		})

		It("still sends failover when the requested value differs", func() {
			obj := newSubscription(map[string]string{"failover": "true"})

			sqls := toSubscriptionAlterSQL(obj, connString, 17, map[string]string{"failover": "false"})
			Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET ("failover" = 'true')`))
		})

		It("treats equivalent boolean spellings as unchanged", func() {
			obj := newSubscription(map[string]string{"failover": "on"})

			sqls := toSubscriptionAlterSQL(obj, connString, 17, map[string]string{"failover": "true"})
			Expect(sqls).ToNot(ContainElement(ContainSubstring("failover")))
		})

		It("keeps the parameter when the catalog value is unknown", func() {
			obj := newSubscription(map[string]string{"failover": "true"})

			sqls := toSubscriptionAlterSQL(obj, connString, 17, nil)
			Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET ("failover" = 'true')`))
		})

		It("drops two_phase as well on PostgreSQL 18", func() {
			obj := newSubscription(map[string]string{
				"failover":  "true",
				"two_phase": "true",
				"origin":    "none",
			})

			sqls := toSubscriptionAlterSQL(obj, connString, 18, map[string]string{
				"failover":  "true",
				"two_phase": "true",
			})
			Expect(sqls).To(ContainElement(`ALTER SUBSCRIPTION "test_sub" SET ("origin" = 'none')`))
			Expect(sqls).ToNot(ContainElement(ContainSubstring("failover")))
			Expect(sqls).ToNot(ContainElement(ContainSubstring("two_phase")))
		})
	})
})
