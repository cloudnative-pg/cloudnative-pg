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
	"time"

	"github.com/lib/pq"
	corev1 "k8s.io/api/core/v1"
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

// Set of tests in which we check that the initdb options are really applied
var _ = Describe("Managed roles tests", Label(tests.LabelSmoke, tests.LabelBasic), func() {
	const (
		clusterManifest = fixturesDir + "/managed_roles/cluster-managed-roles.yaml.template"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("plain vanilla cluster", Ordered, func() {
		const (
			namespace   = "managed-roles"
			username    = "cnpg_admin"
			password    = "cnpg_admin"
			newUserName = "new_role"
		)
		var clusterName, secretName string
		var clusterNameSpacedName *types.NamespacedName
		var secretNameSpacedName *types.NamespacedName
		JustAfterEach(func() {
			if CurrentSpecReport().Failed() {
				env.DumpNamespaceObjects(namespace, "out/"+CurrentSpecReport().LeafNodeText+".log")
			}
		})

		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			err := env.CreateNamespace(namespace)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(func() error {
				return env.DeleteNamespace(namespace)
			})

			clusterName, err = env.GetResourceNameFromYAML(clusterManifest)
			secretName = "cluster-example-cnpgadmin"
			Expect(err).ToNot(HaveOccurred())
			clusterNameSpacedName = &types.NamespacedName{
				Namespace: namespace,
				Name:      clusterName,
			}
			secretNameSpacedName = &types.NamespacedName{
				Namespace: namespace,
				Name:      secretName,
			}
			By("setting up cluster with managed roles", func() {
				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
		})

		assertUserExists := func(namespace, primaryPod, username string, shouldExists bool) {
			cmd := `psql -U postgres postgres -tAc '\du'`
			stdout, _, err := utils.Run(fmt.Sprintf(
				"kubectl exec -n %v %v -- %v",
				namespace,
				primaryPod,
				cmd))
			Expect(err).ToNot(HaveOccurred())
			if shouldExists {
				Expect(stdout).To(ContainSubstring(username))
			} else {
				Expect(stdout).NotTo(ContainSubstring(username))
			}
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

			By("ensuring the role created in the managed stanza is in the database", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				assertUserExists(namespace, primaryPodInfo.Name, username, true)

				cmd := fmt.Sprintf("psql -U postgres postgres -tAc "+
					"\"SELECT 1 FROM pg_roles WHERE rolname='%s' and rolcanlogin=%v and rolsuper=%v "+
					"and rolcreatedb=%v and rolcreaterole=%v and rolinherit=%v and rolreplication=%v "+
					"and rolbypassrls=%v and rolconnlimit=%v\"", username, rolCanLoginInSpec, rolSuperInSpec, rolCreateDBInSpec,
					rolCreateRoleInSpec, rolInheritInSpec, rolReplicationInSpec, rolByPassRLSInSpec, rolConnLimitInSpec)

				stdout, _, err := utils.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					primaryPodInfo.Name,
					cmd))

				Expect(err).ToNot(HaveOccurred())
				Expect(stdout).To(Equal("1\n"))
			})

			By("Verify connectivity of use", func() {
				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				// assert connectable use username and password defined in secrets
				AssertConnection(rwService, username, "postgres", password, *psqlClientPod, 10, env)
			})
		})

		It("can update role attributes in the spec and they are applied in the database", func() {
			expectedLogin := false
			expectedCreateDB := false
			expectedCreateRole := true
			expectedConnLmt := int64(10)
			rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)

			By("updating role attribute in spec", func() {
				var cluster apiv1.Cluster
				err := env.Client.Get(env.Ctx, *clusterNameSpacedName, &cluster)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				updated.Spec.Managed.Roles[0].Login = expectedLogin
				updated.Spec.Managed.Roles[0].CreateDB = expectedCreateDB
				updated.Spec.Managed.Roles[0].CreateRole = expectedCreateRole
				updated.Spec.Managed.Roles[0].ConnectionLimit = expectedConnLmt
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(&cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify the role has been updated in the database", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(func() string {
					cmd := fmt.Sprintf("psql -U postgres postgres -tAc "+
						"\"SELECT 1 FROM pg_roles WHERE rolname='%s' and rolcanlogin=%v "+
						"and rolcreatedb=%v and rolcreaterole=%v and rolconnlimit=%v\"",
						username, expectedLogin, expectedCreateDB, expectedCreateRole, expectedConnLmt)

					stdout, _, err := utils.Run(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						primaryPodInfo.Name,
						cmd))
					if err != nil {
						return ""
					}
					return stdout
				}, 10).Should(Equal("1\n"))
			})

			By("the connection should fail since we disabled the login", func() {
				dsn := fmt.Sprintf("host=%v user=%v dbname=%v password=%v sslmode=require",
					rwService, username, "postgres", password)
				timeout := time.Second * 5
				_, _, err := env.ExecCommand(env.Ctx, *psqlClientPod, specs.PostgresContainerName, &timeout,
					"psql", dsn, "-tAc", "SELECT 1")
				Expect(err).To(HaveOccurred())
			})

			By("enable Login again", func() {
				var cluster apiv1.Cluster
				err := env.Client.Get(env.Ctx, *clusterNameSpacedName, &cluster)
				Expect(err).ToNot(HaveOccurred())
				updated := cluster.DeepCopy()
				updated.Spec.Managed.Roles[0].Login = true
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(&cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("the connectivity should be success again", func() {
				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				// assert connectable use username and password defined in secrets
				AssertConnection(rwService, username, "postgres", password, *psqlClientPod, 10, env)
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
				var cluster apiv1.Cluster
				err := env.Client.Get(env.Ctx, *clusterNameSpacedName, &cluster)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				role := apiv1.RoleConfiguration{
					Name: newUserName,
				}
				updated.Spec.Managed.Roles = append(updated.Spec.Managed.Roles, role)
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(&cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify new_role exists with all attribute default", func() {
				Eventually(func() string {
					cmd := fmt.Sprintf("psql -U postgres postgres -tAc "+
						"\"SELECT 1 FROM pg_roles WHERE rolname='%s' and rolcanlogin=%v and rolsuper=%v "+
						"and rolcreatedb=%v and rolcreaterole=%v and rolinherit=%v and rolreplication=%v "+
						"and rolbypassrls=%v and rolconnlimit=%v\"", newUserName, defaultRolCanLogin,
						defaultRolSuper, defaultRolCreateDB,
						defaultRolCreateRole, defaultRolInherit, defaultRolReplication,
						defaultRolByPassRLS, defaultRolConnLimit)

					stdout, _, err := utils.Run(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						primaryPodInfo.Name,
						cmd))
					if err != nil {
						return ""
					}
					return stdout
				}, 10).Should(Equal("1\n"))
			})
		})

		It("Can update role comment and verify changes in db ", func() {
			By("Update comment for role new_role", func() {
				var cluster apiv1.Cluster
				err := env.Client.Get(env.Ctx, *clusterNameSpacedName, &cluster)
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
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(&cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
			newUserName := "new_role"
			Expect(err).ToNot(HaveOccurred())

			By(fmt.Sprintf("Verify comments update in db for %s", newUserName), func() {
				Eventually(func() string {
					cmd := fmt.Sprintf("psql -U postgres postgres -tAc "+
						"\"SELECT pg_catalog.shobj_description(oid, 'pg_authid') as comment"+
						" FROM pg_catalog.pg_authid WHERE rolname='%s'\"",
						newUserName)

					stdout, _, err := utils.Run(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						primaryPodInfo.Name,
						cmd))
					if err != nil {
						return "error"
					}
					return stdout
				}, 10).Should(Equal(fmt.Sprintf("This is user %s\n", newUserName)))
			})

			By(fmt.Sprintf("Verify comments update in db for %s", username), func() {
				Eventually(func() string {
					cmd := fmt.Sprintf("psql -U postgres postgres -tAc "+
						"\"SELECT pg_catalog.shobj_description(oid, 'pg_authid') as comment"+
						" FROM pg_catalog.pg_authid WHERE rolname='%s'\"",
						username)

					stdout, _, err := utils.Run(fmt.Sprintf(
						"kubectl exec -n %v %v -- %v",
						namespace,
						primaryPodInfo.Name,
						cmd))
					if err != nil {
						return "error"
					}
					return stdout
				}, 10).Should(Equal("\n"))
			})
		})
		// TODO remove pending decorator once CNP-3571 is fixed
		It("Can update role password in secrets and db and verify the connectivity", func() {
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
				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				// assert connectable use username and password defined in secrets
				AssertConnection(rwService, username, "postgres", newPassword, *psqlClientPod, 10, env)
			})

			By("Update password in database", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())
				cmd := fmt.Sprintf("psql -U postgres postgres -tAc "+
					"\"ALTER ROLE %s WITH PASSWORD %s\"",
					username, pq.QuoteLiteral(newPassword))

				_, _, err = utils.Run(fmt.Sprintf(
					"kubectl exec -n %v %v -- %v",
					namespace,
					primaryPodInfo.Name,
					cmd))
				Expect(err).To(BeNil())
			})

			By("Verify password in secrets could still valid", func() {
				rwService := fmt.Sprintf("%v-rw.%v.svc", clusterName, namespace)
				AssertConnection(rwService, username, "postgres", newPassword, *psqlClientPod, 30, env)
			})
		})

		It("Can drop role with ensure absent option", func() {
			By("Delete role new_role with EnsureOption ", func() {
				var cluster apiv1.Cluster
				err := env.Client.Get(env.Ctx, *clusterNameSpacedName, &cluster)
				Expect(err).ToNot(HaveOccurred())

				updated := cluster.DeepCopy()
				for i, r := range updated.Spec.Managed.Roles {
					if r.Name == newUserName {
						updated.Spec.Managed.Roles[i].Ensure = apiv1.EnsureAbsent
					}
				}
				err = env.Client.Patch(env.Ctx, updated, client.MergeFrom(&cluster))
				Expect(err).ToNot(HaveOccurred())
			})

			By("Verify new_role not existed in db", func() {
				primaryPodInfo, err := env.GetClusterPrimary(namespace, clusterName)
				newUserName := "new_role"
				Expect(err).ToNot(HaveOccurred())
				assertUserExists(namespace, primaryPodInfo.Name, newUserName, false)
			})
		})
	})
})
