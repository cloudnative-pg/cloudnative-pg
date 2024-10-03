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

package e2e

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/specs"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils"

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
			username               = "dante"
			appUsername            = "app"
			password               = "dante"
			newUserName            = "new_role"
			unrealizableUser       = "petrarca"
			userWithPerpetualPass  = "boccaccio"
			userWithHashedPassword = "cavalcanti"
		)
		var clusterName, secretName, namespace string
		var secretNameSpacedName *types.NamespacedName

		BeforeAll(func() {
			var err error
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = env.GetResourceNameFromYAML(clusterManifest)
			Expect(err).ToNot(HaveOccurred())

			secretName = "cluster-example-dante"
			secretNameSpacedName = &types.NamespacedName{
				Namespace: namespace,
				Name:      secretName,
			}

			By("setting up cluster with managed roles", func() {
				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
		})

		assertUserExists := func(namespace, primaryPod, username string, shouldExists bool) {
			Eventually(func(g Gomega) {
				stdout, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod,
					},
					utils.DatabaseName("postgres"),
					"\\du")
				g.Expect(err).ToNot(HaveOccurred())
				if shouldExists {
					g.Expect(stdout).To(ContainSubstring(username))
				} else {
					g.Expect(stdout).NotTo(ContainSubstring(username))
				}
			}, 60).Should(Succeed())
		}

		assertInRoles := func(namespace, primaryPod, roleName string, expectedRoles []string) {
			slices.Sort(expectedRoles)
			Eventually(func() []string {
				var rolesInDB []string
				query := `SELECT mem.inroles 
					FROM pg_catalog.pg_authid as auth
					LEFT JOIN (
						SELECT string_agg(pg_get_userbyid(roleid), ',') as inroles, member
						FROM pg_auth_members GROUP BY member
					) mem ON member = oid
					WHERE rolname =` + pq.QuoteLiteral(roleName)
				stdout, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod,
					},
					utils.DatabaseName("postgres"),
					query)
				if err != nil {
					return []string{ERROR}
				}
				rolesInDB = strings.Split(strings.TrimSuffix(stdout, "\n"), ",")
				slices.Sort(rolesInDB)
				return rolesInDB
			}, 30).Should(BeEquivalentTo(expectedRoles))
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
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				assertUserExists(namespace, primaryPodInfo.Name, username, true)
				assertUserExists(namespace, primaryPodInfo.Name, userWithPerpetualPass, true)
				assertUserExists(namespace, primaryPodInfo.Name, userWithHashedPassword, true)
				assertUserExists(namespace, primaryPodInfo.Name, unrealizableUser, false)

				query := fmt.Sprintf("SELECT true FROM pg_roles WHERE rolname='%s' and rolcanlogin=%v and rolsuper=%v "+
					"and rolcreatedb=%v and rolcreaterole=%v and rolinherit=%v and rolreplication=%v "+
					"and rolbypassrls=%v and rolconnlimit=%v", username, rolCanLoginInSpec, rolSuperInSpec, rolCreateDBInSpec,
					rolCreateRoleInSpec, rolInheritInSpec, rolReplicationInSpec, rolByPassRLSInSpec, rolConnLimitInSpec)
				query2 := fmt.Sprintf("SELECT rolvaliduntil is NULL FROM pg_roles WHERE rolname='%s'", userWithPerpetualPass)

				for _, q := range []string{query, query2} {
					stdout, _, err := env.ExecQueryInInstancePod(
						utils.PodLocator{
							Namespace: namespace,
							PodName:   primaryPodInfo.Name,
						},
						utils.DatabaseName("postgres"),
						q)
					Expect(err).ToNot(HaveOccurred())
					Expect(stdout).To(Equal("t\n"))
				}
			})

			By("Verifying connectivity of new managed role", func() {
				primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				// assert connectable use username and password defined in secrets
				AssertConnection(rwService, username, "postgres", password, primaryPod, 30, env)

				AssertConnection(rwService, userWithHashedPassword, "postgres", userWithHashedPassword, primaryPod, 30, env)
			})

			By("ensuring the app role has been granted createdb in the managed stanza", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				assertUserExists(namespace, primaryPodInfo.Name, appUsername, true)

				query := fmt.Sprintf("SELECT rolcreatedb and rolvaliduntil='infinity' "+
					"FROM pg_roles WHERE rolname='%s'", appUsername)

				stdout, _, err := env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPodInfo.Name,
					},
					utils.DatabaseName("postgres"),
					query)
				Expect(err).ToNot(HaveOccurred())
				Expect(stdout).To(Equal("t\n"))
			})

			By("verifying connectivity of app user", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).NotTo(HaveOccurred())

				appUserSecret := corev1.Secret{}
				err = utils.GetObject(
					env,
					types.NamespacedName{Name: cluster.GetApplicationSecretName(), Namespace: namespace},
					&appUserSecret,
				)
				Expect(err).NotTo(HaveOccurred())

				primaryPod, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				pass := string(appUserSecret.Data["password"])
				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				// assert connectable use username and password defined in secrets
				AssertConnection(rwService, appUsername, "postgres", pass, primaryPod, 30, env)
			})

			By("Verify show unrealizable role configurations in the status", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
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
			var primaryPod *corev1.Pod
			var err error
			expectedLogin := false
			expectedCreateDB := false
			expectedCreateRole := true
			expectedConnLmt := int64(10)
			rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)

			By("updating role attribute in spec", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
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
				primaryPod, err = env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() string {
					query := fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname='%s' and rolcanlogin=%v "+
						"and rolcreatedb=%v and rolcreaterole=%v and rolconnlimit=%v",
						username, expectedLogin, expectedCreateDB, expectedCreateRole, expectedConnLmt)

					stdout, _, err := env.ExecQueryInInstancePod(
						utils.PodLocator{
							Namespace: namespace,
							PodName:   primaryPod.Name,
						},
						utils.DatabaseName("postgres"),
						query)
					if err != nil {
						return ""
					}
					return stdout
				}, 30).Should(Equal("1\n"))
			})

			By("the connection should fail since we disabled the login", func() {
				dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require",
					rwService, username, "postgres", password)
				timeout := time.Second * 10
				_, _, err := env.ExecCommand(env.Ctx, *primaryPod, specs.PostgresContainerName, &timeout,
					"psql", dsn, "-tAc", "SELECT 1")
				Expect(err).To(HaveOccurred())
			})

			By("enable Login again", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				updated := cluster.DeepCopy()
				updated.Spec.Managed.Roles[0].Login = true
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("the connectivity should be success again", func() {
				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				// assert connectable use username and password defined in secrets
				AssertConnection(rwService, username, "postgres", password, primaryPod, 30, env)
			})
		})

		It("Can add role with all attribute omitted and verify it is default", func() {
			primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())
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
				cluster, err := env.GetCluster(namespace, clusterName)
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
				Eventually(func() string {
					query := fmt.Sprintf("SELECT 1 FROM pg_roles WHERE rolname='%s' and rolcanlogin=%v and rolsuper=%v "+
						"and rolcreatedb=%v and rolcreaterole=%v and rolinherit=%v and rolreplication=%v "+
						"and rolbypassrls=%v and rolconnlimit=%v", newUserName, defaultRolCanLogin,
						defaultRolSuper, defaultRolCreateDB,
						defaultRolCreateRole, defaultRolInherit, defaultRolReplication,
						defaultRolByPassRLS, defaultRolConnLimit)

					stdout, _, err := env.ExecQueryInInstancePod(
						utils.PodLocator{
							Namespace: namespace,
							PodName:   primaryPodInfo.Name,
						},
						utils.DatabaseName("postgres"),
						query)
					if err != nil {
						return ""
					}
					return stdout
				}, 30).Should(Equal("1\n"))
			})
		})

		It("Can update role comment and verify changes in db ", func() {
			By("Update comment for role new_role", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
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

			primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("Verify comments update in db for %s", newUserName), func() {
				Eventually(func() string {
					query := fmt.Sprintf("SELECT pg_catalog.shobj_description(oid, 'pg_authid') as comment"+
						" FROM pg_catalog.pg_authid WHERE rolname='%s'",
						newUserName)

					stdout, _, err := env.ExecQueryInInstancePod(
						utils.PodLocator{
							Namespace: namespace,
							PodName:   primaryPodInfo.Name,
						},
						utils.DatabaseName("postgres"),
						query)
					if err != nil {
						return ERROR
					}
					return stdout
				}, 30).Should(Equal(fmt.Sprintf("This is user %s\n", newUserName)))
			})

			By(fmt.Sprintf("Verify comments update in db for %s", username), func() {
				Eventually(func() string {
					query := fmt.Sprintf("SELECT pg_catalog.shobj_description(oid, 'pg_authid') as comment"+
						" FROM pg_catalog.pg_authid WHERE rolname='%s'",
						username)

					stdout, _, err := env.ExecQueryInInstancePod(
						utils.PodLocator{
							Namespace: namespace,
							PodName:   primaryPodInfo.Name,
						},
						utils.DatabaseName("postgres"),
						query)
					if err != nil {
						return ERROR
					}
					return stdout
				}, 30).Should(Equal("\n"))
			})
		})

		It("Can update role membership and verify changes in db ", func() {
			primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By("Remove invalid parent role from unrealizableUser and verify user in database", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
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
					cluster, err := env.GetCluster(namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					return len(cluster.Status.ManagedRolesStatus.CannotReconcile)
				}, 30).Should(Equal(0))
				assertUserExists(namespace, primaryPodInfo.Name, unrealizableUser, true)
			})

			By("Add role in InRole for role new_role and verify in database", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
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
				Eventually(func() int {
					cluster, err := env.GetCluster(namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					return len(cluster.Status.ManagedRolesStatus.CannotReconcile)
				}, 30).Should(Equal(0))
				assertInRoles(namespace, primaryPodInfo.Name, newUserName, []string{"postgres", username})
			})

			By("Remove parent role from InRole for role new_role and verify in database", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
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
				Eventually(func() int {
					cluster, err := env.GetCluster(namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					return len(cluster.Status.ManagedRolesStatus.CannotReconcile)
				}, 30).Should(Equal(0))
				assertInRoles(namespace, primaryPodInfo.Name, newUserName, []string{username})
			})

			By("Mock the error for unrealizable User and verify user in database", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
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
				assertUserExists(namespace, primaryPodInfo.Name, unrealizableUser, true)
				Eventually(func() int {
					cluster, err := env.GetCluster(namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					return len(cluster.Status.ManagedRolesStatus.CannotReconcile)
				}, 30).Should(Equal(1))
				Eventually(func() int {
					cluster, err := env.GetCluster(namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					return len(cluster.Status.ManagedRolesStatus.CannotReconcile[unrealizableUser])
				}, 30).Should(Equal(1))
				Eventually(func() string {
					cluster, err := env.GetCluster(namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())
					return cluster.Status.ManagedRolesStatus.CannotReconcile[unrealizableUser][0]
				}, 30).Should(ContainSubstring(fmt.Sprintf("role \"%s\" is a member of role \"%s\"",
					unrealizableUser, unrealizableUser)))
			})
		})

		It("Can update role password in secrets and db and verify the connectivity", func() {
			var primaryPod *corev1.Pod
			var err error
			newPassword := "ThisIsNew"

			By("update password from secrets", func() {
				var secret corev1.Secret
				err := env.Client.Get(env.Ctx, *secretNameSpacedName, &secret)
				Expect(err).ToNot(HaveOccurred())

				updated := secret.DeepCopy()
				updated.Data["password"] = []byte(newPassword)
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(&secret))
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify connectivity using changed password in secret", func() {
				primaryPod, err = env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				// assert connectable use username and password defined in secrets
				AssertConnection(rwService, username, "postgres", newPassword, primaryPod, 30, env)
			})

			By("Update password in database", func() {
				query := fmt.Sprintf("ALTER ROLE %s WITH PASSWORD %s",
					username, pq.QuoteLiteral(newPassword))

				_, _, err = env.ExecQueryInInstancePod(
					utils.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod.Name,
					},
					utils.DatabaseName("postgres"),
					query)
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify password in secrets could still valid", func() {
				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				AssertConnection(rwService, username, "postgres", newPassword, primaryPod, 60, env)
			})
		})

		It("Can update role password validUntil and verify in the database", func() {
			newValidUntilString := "2023-04-04T00:00:00.000000Z"
			By("Update comment for role new_role", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == newUserName {
						updated.Spec.Managed.Roles[i].ValidUntil = &v1.Time{}
					}
					if r.Name == username {
						tt, err := time.Parse(time.RFC3339Nano, newValidUntilString)
						Expect(err).ToNot(HaveOccurred())
						nt := v1.NewTime(tt)
						updated.Spec.Managed.Roles[i].ValidUntil = &nt
					}
				}

				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("Verify valid until is removed in db for %s", newUserName), func() {
				Eventually(func() string {
					query := fmt.Sprintf("SELECT rolvaliduntil is NULL FROM pg_catalog.pg_authid"+
						" WHERE rolname='%s'",
						newUserName)

					stdout, _, err := env.ExecQueryInInstancePod(
						utils.PodLocator{
							Namespace: namespace,
							PodName:   primaryPodInfo.Name,
						},
						utils.DatabaseName("postgres"),
						query)
					if err != nil {
						return ERROR
					}
					return stdout
				}).Should(Equal("t\n"))
			})

			By(fmt.Sprintf("Verify valid until update in db for %s", username), func() {
				Eventually(func() string {
					query := fmt.Sprintf("SELECT rolvaliduntil='%s' FROM pg_catalog.pg_authid "+
						" WHERE rolname='%s'",
						newValidUntilString, username)

					stdout, _, err := env.ExecQueryInInstancePod(
						utils.PodLocator{
							Namespace: namespace,
							PodName:   primaryPodInfo.Name,
						},
						utils.DatabaseName("postgres"),
						query)
					if err != nil {
						return ERROR
					}
					return stdout
				}, 30).Should(Equal("t\n"))
			})
		})

		It("Can drop role with ensure absent option", func() {
			By("Delete role new_role with EnsureOption ", func() {
				cluster, err := env.GetCluster(namespace, clusterName)
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
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				assertUserExists(namespace, primaryPodInfo.Name, newUserName, false)
			})
		})
	})
})
