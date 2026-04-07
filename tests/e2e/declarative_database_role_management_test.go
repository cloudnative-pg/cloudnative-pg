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
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/services"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Set of tests in which we use the DatabaseRole CRD to add new roles on an existing cluster
var _ = Describe("Declarative role management", Label(tests.LabelSmoke, tests.LabelBasic,
	tests.LabelDeclarativeDatabaseRoles), func() {
	const (
		clusterManifest = fixturesDir + "/declarative_roles/cluster.yaml.template"
		level           = tests.Medium
	)

	BeforeEach(func() {
		if testLevelEnv.Depth < int(level) {
			Skip("Test depth is lower than the amount requested for this test")
		}
	})

	Context("in a plain vanilla cluster", Ordered, func() {
		const (
			namespacePrefix = "declarative-roles"
		)
		var (
			clusterName, namespace string
			err                    error
		)

		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("setting up cluster", func() {
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
					return []string{fmt.Sprintf("ERROR: %v", err.Error())}
				}
				rolesInDB = strings.Split(strings.TrimSuffix(stdout, "\n"), ",")
				slices.Sort(rolesInDB)
				return rolesInDB
			}, 30).Should(BeEquivalentTo(expectedRoles))
		}

		assertRoleHasExpectedFields := func(namespace, primaryPod string, role apiv1.DatabaseRole) {
			boolPtrToSQL := func(b *bool) string {
				if b == nil {
					return "NULL"
				}
				if *b {
					return "TRUE"
				}
				return "FALSE"
			}

			query := fmt.Sprintf("SELECT true FROM pg_catalog.pg_roles WHERE rolname='%s' and rolcanlogin=%v and rolsuper=%v "+
				"and rolcreatedb=%v and rolcreaterole=%v and rolinherit is %s and rolreplication=%v "+
				"and rolbypassrls=%v and rolconnlimit=%v", role.Spec.Name, role.Spec.Login, role.Spec.Superuser,
				role.Spec.CreateDB, role.Spec.CreateRole, boolPtrToSQL(role.Spec.Inherit),
				role.Spec.Replication, role.Spec.BypassRLS, role.Spec.ConnectionLimit)
			Eventually(func(g Gomega) {
				stdout, _, err := exec.QueryInInstancePod(
					env.Ctx, env.Client, env.Interface, env.RestClientConfig,
					exec.PodLocator{
						Namespace: namespace,
						PodName:   primaryPod,
					},
					"postgres",
					query)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(strings.TrimSpace(stdout)).Should(Equal("t"), "expected role not found")
			}, 30).Should(Succeed())
		}

		assertTestDeclarativeRole := func(
			roleManifest string,
			retainOnDeletion bool,
		) {
			var (
				role           apiv1.DatabaseRole
				roleObjectName string
			)
			By("applying DatabaseRole CRD manifest", func() {
				CreateResourceFromFile(namespace, roleManifest)
				roleObjectName, err = yaml.GetResourceNameFromYAML(env.Scheme, roleManifest)
				Expect(err).NotTo(HaveOccurred())
			})
			By("ensuring the DatabaseRole CRD succeeded reconciliation", func() {
				// get role object
				role = apiv1.DatabaseRole{}
				roleNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      roleObjectName,
				}

				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, roleNamespacedName, &role)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(role.Status.Applied).Should(HaveValue(BeTrue()))
					g.Expect(role.Status.Message).Should(BeEmpty())
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("verifying new role has been created with the expected fields", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, postgres.PostgresDBName,
					roleExistsQuery(role.Spec.Name), "t"), 30).Should(Succeed())

				assertRoleHasExpectedFields(namespace, primaryPodInfo.Name, role)
			})

			By("verifying new role has been created with the correct groups", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, postgres.PostgresDBName,
					roleExistsQuery(role.Spec.Name), "t"), 30).Should(Succeed())

				assertInRoles(namespace, primaryPodInfo.Name, role.Spec.Name, role.Spec.InRoles)
			})

			By("removing the Role object", func() {
				Expect(objects.Delete(env.Ctx, env.Client, &role)).To(Succeed())
			})

			By("verifying the retention policy in the postgres cluster", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(QueryMatchExpectationPredicate(primaryPodInfo, postgres.PostgresDBName,
					roleExistsQuery(role.Spec.Name), boolPGOutput(retainOnDeletion)), 30).Should(Succeed())
			})
		}

		When("Role CR reclaim policy is set to delete", func() {
			It("can manage a declarative role and delete it in Postgres", func() {
				roleManifest := fixturesDir +
					"/declarative_roles/role-with-delete-reclaim-policy.yaml.template"
				assertTestDeclarativeRole(roleManifest,
					false)
			})
		})

		When("Role CR reclaim policy is set to retain", func() {
			It("can manage a declarative role and release it", func() {
				roleManifest := fixturesDir + "/declarative_roles/databaserole.yaml.template"
				assertTestDeclarativeRole(roleManifest, true)
			})
		})

		When("Two Role CRs are managing the same PostgreSQL Role", func() {
			It("will make sure that only one is managing it", func() {
				var (
					pgRoleName string
					firstRole  *apiv1.DatabaseRole
					secondRole *apiv1.DatabaseRole
				)

				By("choosing a random postgresql user name", func() {
					pgRoleName = fmt.Sprintf("conflicting-role-%d", funk.RandomInt(0, 9999))
				})

				By("applying a manifest for it", func() {
					firstRole = &apiv1.DatabaseRole{}
					firstRole.Name = fmt.Sprintf("first-%s", pgRoleName)
					firstRole.Namespace = namespace
					firstRole.Spec = apiv1.DatabaseRoleSpec{
						RoleConfiguration: apiv1.RoleConfiguration{
							Name:   pgRoleName,
							Ensure: apiv1.EnsurePresent,
							Login:  true,
						},
						ClusterRef: corev1.LocalObjectReference{
							Name: clusterName,
						},
						ReclaimPolicy: apiv1.DatabaseRoleReclaimDelete,
					}
					Expect(env.Client.Create(env.Ctx, firstRole)).To(Succeed())
				})

				By("ensuring the Role CR succeeded reconciliation", func() {
					roleNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      firstRole.Name,
					}
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, roleNamespacedName, firstRole)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(firstRole.Status.Applied).Should(HaveValue(BeTrue()))
						g.Expect(firstRole.Status.Message).Should(BeEmpty())
					}, 300).WithPolling(10 * time.Second).Should(Succeed())
				})

				By("applying the same Role CR but with a different object name", func() {
					secondRole = &apiv1.DatabaseRole{}
					secondRole.Name = fmt.Sprintf("second-%s", pgRoleName)
					secondRole.Namespace = namespace
					secondRole.Spec = apiv1.DatabaseRoleSpec{
						RoleConfiguration: apiv1.RoleConfiguration{
							Name:   pgRoleName,
							Ensure: apiv1.EnsurePresent,
							Login:  true,
						},
						ClusterRef: corev1.LocalObjectReference{
							Name: clusterName,
						},
						ReclaimPolicy: apiv1.DatabaseRoleReclaimDelete,
					}
					Expect(env.Client.Create(env.Ctx, secondRole)).To(Succeed())
				})

				By("ensuring the reconciliation of it failed", func() {
					roleNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      secondRole.Name,
					}
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, roleNamespacedName, secondRole)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(secondRole.Status.Applied).Should(HaveValue(BeFalse()))
						g.Expect(secondRole.Status.Message).ShouldNot(BeEmpty())
					}, 300).WithPolling(10 * time.Second).Should(Succeed())
				})

				By("deleting both of them", func() {
					Expect(objects.Delete(env.Ctx, env.Client, firstRole)).To(Succeed())
					Expect(objects.Delete(env.Ctx, env.Client, secondRole)).To(Succeed())
				})
			})
		})

		When("Role CR is managing the password of the PostgreSQL role", func() {
			It("updates the password inside the PostgreSQL catalog", func() {
				var (
					role            *apiv1.DatabaseRole
					passwordSecret  *corev1.Secret
					pgRoleName      string
					secretName      string
					initialPassword string
					newPassword     string
				)

				By("creating a Role CR without a password secret specified", func() {
					pgRoleName = fmt.Sprintf("password-test-role-%d", funk.RandomInt(0, 9999))
					role = &apiv1.DatabaseRole{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("password-test-role-%s", pgRoleName),
							Namespace: namespace,
						},
						Spec: apiv1.DatabaseRoleSpec{
							RoleConfiguration: apiv1.RoleConfiguration{
								Name:   pgRoleName,
								Ensure: apiv1.EnsurePresent,
								Login:  true,
							},
							ClusterRef: corev1.LocalObjectReference{
								Name: clusterName,
							},
							ReclaimPolicy: apiv1.DatabaseRoleReclaimDelete,
						},
					}
					Expect(env.Client.Create(env.Ctx, role)).To(Succeed())
				})

				By("ensuring the Role has been reconciled", func() {
					roleNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      role.Name,
					}
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, roleNamespacedName, role)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(role.Status.Applied).Should(HaveValue(BeTrue()))
						g.Expect(role.Status.Message).Should(BeEmpty())
					}, 300).WithPolling(10 * time.Second).Should(Succeed())
				})

				By("creating a secret with a random password", func() {
					secretName = fmt.Sprintf("secret-%s", pgRoleName)
					initialPassword = fmt.Sprintf("password-%d", funk.RandomInt(0, 9999))
					passwordSecret = &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      secretName,
							Namespace: namespace,
							Labels: map[string]string{
								utils.WatchedLabelName: "true",
							},
						},
						StringData: map[string]string{
							"username": pgRoleName,
							"password": initialPassword,
						},
						Type: corev1.SecretTypeBasicAuth,
					}
					Expect(env.Client.Create(env.Ctx, passwordSecret)).To(Succeed())
				})

				By("referring to the secret in the Role CR", func() {
					roleNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      role.Name,
					}
					err := env.Client.Get(env.Ctx, roleNamespacedName, role)
					Expect(err).ToNot(HaveOccurred())

					role.Spec.PasswordSecret = &apiv1.LocalObjectReference{
						Name: secretName,
					}
					Expect(env.Client.Update(env.Ctx, role)).To(Succeed())
				})

				By("ensuring the CR has been reconciled", func() {
					roleNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      role.Name,
					}
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, roleNamespacedName, role)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(role.Status.Applied).Should(HaveValue(BeTrue()))
						g.Expect(role.Status.Message).Should(BeEmpty())
						// check if the transaction ID has been set in the status
						g.Expect(role.Status.PasswordState.SecretResourceVersion).ShouldNot(BeZero())
					}, 3000).WithPolling(10 * time.Second).Should(Succeed())
				})

				By("checking if we can connect to PostgreSQL using specified password", func() {
					rwService := services.GetReadWriteServiceName(clusterName)
					AssertConnection(namespace, rwService, postgres.PostgresDBName, pgRoleName, initialPassword, env)
				})

				By("changing the password in the secret", func() {
					secretNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      secretName,
					}
					err := env.Client.Get(env.Ctx, secretNamespacedName, passwordSecret)
					Expect(err).ToNot(HaveOccurred())

					newPassword = fmt.Sprintf("newpassword-%d", funk.RandomInt(0, 9999))
					passwordSecret.Data["password"] = []byte(newPassword)
					Expect(env.Client.Update(env.Ctx, passwordSecret)).To(Succeed())
				})

				By("checking if we can connect to PostgreSQL using the new password", func() {
					rwService := services.GetReadWriteServiceName(clusterName)
					AssertConnection(namespace, rwService, postgres.PostgresDBName, pgRoleName, newPassword, env)
				})
			})
		})
	})

	Context("in a cluster with managed roles", Ordered, func() {
		const (
			namespacePrefix = "declarative-roles"
			clusterManifest = fixturesDir + "/declarative_roles/cluster-managed-roles.yaml.template"
		)
		var (
			clusterName, namespace string
			err                    error
		)

		BeforeAll(func() {
			// Create a cluster in a namespace we'll delete after the test
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("setting up cluster", func() {
				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
		})

		It("marks roles already managed as failed", func() {
			var (
				petrarcaRole *apiv1.DatabaseRole
				aristoRole   *apiv1.DatabaseRole
			)

			By("creating a declarative role named 'petrarca'", func() {
				petrarcaRole = &apiv1.DatabaseRole{}
				petrarcaRole.Name = "role-petrarca"
				petrarcaRole.Namespace = namespace
				petrarcaRole.Spec = apiv1.DatabaseRoleSpec{
					RoleConfiguration: apiv1.RoleConfiguration{
						Name:   "petrarca",
						Ensure: apiv1.EnsurePresent,
						Login:  true,
					},
					ClusterRef: corev1.LocalObjectReference{
						Name: clusterName,
					},
					ReclaimPolicy: apiv1.DatabaseRoleReclaimDelete,
				}
				Expect(env.Client.Create(env.Ctx, petrarcaRole)).To(Succeed())
			})

			By("ensuring the reconciliation of the role failed", func() {
				roleNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      petrarcaRole.Name,
				}
				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, roleNamespacedName, petrarcaRole)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(petrarcaRole.Status.Applied).Should(HaveValue(BeFalse()))
					g.Expect(petrarcaRole.Status.Message).ShouldNot(BeEmpty())
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})

			By("creating a declarative role named 'ariosto'", func() {
				aristoRole = &apiv1.DatabaseRole{}
				aristoRole.Name = "role-ariosto"
				aristoRole.Namespace = namespace
				aristoRole.Spec = apiv1.DatabaseRoleSpec{
					RoleConfiguration: apiv1.RoleConfiguration{
						Name:   "ariosto",
						Ensure: apiv1.EnsurePresent,
						Login:  true,
					},
					ClusterRef: corev1.LocalObjectReference{
						Name: clusterName,
					},
					ReclaimPolicy: apiv1.DatabaseRoleReclaimDelete,
				}
				Expect(env.Client.Create(env.Ctx, aristoRole)).To(Succeed())
			})

			By("ensuring the reconciliation of 'ariosto' succeeded", func() {
				roleNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      aristoRole.Name,
				}
				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, roleNamespacedName, aristoRole)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(aristoRole.Status.Applied).Should(HaveValue(BeTrue()))
					g.Expect(aristoRole.Status.Message).Should(BeEmpty())
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})
		})
	})

	Context("in a Namespace to be deleted manually", func() {
		var (
			err            error
			clusterName    string
			roleObjectName string
			namespace      string
		)

		It("will not prevent the deletion of the namespace with lagging finalizers", func() {
			By("setting up the namespace and the cluster", func() {
				process := GinkgoParallelProcess()
				namespace = fmt.Sprintf("declarative-roles-finalizers-%d-%d", process, funk.RandomInt(0, 9999))

				err = namespaces.CreateNamespace(env.Ctx, env.Client, namespace)
				Expect(err).ToNot(HaveOccurred())

				clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, clusterManifest)
				Expect(err).ToNot(HaveOccurred())

				AssertCreateCluster(namespace, clusterName, clusterManifest, env)
			})
			By("creating the role", func() {
				roleManifest := fixturesDir +
					"/declarative_roles/databaserole-with-delete-reclaim-policy.yaml.template"
				roleObjectName, err = yaml.GetResourceNameFromYAML(env.Scheme, roleManifest)
				Expect(err).NotTo(HaveOccurred())
				CreateResourceFromFile(namespace, roleManifest)
			})
			By("ensuring the role is reconciled successfully", func() {
				// get role object
				roleObj := &apiv1.DatabaseRole{}
				roleNamespacedName := types.NamespacedName{
					Namespace: namespace,
					Name:      roleObjectName,
				}
				Eventually(func(g Gomega) {
					err := env.Client.Get(env.Ctx, roleNamespacedName, roleObj)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(roleObj.Status.Applied).Should(HaveValue(BeTrue()))
				}, 300).WithPolling(10 * time.Second).Should(Succeed())
			})
			By("deleting the namespace and making sure it succeeds before timeout", func() {
				err := namespaces.DeleteNamespaceAndWait(env.Ctx, env.Client, namespace, 120)
				Expect(err).ToNot(HaveOccurred())
				// we need to cleanup testing logs adhoc since we are not using a testingNamespace for this test
				err = namespaces.CleanupClusterLogs(namespace, CurrentSpecReport().Failed())
				Expect(err).ToNot(HaveOccurred())
			})
		})
	})
})
