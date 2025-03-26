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

package e2e

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/services"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// - spinning up a cluster with some post-init-sql query and verifying that they are really executed

// Set of tests in which we exercise managed roles
var _ = Describe("Managed roles tests", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		clusterManifest = fixturesDir + "/managed_roles/cluster-managed-roles.yaml.template"
		level           = tests.Medium
		ERROR           = "error"
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("plain vanilla cluster", Ordered, func() {
		const (
			namespacePrefix        = "managed-roles"
			secretName             = "cluster-example-dante"
			username               = "dante"
			appUsername            = "app"
			password               = "dante"
			newUserName            = "new_role"
			unrealizableUser       = "petrarca"
			userWithPerpetualPass  = "boccaccio"
			userWithHashedPassword = "cavalcanti"
		)
		var clusterName, namespace string

		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("setting up cluster with managed roles", func() {
				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
		})

		assertInRoles := func(namespace, primaryPod, roleName string, expectedRoles []string) {
			slices.Sort(expectedRoles)
			Eventually(func() []string {
				var rolesInDB []string
				query := `SELECT mem.inroles 
					FROM pg_catalog.pg_authid as auth
					LEFT JOIN (
						SELECT string_agg(pg_catalog.pg_get_userbyid(roleid), ',') as inroles, member
						FROM pg_catalog.pg_auth_members GROUP BY member
					) mem ON member = oid
					WHERE rolname =` + pq.QuoteLiteral(roleName)
				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod,
					},
					postgres.PostgresDBName,
					query)
				if err != nil {
					return []string{ERROR}
				}
				rolesInDB = strings.Split(strings.TrimSuffix(stdout, "\n"), ",")
				slices.Sort(rolesInDB)
				return rolesInDB
			}, 30).Should(BeEquivalentTo(expectedRoles))
		}

		assertRoleStatus := func(namespace, clusterName, query, expectedResult string) {
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
			Eventually(func(g Gomega) {
				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod.Name,
					},
					postgres.PostgresDBName,
					query)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(strings.TrimSpace(stdout)).To(Equal(expectedResult))
			}, 30).Should(Succeed())
		}

		It("can create roles specified in the managed roles stanza", func() {
			rolCanLoginInSpec := true
			rolSuperInSpec := false
			rolCreateDBInSpec := true
			rolCreateRoleInSpec := false
			rolInheritInSpec := false
			rolReplicationInSpec := false
			rolByPassRLSInSpec := false
			rolConnLimitInSpec := 4

			By("ensuring the roles created in the managed stanza are in the database with correct attributes", func() {
				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
					roleExistsQuery(username), "t"), 30).Should(Succeed())
				Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
					roleExistsQuery(userWithPerpetualPass), "t"), 30).Should(Succeed())
				Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
					roleExistsQuery(userWithHashedPassword), "t"), 30).Should(Succeed())
				Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
					roleExistsQuery(unrealizableUser), "f"), 30).Should(Succeed())

				query := fmt.Sprintf("SELECT true FROM pg_catalog.pg_roles WHERE rolname='%s' and rolcanlogin=%v and rolsuper=%v "+
					"and rolcreatedb=%v and rolcreaterole=%v and rolinherit=%v and rolreplication=%v "+
					"and rolbypassrls=%v and rolconnlimit=%v", username, rolCanLoginInSpec, rolSuperInSpec,
					rolCreateDBInSpec,
					rolCreateRoleInSpec, rolInheritInSpec, rolReplicationInSpec, rolByPassRLSInSpec, rolConnLimitInSpec)
				query2 := fmt.Sprintf("SELECT rolvaliduntil is NULL FROM pg_catalog.pg_roles WHERE rolname='%s'",
					userWithPerpetualPass)

				for _, q := range []string{query, query2} {
					stdout, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{
							Namespace: primaryPod.Namespace,
							PodName:   primaryPod.Name,
						},
						postgres.PostgresDBName,
						q)
					Expect(err).ToNot(HaveOccurred())
					Expect(stdout).To(Equal("t\n"))
				}
			})

			By("Verifying connectivity of new managed role", func() {
				rwService := services.GetReadWriteServiceName(clusterName)
				// assert connectable use username and password defined in secrets
				AssertConnection(namespace, rwService, postgres.PostgresDBName, username, password, env)
			})

			By("ensuring the app role has been granted createdb in the managed stanza", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, postgres.PostgresDBName,
					roleExistsQuery(appUsername), "t"), 30).Should(Succeed())

				query := fmt.Sprintf("SELECT rolcreatedb and rolvaliduntil='infinity' "+
					"FROM pg_catalog.pg_roles WHERE rolname='%s'", appUsername)
				assertRoleStatus(namespace, clusterName, query, "t")
			})

			By("verifying connectivity of app user", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())

				appUserSecret := corev1.Secret{}
				err = objects.Get(
					env.Ctx, env.Client,
					types.NamespacedName{Name: cluster.GetApplicationSecretName(), Namespace: namespace},
					&appUserSecret,
				)
				Expect(err).NotTo(HaveOccurred())

				pass := string(appUserSecret.Data["password"])
				rwService := services.GetReadWriteServiceName(clusterName)
				// assert connectable use username and password defined in secrets
				AssertConnection(namespace, rwService, postgres.PostgresDBName, appUsername, pass, env)
			})

			By("Verify show unrealizable role configurations in the status", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() int {
					return len(cluster.Status.ManagedRolesStatus.CannotReconcile)
				}, 30).Should(Equal(1))
				Eventually(func() int {
					return len(cluster.Status.ManagedRolesStatus.CannotReconcile[unrealizableUser])
				}, 30).Should(Equal(1))
				Eventually(func() string {
					return cluster.Status.ManagedRolesStatus.CannotReconcile[unrealizableUser][0]
				}, 30).Should(ContainSubstring("role \"foobar\" does not exist"))
			})
		})

		It("can update role attributes in the spec and they are applied in the database", func() {
			expectedLogin := false
			expectedCreateDB := false
			expectedCreateRole := true
			expectedConnLmt := int64(10)
			rwService := services.GetReadWriteServiceName(clusterName)

			By("updating role attribute in spec", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				updated.Spec.Managed.Roles[0].Login = expectedLogin
				updated.Spec.Managed.Roles[0].CreateDB = expectedCreateDB
				updated.Spec.Managed.Roles[0].CreateRole = expectedCreateRole
				updated.Spec.Managed.Roles[0].ConnectionLimit = expectedConnLmt
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify the role has been updated in the database", func() {
				query := fmt.Sprintf("SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='%s' and rolcanlogin=%v "+
					"and rolcreatedb=%v and rolcreaterole=%v and rolconnlimit=%v",
					username, expectedLogin, expectedCreateDB, expectedCreateRole, expectedConnLmt)
				assertRoleStatus(namespace, clusterName, query, "1")
			})

			By("the connection should fail since we disabled the login", func() {
				forwardConn, conn, err := postgres.ForwardPSQLServiceConnection(
					env.Ctx, env.Interface, env.RestClientConfig,
					namespace, rwService, postgres.PostgresDBName, username, password,
				)
				defer func() {
					_ = conn.Close()
					forwardConn.Close()
				}()
				Expect(err).ToNot(HaveOccurred())

				_, err = conn.Exec("SELECT 1")
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("not permitted to log in"))
			})

			By("enable Login again", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				updated := cluster.DeepCopy()
				updated.Spec.Managed.Roles[0].Login = true
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("verifying Login is now enabled", func() {
				expectedLogin = true
				query := fmt.Sprintf("SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='%s' and rolcanlogin=%v "+
					"and rolcreatedb=%v and rolcreaterole=%v and rolconnlimit=%v",
					username, expectedLogin, expectedCreateDB, expectedCreateRole, expectedConnLmt)
				assertRoleStatus(namespace, clusterName, query, "1")
			})

			By("the connectivity should be success again", func() {
				rwService := services.GetReadWriteServiceName(clusterName)
				// assert connectable use username and password defined in secrets
				AssertConnection(namespace, rwService, postgres.PostgresDBName, username, password, env)
			})
		})

		It("Can add role with all attribute omitted and verify it is default", func() {
			const (
				defaultRolCanLogin    = false
				defaultRolSuper       = false
				defaultRolCreateDB    = false
				defaultRolCreateRole  = false
				defaultRolInherit     = true
				defaultRolReplication = false
				defaultRolByPassRLS   = false
				defaultRolConnLimit   = int64(-1)
			)
			By("Add role new_role with all attribute omit", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				role := apiv1.RoleConfiguration{
					Name: newUserName,
				}
				updated.Spec.Managed.Roles = append(updated.Spec.Managed.Roles, role)
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify new_role exists with all attribute default", func() {
				query := fmt.Sprintf("SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='%s' and rolcanlogin=%v and rolsuper=%v "+
					"and rolcreatedb=%v and rolcreaterole=%v and rolinherit=%v and rolreplication=%v "+
					"and rolbypassrls=%v and rolconnlimit=%v", newUserName, defaultRolCanLogin,
					defaultRolSuper, defaultRolCreateDB,
					defaultRolCreateRole, defaultRolInherit, defaultRolReplication,
					defaultRolByPassRLS, defaultRolConnLimit)

				assertRoleStatus(namespace, clusterName, query, "1")
			})
		})

		It("Can update role comment and verify changes in db ", func() {
			By("Update comment for role new_role", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == newUserName {
						updated.Spec.Managed.Roles[i].Comment = fmt.Sprintf("This is user %s", newUserName)
					}
					if r.Name == username {
						updated.Spec.Managed.Roles[i].Comment = ""
					}
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By(fmt.Sprintf("Verify comments update in db for %s", newUserName), func() {
				query := fmt.Sprintf("SELECT pg_catalog.shobj_description(oid, 'pg_authid') as comment"+
					" FROM pg_catalog.pg_authid WHERE rolname='%s'",
					newUserName)
				assertRoleStatus(namespace, clusterName, query, fmt.Sprintf("This is user %s", newUserName))
			})

			By(fmt.Sprintf("Verify comments update in db for %s", username), func() {
				query := fmt.Sprintf("SELECT pg_catalog.shobj_description(oid, 'pg_authid') as comment"+
					" FROM pg_catalog.pg_authid WHERE rolname='%s'",
					username)
				assertRoleStatus(namespace, clusterName, query, "")
			})
		})

		It("Can update role membership and verify changes in db ", func() {
			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("Remove invalid parent role from unrealizableUser and verify user in database", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == unrealizableUser {
						updated.Spec.Managed.Roles[i].InRoles = []string{username}
					}
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
				Eventually(func() int {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					return len(cluster.Status.ManagedRolesStatus.CannotReconcile)
				}, 30).Should(Equal(0))
				Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
					roleExistsQuery(unrealizableUser), "t"), 30).Should(Succeed())
			})

			By("Add role in InRole for role new_role and verify in database", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == newUserName {
						updated.Spec.Managed.Roles[i].InRoles = []string{
							"postgres",
							username,
						}
					}
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.ManagedRolesStatus.CannotReconcile).To(BeEmpty())
				}, 30).Should(Succeed())
				assertInRoles(namespace, primaryPod.Name, newUserName, []string{"postgres", username})
			})

			By("Remove parent role from InRole for role new_role and verify in database", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == newUserName {
						updated.Spec.Managed.Roles[i].InRoles = []string{
							username,
						}
					}
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.ManagedRolesStatus.CannotReconcile).To(BeEmpty())
				}, 30).Should(Succeed())
				assertInRoles(namespace, primaryPod.Name, newUserName, []string{username})
			})

			By("Mock the error for unrealizable User and verify user in database", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == unrealizableUser {
						updated.Spec.Managed.Roles[i].InRoles = []string{unrealizableUser}
					}
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
				// user not changed
				Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
					roleExistsQuery(unrealizableUser), "t"), 30).Should(Succeed())
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.ManagedRolesStatus.CannotReconcile).To(HaveLen(1))
				}, 30).Should(Succeed())
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.ManagedRolesStatus.CannotReconcile[unrealizableUser]).To(HaveLen(1))
				}, 30).Should(Succeed())
				Eventually(func(g Gomega) {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(cluster.Status.ManagedRolesStatus.CannotReconcile[unrealizableUser][0]).To(
						ContainSubstring(fmt.Sprintf("role \"%s\" is a member of role \"%s\"",
							unrealizableUser, unrealizableUser)))
				}, 30).Should(Succeed())
			})
		})

		It("Can update role password in secrets and db and verify the connectivity", func() {
			var err error
			newPassword := "ThisIsNew"

			primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("update password from secrets", func() {
				AssertUpdateSecret("password", newPassword, secretName,
					namespace, clusterName, 30, env)
			})

			By("Verify connectivity using changed password in secret", func() {
				rwService := services.GetReadWriteServiceName(clusterName)
				// assert connectable use username and password defined in secrets
				AssertConnection(namespace, rwService, postgres.PostgresDBName, username, newPassword, env)
			})

			By("Update password in database", func() {
				query := fmt.Sprintf("ALTER ROLE %s WITH PASSWORD %s",
					username, pq.QuoteLiteral(newPassword))

				_, _, err = exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod.Name,
					},
					postgres.PostgresDBName,
					query)
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify password in secrets is still valid", func() {
				rwService := services.GetReadWriteServiceName(clusterName)
				AssertConnection(namespace, rwService, postgres.PostgresDBName, username, newPassword, env)
			})
		})

		It("Can update role password validUntil and verify in the database", func() {
			newValidUntilString := "2023-04-04T00:00:00.000000Z"
			By("Update comment for role new_role", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == newUserName {
						updated.Spec.Managed.Roles[i].ValidUntil = &metav1.Time{}
					}
					if r.Name == username {
						tt, err := time.Parse(time.RFC3339Nano, newValidUntilString)
						Expect(err).ToNot(HaveOccurred())
						nt := metav1.NewTime(tt)
						updated.Spec.Managed.Roles[i].ValidUntil = &nt
					}
				}

				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By(fmt.Sprintf("Verify valid until is removed in db for %s", newUserName), func() {
				query := fmt.Sprintf("SELECT rolvaliduntil is NULL FROM pg_catalog.pg_authid"+
					" WHERE rolname='%s'",
					newUserName)
				assertRoleStatus(namespace, clusterName, query, "t")
			})

			By(fmt.Sprintf("Verify valid until update in db for %s", username), func() {
				query := fmt.Sprintf("SELECT rolvaliduntil='%s' FROM pg_catalog.pg_authid "+
					" WHERE rolname='%s'",
					newValidUntilString, username)
				assertRoleStatus(namespace, clusterName, query, "t")
			})
		})

		It("Can drop role with ensure absent option", func() {
			By("Delete role new_role with EnsureOption ", func() {
				cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == newUserName {
						updated.Spec.Managed.Roles[i].Ensure = apiv1.EnsureAbsent
					}
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify new_role not existed in db", func() {
				primaryPod, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				Eventually(QueryMatchExpectationPredicate(primaryPod, postgres.PostgresDBName,
					roleExistsQuery(newUserName), "f"), 30).Should(Succeed())
			})
		})
	})
})
