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

package postgres

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudnative-pg/cnpg-i/pkg/postgres"
	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	"github.com/cloudnative-pg/machinery/pkg/image/reference"
	"github.com/cloudnative-pg/machinery/pkg/postgres/version"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/postgres/constants"
	pgconfig "github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	ldapBaseDN          = "ldapBaseDN"
	ldapBindDN          = "ldapBindDN"
	ldapSearchAttribute = "ldapSearchAttribute"
	ldapSearchFilter    = "ldapSearchFilter"
	ldapServer          = "ldapServer"
	ldapPort            = 1234
	ldapScheme          = apiv1.LDAPSchemeLDAP
	ldapPrefix          = "ldapPrefix"
	ldapSuffix          = "ldapSuffix"
)

var _ = Describe("testing the building of the ldap config string", func() {
	ldapPassword := rand.String(12)

	cluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configurationTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{
			PostgresConfiguration: apiv1.PostgresConfiguration{
				LDAP: &apiv1.LDAPConfig{
					BindSearchAuth: &apiv1.LDAPBindSearchAuth{
						BindPassword: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: "testLDAPBindPasswordSecret",
							},
							Key: "key",
						},
						BaseDN:          ldapBaseDN,
						BindDN:          ldapBindDN,
						SearchAttribute: ldapSearchAttribute,
						SearchFilter:    ldapSearchFilter,
					},
					Server: ldapServer,
					Scheme: apiv1.LDAPSchemeLDAP,
					Port:   ldapPort,
					TLS:    true,
				},
			},
		},
	}
	It("returns nil for a cluster with no ldap configuration", func() {
		clusterWithoutLDAP := cluster.DeepCopy()
		clusterWithoutLDAP.Spec.PostgresConfiguration.LDAP.BindSearchAuth = nil
		clusterWithoutLDAP.Spec.PostgresConfiguration.LDAP.BindAsAuth = nil

		str := buildLDAPConfigString(clusterWithoutLDAP, ldapPassword)
		Expect(str).To(Equal(""))
	})
	It("correctly builds a bindSearchAuth string", func() {
		str := buildLDAPConfigString(&cluster, ldapPassword)
		Expect(str).To(Equal(fmt.Sprintf(`host all all 0.0.0.0/0 ldap ldapserver="%s" ldapport=%d `+
			`ldapscheme="%s" ldaptls=1 ldapbasedn="%s" ldapbinddn="%s" `+
			`ldapbindpasswd="%s" ldapsearchfilter="%s" ldapsearchattribute="%s"`,
			ldapServer, ldapPort, ldapScheme, ldapBaseDN,
			ldapBindDN, ldapPassword, ldapSearchFilter, ldapSearchAttribute)))
	})
	It("correctly builds a bindAsAuth string", func() {
		baaCluster := cluster.DeepCopy()
		baaCluster.Spec.PostgresConfiguration.LDAP.BindSearchAuth = nil
		baaCluster.Spec.PostgresConfiguration.LDAP.BindAsAuth = &apiv1.LDAPBindAsAuth{
			Prefix: ldapPrefix,
			Suffix: ldapSuffix,
		}
		str := buildLDAPConfigString(baaCluster, ldapPassword)
		Expect(str).To(Equal(fmt.Sprintf(`host all all 0.0.0.0/0 ldap ldapserver="%s" `+
			`ldapport=%d ldapscheme="%s" ldaptls=1 ldapprefix="%s" ldapsuffix="%s"`,
			ldapServer, ldapPort, ldapScheme, ldapPrefix, ldapSuffix)))
	})
	It("if password contains a newline, ends the line with a backslash and carries on", func() {
		str := buildLDAPConfigString(&cluster, "really\"nasty\npass")
		Expect(strings.Split(str, "\n")).To(HaveLen(2))
		Expect(str).To(Equal(fmt.Sprintf(`host all all 0.0.0.0/0 ldap ldapserver="%s" `+
			`ldapport=%d ldapscheme="%s" ldaptls=1 ldapbasedn="%s" `+
			`ldapbinddn="%s" ldapbindpasswd="really""nasty\`+
			"\n"+
			`pass" ldapsearchfilter="%s" ldapsearchattribute="%s"`,
			ldapServer, ldapPort, ldapScheme, ldapBaseDN, ldapBindDN,
			ldapSearchFilter, ldapSearchAttribute)))
	})
})

var _ = Describe("Test building of the list of temporary tablespaces", func() {
	defaultVersion, defaultVersionErr := version.FromTag(reference.New(versions.DefaultImageName).Tag)
	Expect(defaultVersionErr).ToNot(HaveOccurred())
	defaultMajor := int(defaultVersion.Major())

	clusterWithoutTablespaces := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configurationTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{},
	}

	clusterWithoutTemporaryTablespaces := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configurationTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{
			Tablespaces: []apiv1.TablespaceConfiguration{
				{
					Name:      "data_tablespace",
					Temporary: false,
				},
			},
		},
	}

	clusterWithTemporaryTablespaces := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configurationTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{
			Tablespaces: []apiv1.TablespaceConfiguration{
				{
					Name:      "data_tablespace",
					Temporary: false,
				},
				{
					Name:      "temporary_tablespace",
					Temporary: true,
				},
				{
					Name:      "other_temporary_tablespace",
					Temporary: true,
				},
			},
		},
	}

	It("doesn't set temp_tablespaces if there are no declared tablespaces", func(ctx SpecContext) {
		config, _, err := createPostgresqlConfiguration(
			ctx, &clusterWithoutTablespaces, true, defaultMajor,
			postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).ToNot(ContainSubstring("temp_tablespaces"))
	})

	It("doesn't set temp_tablespaces if there are no temporary tablespaces", func(ctx SpecContext) {
		config, _, err := createPostgresqlConfiguration(
			ctx, &clusterWithoutTemporaryTablespaces, true, defaultMajor,
			postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).ToNot(ContainSubstring("temp_tablespaces"))
	})

	It("sets temp_tablespaces when there are temporary tablespaces", func(ctx SpecContext) {
		config, _, err := createPostgresqlConfiguration(
			ctx, &clusterWithTemporaryTablespaces, true, defaultMajor,
			postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(ContainSubstring("temp_tablespaces = 'other_temporary_tablespace,temporary_tablespace'"))
	})
})

var _ = Describe("recovery_min_apply_delay", func() {
	defaultVersion, err := version.FromTag(reference.New(versions.DefaultImageName).Tag)
	Expect(err).ToNot(HaveOccurred())
	defaultMajor := int(defaultVersion.Major())

	primaryCluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configurationTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{
			ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
				Enabled: ptr.To(false),
				MinApplyDelay: &metav1.Duration{
					Duration: 1 * time.Hour,
				},
			},
		},
	}

	replicaCluster := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configurationTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{
			ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
				Enabled: ptr.To(true),
				MinApplyDelay: &metav1.Duration{
					Duration: 1 * time.Hour,
				},
			},
		},
	}

	replicaClusterWithNoDelay := apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "configurationTest",
			Namespace: "default",
		},

		Spec: apiv1.ClusterSpec{
			ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
				Enabled: ptr.To(true),
			},
		},
	}

	It("do not set recovery_min_apply_delay in primary clusters", func(ctx SpecContext) {
		Expect(primaryCluster.IsReplica()).To(BeFalse())

		config, _, err := createPostgresqlConfiguration(
			ctx, &primaryCluster, true, defaultMajor,
			postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).ToNot(ContainSubstring("recovery_min_apply_delay"))
	})

	It("set recovery_min_apply_delay in replica clusters when set", func(ctx SpecContext) {
		Expect(replicaCluster.IsReplica()).To(BeTrue())

		config, _, err := createPostgresqlConfiguration(
			ctx, &replicaCluster, true, defaultMajor,
			postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(ContainSubstring("recovery_min_apply_delay = '3600s'"))
	})

	It("do not set recovery_min_apply_delay in replica clusters when not set", func(ctx SpecContext) {
		Expect(replicaClusterWithNoDelay.IsReplica()).To(BeTrue())

		config, _, err := createPostgresqlConfiguration(
			ctx, &replicaClusterWithNoDelay, true, defaultMajor,
			postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).ToNot(ContainSubstring("recovery_min_apply_delay"))
	})
})

var _ = Describe("selectAdditionalExtensions", func() {
	steadyStateCluster := func() *apiv1.Cluster {
		return &apiv1.Cluster{
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{
					Extensions: []apiv1.ExtensionConfiguration{{Name: "old-ext"}},
				},
			},
		}
	}
	upgradingCluster := func() *apiv1.Cluster {
		c := steadyStateCluster()
		c.Status.TargetPGDataImageInfo = &apiv1.ImageInfo{
			Extensions: []apiv1.ExtensionConfiguration{{Name: "new-ext"}},
		}
		return c
	}

	It("returns the source-version extensions and steady-state baseDir for non-upgrade ops", func() {
		exts, baseDir, err := selectAdditionalExtensions(steadyStateCluster(), postgres.OperationType_TYPE_UNSPECIFIED)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(HaveLen(1))
		Expect(exts[0].Name).To(Equal("old-ext"))
		Expect(baseDir).To(Equal("/extensions"))
	})

	It("returns the target-version extensions and upgrade baseDir for TYPE_UPGRADE", func() {
		exts, baseDir, err := selectAdditionalExtensions(upgradingCluster(), postgres.OperationType_TYPE_UPGRADE)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(HaveLen(1))
		Expect(exts[0].Name).To(Equal("new-ext"))
		Expect(baseDir).To(Equal("/new-extensions"))
	})

	It("errors when TargetPGDataImageInfo is missing during a TYPE_UPGRADE", func() {
		_, _, err := selectAdditionalExtensions(steadyStateCluster(), postgres.OperationType_TYPE_UPGRADE)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("TargetPGDataImageInfo"))
	})

	It("returns nil and steady-state baseDir when PGDataImageInfo is missing", func() {
		exts, baseDir, err := selectAdditionalExtensions(&apiv1.Cluster{}, postgres.OperationType_TYPE_UNSPECIFIED)
		Expect(err).ToNot(HaveOccurred())
		Expect(exts).To(BeNil())
		Expect(baseDir).To(Equal("/extensions"))
	})
})

var _ = Describe("image volume extensions configuration", func() {
	defaultVersion, err := version.FromTag(reference.New(versions.DefaultImageName).Tag)
	Expect(err).ToNot(HaveOccurred())
	defaultMajor := int(defaultVersion.Major())

	clusterWithExtensions := func(extensions ...apiv1.ExtensionConfiguration) *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "configurationTest", Namespace: "default"},
			Status: apiv1.ClusterStatus{
				PGDataImageInfo: &apiv1.ImageInfo{Extensions: extensions},
			},
		}
	}

	// A Pod whose extension images are not mounted yet, like one waiting for
	// its rollout, must report the same hash as a Pod that mounts them,
	// otherwise the rollout waits forever for the configuration to be uniform
	It("hashes the extension directories requested by the Cluster, even when missing", func(ctx SpecContext) {
		oneExtension, oneExtensionSha, err := createPostgresqlConfiguration(
			ctx, clusterWithExtensions(apiv1.ExtensionConfiguration{Name: "first"}), true, defaultMajor,
			postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())

		twoExtensions, twoExtensionsSha, err := createPostgresqlConfiguration(
			ctx,
			clusterWithExtensions(
				apiv1.ExtensionConfiguration{Name: "first"},
				apiv1.ExtensionConfiguration{Name: "second"},
			),
			true, defaultMajor,
			postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())

		// No extension directory exists here, so both files skip them all and
		// differ only in the hash, which identifies the requested configuration
		Expect(twoExtensions).ToNot(ContainSubstring("/extensions/"))
		Expect(twoExtensions).To(ContainSubstring(fmt.Sprintf("cnpg.config_sha256 = '%s'", twoExtensionsSha)))
		Expect(twoExtensionsSha).ToNot(Equal(oneExtensionSha))
		Expect(twoExtensions).To(Equal(strings.ReplaceAll(oneExtension, oneExtensionSha, twoExtensionsSha)))
	})
})

var _ = Describe("removeMissingDirectories", func() {
	It("removes only the candidates that are missing", func() {
		base := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(base, "pgvector", "share"), 0o750)).To(Succeed())
		config := &pgconfig.PgConfiguration{}
		config.OverwriteConfig(pgconfig.ExtensionControlPath, strings.Join([]string{
			"$system",
			filepath.Join(base, "pgvector", "share"),
			filepath.Join(base, "keycloak-oauth-validator", "share"),
			"/user/defined",
		}, ":"))

		Expect(removeMissingDirectories(config, pgconfig.ExtensionControlPath, []string{
			filepath.Join(base, "pgvector", "share"),
			filepath.Join(base, "keycloak-oauth-validator", "share"),
		})).To(Succeed())
		Expect(config.GetConfig(pgconfig.ExtensionControlPath)).To(Equal(strings.Join([]string{
			"$system",
			filepath.Join(base, "pgvector", "share"),
			"/user/defined",
		}, ":")))
	})

	It("doesn't add a parameter that is not set", func() {
		config := &pgconfig.PgConfiguration{}
		Expect(removeMissingDirectories(config, pgconfig.DynamicLibraryPath, []string{"/missing/lib"})).To(Succeed())
		Expect(config.GetConfigurationParameters()).ToNot(HaveKey(pgconfig.DynamicLibraryPath))
	})
})

var _ = Describe("filterExistingDirectories", func() {
	It("keeps only the paths that are existing directories", func() {
		// keycloak-oauth-validator ships `lib` but no `share`, and
		// `LICENSE` is a file, not a directory
		base := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(base, "pgvector", "share"), 0o750)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(base, "keycloak-oauth-validator", "lib"), 0o750)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(base, "keycloak-oauth-validator", "LICENSE"), nil, 0o600)).To(Succeed())

		paths, err := filterExistingDirectories([]string{
			filepath.Join(base, "pgvector", "share"),
			filepath.Join(base, "keycloak-oauth-validator", "share"),
			filepath.Join(base, "keycloak-oauth-validator", "lib"),
			filepath.Join(base, "keycloak-oauth-validator", "LICENSE"),
			filepath.Join(base, "keycloak-oauth-validator", "LICENSE", "lib"),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(paths).To(Equal([]string{
			filepath.Join(base, "pgvector", "share"),
			filepath.Join(base, "keycloak-oauth-validator", "lib"),
		}))
	})
})

var _ = Describe("the override.conf file writers", func() {
	var pgData string
	const primaryConnInfo = "host=cluster-example-rw user=streaming_replica"

	readOverrideConf := func() string {
		content, err := fileutils.ReadFile(filepath.Join(pgData, constants.PostgresqlOverrideConfigurationFile))
		Expect(err).ToNot(HaveOccurred())
		return string(content)
	}

	BeforeEach(func() {
		pgData = GinkgoT().TempDir()
	})

	It("writes the replica restore_command and the replication slot", func() {
		changed, err := configurePostgresOverrideConfFile(pgData, primaryConnInfo, "_cnpg_test")
		Expect(err).ToNot(HaveOccurred())
		Expect(changed).To(BeTrue())

		content := readOverrideConf()
		Expect(content).To(ContainSubstring("wal-restore"))
		Expect(content).ToNot(ContainSubstring("--rewind"))
		Expect(content).To(ContainSubstring("primary_slot_name = '_cnpg_test'"))
		Expect(content).To(ContainSubstring(primaryConnInfo))
	})

	It("omits the replication slot when the slot name is empty", func() {
		_, err := configurePostgresOverrideConfFile(pgData, primaryConnInfo, "")
		Expect(err).ToNot(HaveOccurred())

		Expect(readOverrideConf()).ToNot(ContainSubstring("primary_slot_name"))
	})

	It("writes a rewind-mode restore_command with no replication slot", func() {
		changed, err := configurePostgresOverrideConfFileForRewind(pgData, primaryConnInfo)
		Expect(err).ToNot(HaveOccurred())
		Expect(changed).To(BeTrue())

		content := readOverrideConf()
		Expect(content).To(ContainSubstring("--rewind %f %p"))
		Expect(content).ToNot(ContainSubstring("primary_slot_name"))
	})

	It("removes the rewind-mode restore_command when demoting after a rewind", func() {
		_, err := configurePostgresOverrideConfFileForRewind(pgData, primaryConnInfo)
		Expect(err).ToNot(HaveOccurred())

		changed, err := UpdateReplicaConfiguration(pgData, primaryConnInfo, "_cnpg_test")
		Expect(err).ToNot(HaveOccurred())
		Expect(changed).To(BeTrue())

		content := readOverrideConf()
		Expect(content).ToNot(ContainSubstring("--rewind"))
		Expect(content).To(ContainSubstring("primary_slot_name = '_cnpg_test'"))
	})
})

var _ = Describe("synchronized_standby_slots", func() {
	newCluster := func() *apiv1.Cluster {
		return &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: apiv1.ClusterSpec{
				ReplicationSlots: &apiv1.ReplicationSlotsConfiguration{
					HighAvailability: &apiv1.ReplicationSlotsHAConfiguration{
						Enabled:                    ptr.To(true),
						SynchronizeLogicalDecoding: true,
					},
				},
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Parameters: map[string]string{"sync_replication_slots": "on"},
				},
			},
			Status: apiv1.ClusterStatus{
				CurrentPrimary: "test-1",
				InstanceNames:  []string{"test-1", "test-2", "test-3"},
			},
		}
	}

	It("lists every replica", func(ctx SpecContext) {
		config, _, err := createPostgresqlConfiguration(
			ctx, newCluster(), true, 17, postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(ContainSubstring("synchronized_standby_slots = '_cnpg_test_2,_cnpg_test_3'"))
	})

	It("skips the replicas that are still bootstrapping", func(ctx SpecContext) {
		cluster := newCluster()
		cluster.Status.InitializingPVC = []string{"test-3"}
		config, _, err := createPostgresqlConfiguration(
			ctx, cluster, true, 17, postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).To(ContainSubstring("synchronized_standby_slots = '_cnpg_test_2'"))
	})

	It("doesn't set the parameter when the only replica is bootstrapping", func(ctx SpecContext) {
		cluster := newCluster()
		cluster.Status.InstanceNames = []string{"test-1", "test-2"}
		cluster.Status.InitializingPVC = []string{"test-2"}
		config, _, err := createPostgresqlConfiguration(
			ctx, cluster, true, 17, postgres.OperationType_TYPE_UNSPECIFIED,
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(config).ToNot(ContainSubstring("synchronized_standby_slots"))
	})
})
