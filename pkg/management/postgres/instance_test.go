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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/fileutils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("testing primary instance methods", Ordered, func() {
	tempDir, err := os.MkdirTemp("", "primary")
	Expect(err).ToNot(HaveOccurred())

	instance := Instance{
		PgData: filepath.Join(tempDir, "/testdata/primary"),
	}

	signalPath := filepath.Join(instance.PgData, "standby.signal")
	postgresOverrideConf := filepath.Join(instance.PgData, "override.conf")
	pgControl := filepath.Join(instance.PgData, "global", "pg_control")
	pgControlOld := pgControl + pgControlFileBackupExtension

	BeforeEach(func() {
		_, err := fileutils.WriteStringToFile(instance.PgData+"/PG_VERSION", "14")
		Expect(err).ToNot(HaveOccurred())
	})

	assertFileExists := func(path, name string) {
		f, err := os.Stat(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(f.Name()).To(Equal(name))
	}

	AfterEach(func() {
		_ = os.Remove(signalPath)
		_ = os.Remove(postgresOverrideConf)
		_ = os.Remove(pgControl)
		_ = os.Remove(pgControlOld)
	})

	It("should correctly recognize a primary instance", func() {
		isPrimary, err := instance.IsPrimary()
		Expect(err).ToNot(HaveOccurred())
		Expect(isPrimary).To(BeTrue())

		_, err = fileutils.WriteStringToFile(signalPath, "")
		Expect(err).ToNot(HaveOccurred())
		isPrimary, err = instance.IsPrimary()
		Expect(err).ToNot(HaveOccurred())
		Expect(isPrimary).To(BeFalse())
	})

	It("should properly demote a primary", func(ctx context.Context) {
		err := instance.Demote(ctx, &apiv1.Cluster{})
		Expect(err).ToNot(HaveOccurred())

		assertFileExists(signalPath, "standby.signal")
		assertFileExists(postgresOverrideConf, "override.conf")
	})

	It("should correctly restore pg_control from the pg_control.old file", func() {
		data := []byte("pgControlFakeData")

		err := fileutils.EnsureParentDirectoryExists(pgControlOld)
		Expect(err).ToNot(HaveOccurred())

		err = os.WriteFile(pgControlOld, data, 0o600)
		Expect(err).ToNot(HaveOccurred())

		err = instance.managePgControlFileBackup()
		Expect(err).ToNot(HaveOccurred())

		assertFileExists(pgControl, "pg_control")
	})

	It("should properly remove pg_control file", func() {
		data := []byte("pgControlFakeData")

		err := fileutils.EnsureParentDirectoryExists(pgControlOld)
		Expect(err).ToNot(HaveOccurred())

		err = os.WriteFile(pgControl, data, 0o600)
		Expect(err).ToNot(HaveOccurred())

		err = instance.removePgControlFileBackup()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail if the pg_control file has issues", func() {
		err := fileutils.EnsureParentDirectoryExists(pgControl)
		Expect(err).ToNot(HaveOccurred())

		err = os.WriteFile(pgControl, nil, 0o600)
		Expect(err).ToNot(HaveOccurred())

		err = os.Chmod(filepath.Join(instance.PgData, "global"), 0o000)
		Expect(err).ToNot(HaveOccurred())

		err = instance.managePgControlFileBackup()
		Expect(err).To(HaveOccurred())

		err = os.Chmod(filepath.Join(instance.PgData, "global"), 0o755) //nolint:gosec
		Expect(err).ToNot(HaveOccurred())

		err = instance.managePgControlFileBackup()
		Expect(err).To(HaveOccurred())
	})

	AfterAll(func() {
		err := fileutils.RemoveDirectoryContent(tempDir)
		Expect(err).ToNot(HaveOccurred())

		err = fileutils.RemoveFile(tempDir)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("testing replica instance methods", Ordered, func() {
	tempDir, err := os.MkdirTemp("", "primary")
	Expect(err).ToNot(HaveOccurred())

	instance := Instance{
		PgData: tempDir + "/testdata/replica",
	}
	signalPath := filepath.Join(instance.PgData, "standby.signal")

	BeforeEach(func() {
		_, err := fileutils.WriteStringToFile(signalPath, "")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should correctly recognize a replica instance", func() {
		isPrimary, err := instance.IsPrimary()
		Expect(err).ToNot(HaveOccurred())
		Expect(isPrimary).To(BeFalse())
	})

	AfterAll(func() {
		err := fileutils.RemoveDirectoryContent(tempDir)
		Expect(err).ToNot(HaveOccurred())

		err = fileutils.RemoveFile(tempDir)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("testing environment variables", func() {
	It("should return the default Socket Directory", func() {
		socketDir := GetSocketDir()
		Expect(socketDir).To(BeEquivalentTo(postgres.SocketDirectory))
	})

	It("should return the default or defined PostgreSQL port", func() {
		pgPort := GetServerPort()
		Expect(pgPort).To(BeEquivalentTo(postgres.ServerPort))

		pgPortEnv := 777
		err := os.Setenv("PGPORT", fmt.Sprintf("%v", pgPortEnv))
		Expect(err).ShouldNot(HaveOccurred())
		pgPort = GetServerPort()
		Expect(pgPort).To(BeEquivalentTo(pgPortEnv))

		err = os.Setenv("PGPORT", "peggie")
		Expect(err).ShouldNot(HaveOccurred())
		pgPort = GetServerPort()
		Expect(pgPort).To(BeEquivalentTo(postgres.ServerPort))
	})
})

var _ = Describe("check atomic bool", func() {
	instance := Instance{}
	instance.mightBeUnavailable.Store(true)

	It("should indicate instance might be unavailable after fencing is set", func() {
		isFenced := instance.IsFenced()
		Expect(isFenced).To(BeFalse())

		instance.SetFencing(true)
		isFenced = instance.IsFenced()
		Expect(isFenced).To(BeTrue())
		unAvailable := instance.MightBeUnavailable()
		Expect(unAvailable).To(BeTrue())
	})

	It("should recognize whether readiness can be checked depending on the setting", func() {
		instance.SetCanCheckReadiness(false)
		canBeChecked := instance.CanCheckReadiness()
		Expect(canBeChecked).To(BeFalse())

		instance.SetCanCheckReadiness(true)
		canBeChecked = instance.CanCheckReadiness()
		Expect(canBeChecked).To(BeTrue())
	})

	It("should recognize whether the instance might be unavailable based on the setting", func() {
		instance.SetMightBeUnavailable(false)
		unAvailable := instance.MightBeUnavailable()
		Expect(unAvailable).To(BeFalse())

		instance.SetMightBeUnavailable(true)
		unAvailable = instance.MightBeUnavailable()
		Expect(unAvailable).To(BeTrue())
	})
})

var _ = Describe("ALTER SYSTEM enable and disable in PostgreSQL <17", func() {
	var instance Instance
	var autoConfFile string

	BeforeEach(func() {
		tmpDir := GinkgoT().TempDir()
		instance.PgData = tmpDir

		autoConfFile = filepath.Join(tmpDir, "postgresql.auto.conf")
		f, err := os.Create(autoConfFile) // nolint: gosec
		Expect(err).ToNot(HaveOccurred())

		err = f.Close()
		Expect(err).ToNot(HaveOccurred())
	})

	It("should be able to enable ALTER SYSTEM", func() {
		err := instance.SetPostgreSQLAutoConfWritable(true)
		Expect(err).ToNot(HaveOccurred())

		info, err := os.Stat(autoConfFile)
		Expect(err).ToNot(HaveOccurred())

		Expect(info.Mode()).To(BeEquivalentTo(0o600))
	})

	It("should be able to disable ALTER SYSTEM", func() {
		err := instance.SetPostgreSQLAutoConfWritable(false)
		Expect(err).ToNot(HaveOccurred())

		info, err := os.Stat(autoConfFile)
		Expect(err).ToNot(HaveOccurred())

		Expect(info.Mode()).To(BeEquivalentTo(0o400))
	})
})

var _ = Describe("buildPostgresEnv", func() {
	var cluster apiv1.Cluster
	var instance Instance

	BeforeEach(func() {
		err := os.Unsetenv("LD_LIBRARY_PATH")
		Expect(err).ToNot(HaveOccurred())

		cluster = apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "cluster-example",
				Namespace: "default",
			},
			Spec: apiv1.ClusterSpec{
				PostgresConfiguration: apiv1.PostgresConfiguration{
					Extensions: []apiv1.ExtensionConfiguration{
						{
							Name: "foo",
							ImageVolumeSource: corev1.ImageVolumeSource{
								Reference: "foo:dev",
							},
						},
						{
							Name: "bar",
							ImageVolumeSource: corev1.ImageVolumeSource{
								Reference: "bar:dev",
							},
						},
					},
				},
			},
		}
		instance.Cluster = &cluster
	})

	Context("Extensions enabled, LD_LIBRARY_PATH undefined", func() {
		It("should be empty by default", func() {
			ldLibraryPath := getLibraryPathFromEnv(instance.buildPostgresEnv())
			Expect(ldLibraryPath).To(BeEmpty())
		})
	})

	Context("Extensions enabled, LD_LIBRARY_PATH defined", func() {
		const (
			path1 = postgres.ExtensionsBaseDirectory + "/foo/syslib"
			path2 = postgres.ExtensionsBaseDirectory + "/foo/sample"
			path3 = postgres.ExtensionsBaseDirectory + "/bar/syslib"
			path4 = postgres.ExtensionsBaseDirectory + "/bar/sample"
		)
		finalPaths := strings.Join([]string{path1, path2, path3, path4}, ":")

		BeforeEach(func() {
			cluster.Spec.PostgresConfiguration.Extensions[0].LdLibraryPath = []string{"/syslib", "sample/"}
			cluster.Spec.PostgresConfiguration.Extensions[1].LdLibraryPath = []string{"./syslib", "./sample/"}
		})

		It("should be defined", func() {
			ldLibraryPath := getLibraryPathFromEnv(instance.buildPostgresEnv())
			Expect(ldLibraryPath).To(Equal(fmt.Sprintf("LD_LIBRARY_PATH=%s", finalPaths)))
		})
		It("should retain existing values", func() {
			GinkgoT().Setenv("LD_LIBRARY_PATH", "/my/library/path")

			ldLibraryPath := getLibraryPathFromEnv(instance.buildPostgresEnv())
			Expect(ldLibraryPath).To(BeEquivalentTo(fmt.Sprintf("LD_LIBRARY_PATH=/my/library/path:%s", finalPaths)))
		})
	})

	Context("Extensions disabled", func() {
		BeforeEach(func() {
			cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{}
		})
		It("LD_LIBRARY_PATH should be empty", func() {
			ldLibraryPath := getLibraryPathFromEnv(instance.buildPostgresEnv())
			Expect(ldLibraryPath).To(BeEmpty())
		})
	})
})

func getLibraryPathFromEnv(envs []string) string {
	var ldLibraryPath string

	for i := len(envs) - 1; i >= 0; i-- {
		if strings.HasPrefix(envs[i], "LD_LIBRARY_PATH=") {
			ldLibraryPath = envs[i]
			break
		}
	}

	return ldLibraryPath
}

var _ = Describe("GetPrimaryConnInfo", func() {
	var instance *Instance

	BeforeEach(func() {
		instance = &Instance{
			Cluster: &apiv1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster",
				},
			},
		}
		instance.WithPodName("test-cluster-1").WithClusterName("test-cluster")
	})

	AfterEach(func() {
		err := os.Unsetenv("CNPG_STANDBY_TCP_USER_TIMEOUT")
		Expect(err).ToNot(HaveOccurred())
	})

	It("should use default 5000ms tcp_user_timeout when env var is not set", func() {
		err := os.Unsetenv("CNPG_STANDBY_TCP_USER_TIMEOUT")
		Expect(err).ToNot(HaveOccurred())
		connInfo := instance.GetPrimaryConnInfo()
		Expect(connInfo).To(ContainSubstring("tcp_user_timeout='5000'"))
	})

	It("should use custom tcp_user_timeout when env var is set", func() {
		err := os.Setenv("CNPG_STANDBY_TCP_USER_TIMEOUT", "10000")
		Expect(err).ToNot(HaveOccurred())
		connInfo := instance.GetPrimaryConnInfo()
		Expect(connInfo).To(ContainSubstring("tcp_user_timeout='10000'"))
	})

	It("should allow setting tcp_user_timeout to 0 explicitly", func() {
		err := os.Setenv("CNPG_STANDBY_TCP_USER_TIMEOUT", "0")
		Expect(err).ToNot(HaveOccurred())
		connInfo := instance.GetPrimaryConnInfo()
		Expect(connInfo).To(ContainSubstring("tcp_user_timeout='0'"))
	})

	It("should escape single quotes in tcp_user_timeout value", func() {
		err := os.Setenv("CNPG_STANDBY_TCP_USER_TIMEOUT", "5000'injection")
		Expect(err).ToNot(HaveOccurred())
		connInfo := instance.GetPrimaryConnInfo()
		Expect(connInfo).To(ContainSubstring("tcp_user_timeout='5000\\'injection'"))
	})

	It("should escape backslashes in tcp_user_timeout value", func() {
		err := os.Setenv("CNPG_STANDBY_TCP_USER_TIMEOUT", "5000\\test")
		Expect(err).ToNot(HaveOccurred())
		connInfo := instance.GetPrimaryConnInfo()
		Expect(connInfo).To(ContainSubstring("tcp_user_timeout='5000\\\\test'"))
	})
})

var _ = Describe("NewInstance", func() {
	It("should initialize Cluster as non-nil", func() {
		instance := NewInstance()
		Expect(instance.Cluster).ToNot(BeNil())
	})

	It("should generate a non-empty SessionID", func() {
		instance := NewInstance()
		Expect(instance.SessionID).ToNot(BeEmpty())
	})
})

var _ = Describe("RequiresDesignatedPrimaryTransition", func() {
	var instance *Instance
	var cluster *apiv1.Cluster
	var tempDir string

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "test-primary")
		Expect(err).ToNot(HaveOccurred())

		instance = NewInstance()
		instance.PgData = tempDir

		cluster = &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-cluster",
			},
			Spec: apiv1.ClusterSpec{
				ReplicaCluster: &apiv1.ReplicaClusterConfiguration{
					Enabled: ptr.To(true),
					Source:  "external-cluster",
				},
			},
		}
	})

	AfterEach(func() {
		err := os.RemoveAll(tempDir)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should return false when cluster is not a replica", func() {
		cluster.Spec.ReplicaCluster = nil
		instance.Cluster = cluster
		result := instance.RequiresDesignatedPrimaryTransition()
		Expect(result).To(BeFalse())
	})

	It("should return false when transition is not requested", func() {
		instance.Cluster = cluster
		// No condition set means transition is not requested
		result := instance.RequiresDesignatedPrimaryTransition()
		Expect(result).To(BeFalse())
	})

	It("should return false when instance is not fenced and not unavailable", func() {
		instance.Cluster = cluster
		instance.SetFencing(false)
		instance.SetMightBeUnavailable(false)

		// Set the condition to request transition
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   "ReplicaClusterDesignatedPrimaryTransition",
			Status: metav1.ConditionFalse,
			Reason: "Test",
		})

		result := instance.RequiresDesignatedPrimaryTransition()
		Expect(result).To(BeFalse())
	})

	It("should return true when all conditions are met for fenced primary", func() {
		instance.Cluster = cluster
		instance.SetFencing(true)
		instance.WithPodName("test-cluster-1")

		// Set CurrentPrimary to this instance
		cluster.Status.CurrentPrimary = "test-cluster-1"

		// Set the condition to request transition
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   "ReplicaClusterDesignatedPrimaryTransition",
			Status: metav1.ConditionFalse,
			Reason: "Test",
		})

		result := instance.RequiresDesignatedPrimaryTransition()
		Expect(result).To(BeTrue())
	})

	It("should return true when all conditions are met for unavailable primary", func() {
		instance.Cluster = cluster
		instance.SetMightBeUnavailable(true)
		instance.WithPodName("test-cluster-1")

		// Set CurrentPrimary to this instance
		cluster.Status.CurrentPrimary = "test-cluster-1"

		// Set the condition to request transition
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   "ReplicaClusterDesignatedPrimaryTransition",
			Status: metav1.ConditionFalse,
			Reason: "Test",
		})

		result := instance.RequiresDesignatedPrimaryTransition()
		Expect(result).To(BeTrue())
	})

	It("should return false when CurrentPrimary is different", func() {
		instance.Cluster = cluster
		instance.SetFencing(true)
		instance.WithPodName("test-cluster-2")

		// Set CurrentPrimary to a different instance
		cluster.Status.CurrentPrimary = "test-cluster-1"

		// Set the condition to request transition
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   "ReplicaClusterDesignatedPrimaryTransition",
			Status: metav1.ConditionFalse,
			Reason: "Test",
		})

		result := instance.RequiresDesignatedPrimaryTransition()
		Expect(result).To(BeFalse())
	})
})
