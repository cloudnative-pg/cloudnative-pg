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
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/thoas/go-funk"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
	"github.com/cloudnative-pg/cloudnative-pg/tests"
	clusterasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/cluster"
	pgasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/postgres"
	secretsasserts "github.com/cloudnative-pg/cloudnative-pg/tests/internal/asserts/secrets"
	"github.com/cloudnative-pg/cloudnative-pg/tests/internal/resources"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	podutils "github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/services"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type MatchGrants struct {
	expectedGrants []apiv1.RoleGrant
}

func (m MatchGrants) Match(actual any) (success bool, err error) {
	roleGrantStrings, ok := actual.([]string)
	if !ok {
		return false, fmt.Errorf("MatchGrants expects a list of strings")
	}

	var roleGrants []apiv1.RoleGrant
	for _, roleGrantString := range roleGrantStrings {
		roleGrantParts := strings.Split(roleGrantString, "|")
		if len(roleGrantParts) != 4 {
			return false, fmt.Errorf("MatchGrants can only match against results with 4 columns, got %d (%v)", len(roleGrantParts), roleGrantParts)
		}

		adminOption, err := strconv.ParseBool(roleGrantParts[1])
		if err != nil {
			return false, fmt.Errorf("Couldn't convert %v to boolean admin option", roleGrantParts[1])
		}

		inheritOption, err := strconv.ParseBool(roleGrantParts[2])
		if err != nil {
			return false, fmt.Errorf("Couldn't convert %v to boolean inherit option", roleGrantParts[2])
		}

		setOption, err := strconv.ParseBool(roleGrantParts[3])
		if err != nil {
			return false, fmt.Errorf("Couldn't convert %v to boolean set option", roleGrantParts[3])
		}

		roleGrants = append(roleGrants, apiv1.RoleGrant{
			Name:    roleGrantParts[0],
			Admin:   &adminOption,
			Inherit: &inheritOption,
			Set:     &setOption,
		})
	}

	sortByName := func(x, y apiv1.RoleGrant) int {
		if x.Name < y.Name {
			return -1
		}
		if x.Name > y.Name {
			return 1
		}
		return 0
	}

	slices.SortStableFunc(roleGrants, sortByName)
	slices.SortStableFunc(m.expectedGrants, sortByName)

	if !slices.EqualFunc(roleGrants, m.expectedGrants, func(a, b apiv1.RoleGrant) bool {
		return a.Name == b.Name &&
			(a.Admin == nil || b.Admin == nil || *a.Admin == *b.Admin) &&
			(a.Inherit == nil || b.Inherit == nil || *a.Inherit == *b.Inherit) &&
			(a.Set == nil || b.Set == nil || *a.Set == *b.Set)
	}) {
		return false, fmt.Errorf("%v didn't match %v", roleGrants, m.expectedGrants)
	}
	return true, nil
}

func (m MatchGrants) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected %v to match %v", actual, m.expectedGrants)
}

func (m MatchGrants) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected %v not to match %v", actual, m.expectedGrants)
}

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
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
			})
		})

		assertInRoles := func(namespace, primaryPod, roleName string, expectedInRoles []string, expectedRoleGrants []apiv1.RoleGrant) {
			var expectedRoles []apiv1.RoleGrant
			for _, role := range expectedRoleGrants {
				expectedRoles = append(expectedRoles, role)
			}
			for _, roleName := range expectedInRoles {
				expectedRoles = append(expectedRoles, apiv1.RoleGrant{Name: roleName})
			}

			Eventually(func() []string {
				var rolesInDB []string
				query := `SELECT
						pg_catalog.pg_get_userbyid(am.roleid) ||
						'|' ||
						am.admin_option ||
						'|' ||
						am.inherit_option ||
						'|' ||
						am.set_option
					FROM pg_auth_members am
					JOIN pg_roles child_role ON am.member = child_role.oid
					WHERE child_role.rolname = ` + pq.QuoteLiteral(roleName)
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
				rolesInDB = strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
				slices.Sort(rolesInDB)
				return rolesInDB
			}, 30).Should(MatchGrants{expectedRoles})
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

			query := fmt.Sprintf("SELECT true FROM pg_catalog.pg_roles WHERE rolname=%s and rolcanlogin=%v and rolsuper=%v "+
				"and rolcreatedb=%v and rolcreaterole=%v and rolinherit is %s and rolreplication=%v "+
				"and rolbypassrls=%v and rolconnlimit=%v", pq.QuoteLiteral(role.Spec.Name), role.Spec.Login, role.Spec.Superuser,
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

			if role.Spec.ValidUntil != nil {
				validUntilQuery := fmt.Sprintf(
					"SELECT rolvaliduntil=%s FROM pg_catalog.pg_authid WHERE rolname=%s",
					pq.QuoteLiteral(role.Spec.ValidUntil.UTC().Format("2006-01-02T15:04:05+00")),
					pq.QuoteLiteral(role.Spec.Name))
				Eventually(func(g Gomega) {
					stdout, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{Namespace: namespace, PodName: primaryPod},
						"postgres", validUntilQuery)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(strings.TrimSpace(stdout)).Should(Equal("t"), "validUntil mismatch")
				}, 30).Should(Succeed())
			}

			if role.Spec.Comment != "" {
				commentQuery := fmt.Sprintf(
					"SELECT pg_catalog.shobj_description(oid, 'pg_authid') FROM pg_catalog.pg_authid WHERE rolname=%s",
					pq.QuoteLiteral(role.Spec.Name))
				Eventually(func(g Gomega) {
					stdout, _, err := exec.QueryInInstancePod(
						env.Ctx, env.Client, env.Interface, env.RestClientConfig,
						exec.PodLocator{Namespace: namespace, PodName: primaryPod},
						"postgres", commentQuery)
					g.Expect(err).ToNot(HaveOccurred())
					g.Expect(strings.TrimSpace(stdout)).Should(Equal(role.Spec.Comment), "comment mismatch")
				}, 30).Should(Succeed())
			}
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
				resources.CreateResourceFromFile(env, namespace, roleManifest)
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

				Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryPodInfo, postgres.PostgresDBName,
					roleExistsQuery(role.Spec.Name), "t"), 30).Should(Succeed())

				assertRoleHasExpectedFields(namespace, primaryPodInfo.Name, role)
			})

			By("verifying new role has been created with the correct groups", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryPodInfo, postgres.PostgresDBName,
					roleExistsQuery(role.Spec.Name), "t"), 30).Should(Succeed())

				assertInRoles(namespace, primaryPodInfo.Name, role.Spec.Name, role.Spec.InRoles, role.Spec.RoleGrants)
			})

			By("verifying that changing the roles roleGrants works", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryPodInfo, postgres.PostgresDBName,
					roleExistsQuery(role.Spec.Name), "t"), 30).Should(Succeed())

				assertInRoles(namespace, primaryPodInfo.Name, role.Spec.Name, role.Spec.InRoles, role.Spec.RoleGrants)
			})

			By("removing the Role object", func() {
				Expect(objects.Delete(env.Ctx, env.Client, &role)).To(Succeed())
			})

			By("verifying the retention policy in the postgres cluster", func() {
				primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
				Expect(err).ToNot(HaveOccurred())

				Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryPodInfo, postgres.PostgresDBName,
					roleExistsQuery(role.Spec.Name), boolPGOutput(retainOnDeletion)), 30).Should(Succeed())
			})
		}

		When("Role CR reclaim policy is set to delete", func() {
			It("can manage a declarative role and delete it in Postgres", func() {
				roleManifest := fixturesDir +
					"/declarative_roles/databaserole-with-delete-reclaim-policy.yaml.template"
				assertTestDeclarativeRole(roleManifest, false)
			})
		})

		When("Role CR reclaim policy is set to retain", func() {
			It("can manage a declarative role and release it", func() {
				roleManifest := fixturesDir + "/declarative_roles/databaserole.yaml.template"
				assertTestDeclarativeRole(roleManifest, true)
			})
		})

		When("Role CR attributes are updated", func() {
			It("applies the updated attributes in PostgreSQL", func() {
				var role *apiv1.DatabaseRole
				pgRoleName := fmt.Sprintf("update-attrs-%d", funk.RandomInt(0, 9999))

				By("creating a role with initial attributes", func() {
					initialValidUntil := metav1.NewTime(time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC))
					role = &apiv1.DatabaseRole{
						ObjectMeta: metav1.ObjectMeta{
							Name:      fmt.Sprintf("role-%s", pgRoleName),
							Namespace: namespace,
						},
						Spec: apiv1.DatabaseRoleSpec{
							RoleConfiguration: apiv1.RoleConfiguration{
								Name:            pgRoleName,
								Ensure:          apiv1.EnsurePresent,
								Login:           true,
								CreateDB:        false,
								ConnectionLimit: -1,
								ValidUntil:      &initialValidUntil,
								Comment:         "initial comment",
							},
							ClusterRef: corev1.LocalObjectReference{
								Name: clusterName,
							},
							ReclaimPolicy: apiv1.DatabaseRoleReclaimDelete,
						},
					}
					Expect(env.Client.Create(env.Ctx, role)).To(Succeed())
				})

				By("ensuring the role is reconciled", func() {
					roleNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      role.Name,
					}
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, roleNamespacedName, role)
						g.Expect(err).ToNot(HaveOccurred())
						g.Expect(role.Status.Applied).Should(HaveValue(BeTrue()))
					}, 300).WithPolling(10 * time.Second).Should(Succeed())
				})

				By("verifying initial attributes in PostgreSQL", func() {
					primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					assertRoleHasExpectedFields(namespace, primaryPodInfo.Name, *role)
				})

				By("updating the role attributes", func() {
					roleNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      role.Name,
					}
					Expect(env.Client.Get(env.Ctx, roleNamespacedName, role)).To(Succeed())
					oldRole := role.DeepCopy()
					role.Spec.Login = false
					role.Spec.CreateDB = true
					role.Spec.ConnectionLimit = 5
					updatedValidUntil := metav1.NewTime(time.Date(2050, 6, 15, 12, 0, 0, 0, time.UTC))
					role.Spec.ValidUntil = &updatedValidUntil
					role.Spec.Comment = "updated comment"
					Expect(env.Client.Patch(env.Ctx, role, client.MergeFrom(oldRole))).To(Succeed())
				})

				By("verifying updated attributes in PostgreSQL", func() {
					primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					Eventually(func(g Gomega) {
						roleNamespacedName := types.NamespacedName{
							Namespace: namespace,
							Name:      role.Name,
						}
						g.Expect(env.Client.Get(env.Ctx, roleNamespacedName, role)).To(Succeed())
						g.Expect(role.Status.Applied).Should(HaveValue(BeTrue()))
						g.Expect(role.Status.ObservedGeneration).Should(Equal(role.Generation))
					}, 300).WithPolling(10 * time.Second).Should(Succeed())

					assertRoleHasExpectedFields(namespace, primaryPodInfo.Name, *role)
				})

				By("cleaning up", func() {
					Expect(objects.Delete(env.Ctx, env.Client, role)).To(Succeed())
				})
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

				By("deleting the conflicting Role CR", func() {
					Expect(objects.Delete(env.Ctx, env.Client, secondRole)).To(Succeed())
					roleNamespacedName := types.NamespacedName{
						Namespace: namespace,
						Name:      secondRole.Name,
					}
					Eventually(func(g Gomega) {
						err := env.Client.Get(env.Ctx, roleNamespacedName, &apiv1.DatabaseRole{})
						g.Expect(apierrs.IsNotFound(err)).To(BeTrue())
					}, 120).WithPolling(5 * time.Second).Should(Succeed())
				})

				By("verifying the role managed by the surviving CR was not dropped", func() {
					primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryPodInfo, postgres.PostgresDBName,
						roleExistsQuery(pgRoleName), "t"), 30).Should(Succeed())
				})

				By("deleting the managing Role CR and verifying the role is dropped", func() {
					Expect(objects.Delete(env.Ctx, env.Client, firstRole)).To(Succeed())
					primaryPodInfo, err := clusterutils.GetPrimary(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					Eventually(pgasserts.QueryMatchExpectationPredicate(env, primaryPodInfo, postgres.PostgresDBName,
						roleExistsQuery(pgRoleName), "f"), 60).Should(Succeed())
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
						g.Expect(role.Status.SecretResourceVersion).ShouldNot(BeZero())
					}, 300).WithPolling(10 * time.Second).Should(Succeed())
				})

				By("checking if we can connect to PostgreSQL using specified password", func() {
					rwService := services.GetReadWriteServiceName(clusterName)
					pgasserts.AssertConnection(env, namespace, rwService, postgres.PostgresDBName, pgRoleName, initialPassword)
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
					pgasserts.AssertConnection(env, namespace, rwService, postgres.PostgresDBName, pgRoleName, newPassword)
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
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
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

	Context("with clientCertificate enabled", Ordered, func() {
		const (
			certClusterManifest = fixturesDir + "/declarative_roles/cluster-with-cert-auth.yaml.template"
			namespacePrefix     = "declarative-roles-cert"
		)
		var (
			clusterName, namespace string
			err                    error
		)

		BeforeAll(func() {
			namespace, err = env.CreateUniqueTestNamespace(env.Ctx, env.Client, namespacePrefix)
			Expect(err).ToNot(HaveOccurred())

			clusterName, err = yaml.GetResourceNameFromYAML(env.Scheme, certClusterManifest)
			Expect(err).ToNot(HaveOccurred())

			By("setting up cluster with cert auth HBA", func() {
				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, certClusterManifest)
			})
		})

		It("issues a cert Secret, allows connection, cleans up on opt-out and deletion",
			Label(tests.LabelDeclarativeDatabaseRoles), func() {
				const roleCRName = "role-app"

				var role *apiv1.DatabaseRole
				By("creating a DatabaseRole for the 'app' user with clientCertificate enabled", func() {
					role = &apiv1.DatabaseRole{
						ObjectMeta: metav1.ObjectMeta{
							Name:      roleCRName,
							Namespace: namespace,
						},
						Spec: apiv1.DatabaseRoleSpec{
							RoleConfiguration: apiv1.RoleConfiguration{
								Name:  "app",
								Login: true,
							},
							ClusterRef:    corev1.LocalObjectReference{Name: clusterName},
							ReclaimPolicy: apiv1.DatabaseRoleReclaimRetain,
							ClientCertificate: &apiv1.ClientCertificateConfiguration{
								Enabled: ptr.To(true),
							},
						},
					}
					Expect(env.Client.Create(env.Ctx, role)).To(Succeed())
				})

				certSecretName := role.GetClientCertSecretName()

				By("waiting for the cert Secret and status.clientCertificate.expiration to be set", func() {
					roleKey := types.NamespacedName{Namespace: namespace, Name: roleCRName}
					Eventually(func(g Gomega) {
						g.Expect(env.Client.Get(env.Ctx, roleKey, role)).To(Succeed())
						g.Expect(role.Status.ClientCertificate).NotTo(BeNil())
						g.Expect(role.Status.ClientCertificate.Expiration).NotTo(BeEmpty())
						g.Expect(role.Status.ClientCertificate.Message).To(BeEmpty())
					}, 120).WithPolling(5 * time.Second).Should(Succeed())

					var certSecret corev1.Secret
					Expect(env.Client.Get(env.Ctx, types.NamespacedName{
						Namespace: namespace, Name: certSecretName,
					}, &certSecret)).To(Succeed())
					Expect(certSecret.Data).To(HaveKey("tls.crt"))
					Expect(certSecret.Data).To(HaveKey("tls.key"))
				})

				By("connecting to PostgreSQL using the generated client certificate", func() {
					cluster, err := clusterutils.Get(env.Ctx, env.Client, namespace, clusterName)
					Expect(err).ToNot(HaveOccurred())

					var secretMode int32 = 0o600
					seccompProfile := &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
					pod := corev1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Namespace: namespace,
							Name:      "cert-verify-app",
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "secret-volume-root-ca",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  cluster.GetClientCASecretName(),
											DefaultMode: &secretMode,
										},
									},
								},
								{
									Name: "secret-volume-tls",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName:  certSecretName,
											DefaultMode: &secretMode,
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Name:  "cert-verify-app",
									Image: "ghcr.io/cloudnative-pg/webtest:1.7.0",
									VolumeMounts: []corev1.VolumeMount{
										{Name: "secret-volume-root-ca", MountPath: "/etc/secrets/ca"},
										{Name: "secret-volume-tls", MountPath: "/etc/secrets/tls"},
									},
									SecurityContext: &corev1.SecurityContext{
										AllowPrivilegeEscalation: ptr.To(false),
										SeccompProfile:           seccompProfile,
									},
								},
							},
							SecurityContext: &corev1.PodSecurityContext{
								SeccompProfile: seccompProfile,
							},
						},
					}
					Expect(podutils.CreateAndWaitForReady(env.Ctx, env.Client, &pod, 240)).To(Succeed())
					secretsasserts.AssertSSLVerifyFullDBConnectionFromAppPod(env, namespace, clusterName, pod)
				})

				By("deleting the cert Secret when clientCertificate is disabled", func() {
					roleKey := types.NamespacedName{Namespace: namespace, Name: roleCRName}
					Expect(env.Client.Get(env.Ctx, roleKey, role)).To(Succeed())
					oldRole := role.DeepCopy()
					role.Spec.ClientCertificate.Enabled = ptr.To(false)
					Expect(env.Client.Patch(env.Ctx, role, client.MergeFrom(oldRole))).To(Succeed())

					Eventually(func(g Gomega) {
						var secret corev1.Secret
						err := env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace, Name: certSecretName,
						}, &secret)
						g.Expect(err).To(MatchError(ContainSubstring("not found")))

						g.Expect(env.Client.Get(env.Ctx, roleKey, role)).To(Succeed())
						g.Expect(role.Status.ClientCertificate).To(BeNil())
					}, 60).WithPolling(5 * time.Second).Should(Succeed())
				})

				By("garbage-collecting the cert Secret when the DatabaseRole is deleted", func() {
					// Re-enable cert issuance so a secret exists before deletion.
					roleKey := types.NamespacedName{Namespace: namespace, Name: roleCRName}
					Expect(env.Client.Get(env.Ctx, roleKey, role)).To(Succeed())
					oldRole := role.DeepCopy()
					role.Spec.ClientCertificate.Enabled = ptr.To(true)
					Expect(env.Client.Patch(env.Ctx, role, client.MergeFrom(oldRole))).To(Succeed())

					Eventually(func(g Gomega) {
						var secret corev1.Secret
						g.Expect(env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace, Name: certSecretName,
						}, &secret)).To(Succeed())
					}, 60).WithPolling(5 * time.Second).Should(Succeed())

					Expect(objects.Delete(env.Ctx, env.Client, role)).To(Succeed())

					Eventually(func(g Gomega) {
						var secret corev1.Secret
						err := env.Client.Get(env.Ctx, types.NamespacedName{
							Namespace: namespace, Name: certSecretName,
						}, &secret)
						g.Expect(err).To(MatchError(ContainSubstring("not found")))
					}, 60).WithPolling(5 * time.Second).Should(Succeed())
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

				clusterasserts.AssertCreateCluster(env, testTimeouts, namespace, clusterName, clusterManifest)
			})
			By("creating the role", func() {
				roleManifest := fixturesDir +
					"/declarative_roles/databaserole-with-delete-reclaim-policy.yaml.template"
				roleObjectName, err = yaml.GetResourceNameFromYAML(env.Scheme, roleManifest)
				Expect(err).NotTo(HaveOccurred())
				resources.CreateResourceFromFile(env, namespace, roleManifest)
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

func roleExistsQuery(roleName string) string {
	return fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM pg_catalog.pg_roles WHERE rolname='%v')", roleName)
}

func boolPGOutput(expectedValue bool) string {
	stringExpectedValue := "f"
	if expectedValue {
		stringExpectedValue = "t"
	}
	return stringExpectedValue
}
