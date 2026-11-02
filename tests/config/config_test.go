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

package config

import (
	"os"
	"path/filepath"
	"sync"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func writeConfigFile(content string) string {
	path := filepath.Join(GinkgoT().TempDir(), "config.yaml")
	Expect(os.WriteFile(path, []byte(content), 0o600)).To(Succeed())
	return path
}

var _ = Describe("Load", func() {
	It("loads every section of a full configuration file", func() {
		cfg, err := Load(writeConfigFile(`
postgres:
  image: registry.example.com/postgresql:18.1
  preRollingUpdateImage: registry.example.com/postgresql:18.0
  imageRepository: registry.example.com/postgresql
  postgisImageRepository: registry.example.com/postgis
storage:
  storageClass: standard
  csiStorageClass: csi-hostpath-sc
  volumeSnapshotClass: csi-hostpath-snapclass
cloudVendor: aks
depth: 1
labelFilter: "backup || basic"
skipUpgradeSuite: true
timeouts:
  failover: 120
deployment:
  method: helm
  barmanPluginVersion: release
  barmanPluginVersionResolved: v0.7.0
registryPullSecret:
  server: registry.example.com
  username: user
  password: secret
azure:
  storageAccount: account
  storageKey: key
  blobContainer: container
preserveNamespaces:
  - keepme
branchName: release/v1.28
majorUpgrade:
  imageRegistry: registry.example.com/trunk
  standardSuffix: "-standard-forky"
  minimalSuffix: "-minimal-forky"
  systemSuffix: "-system-forky"
  postgisSuffix: "-postgis-forky"
  skipArchiveScenario: true
`))
		Expect(err).ToNot(HaveOccurred())

		Expect(cfg.Postgres.Image).To(Equal("registry.example.com/postgresql:18.1"))
		Expect(cfg.Postgres.PreRollingUpdateImage).To(Equal("registry.example.com/postgresql:18.0"))
		Expect(cfg.Postgres.ImageRepository).To(Equal("registry.example.com/postgresql"))
		Expect(cfg.Postgres.PostGISImageRepository).To(Equal("registry.example.com/postgis"))
		Expect(cfg.Storage.StorageClass).To(Equal("standard"))
		Expect(cfg.Storage.CSIStorageClass).To(Equal("csi-hostpath-sc"))
		Expect(cfg.Storage.VolumeSnapshotClass).To(Equal("csi-hostpath-snapclass"))
		Expect(cfg.CloudVendor).To(Equal("aks"))
		Expect(cfg.Depth).To(HaveValue(Equal(1)))
		Expect(cfg.LabelFilter).To(Equal("backup || basic"))
		Expect(cfg.SkipUpgradeSuite).To(BeTrue())
		Expect(cfg.Timeouts).To(Equal(map[string]int{"failover": 120}))
		Expect(cfg.Deployment.Method).To(Equal("helm"))
		Expect(cfg.Deployment.BarmanPluginVersion).To(Equal("release"))
		Expect(cfg.Deployment.BarmanPluginVersionResolved).To(Equal("v0.7.0"))
		Expect(cfg.RegistryPullSecret.Server).To(Equal("registry.example.com"))
		Expect(cfg.RegistryPullSecret.Username).To(Equal("user"))
		Expect(cfg.RegistryPullSecret.Password).To(Equal("secret"))
		Expect(cfg.Azure.StorageAccount).To(Equal("account"))
		Expect(cfg.Azure.StorageKey).To(Equal("key"))
		Expect(cfg.Azure.BlobContainer).To(Equal("container"))
		Expect(cfg.PreserveNamespaces).To(ConsistOf("keepme"))
		Expect(cfg.BranchName).To(Equal("release/v1.28"))
		Expect(cfg.MajorUpgrade.ImageRegistry).To(Equal("registry.example.com/trunk"))
		Expect(cfg.MajorUpgrade.StandardSuffix).To(Equal("-standard-forky"))
		Expect(cfg.MajorUpgrade.SkipArchiveScenario).To(BeTrue())
	})

	It("applies the defaults to an empty file", func() {
		cfg, err := Load(writeConfigFile(""))
		Expect(err).ToNot(HaveOccurred())

		Expect(cfg.Postgres.Image).To(Equal(versions.DefaultImageName))
		Expect(cfg.Postgres.PreRollingUpdateImage).To(BeEmpty())
		Expect(cfg.Postgres.ImageRepository).To(Equal("ghcr.io/cloudnative-pg/postgresql"))
		Expect(cfg.Postgres.PostGISImageRepository).To(Equal("ghcr.io/cloudnative-pg/postgis"))
		Expect(cfg.CloudVendor).To(Equal(VendorKind))
		Expect(cfg.Depth).To(BeNil())
		Expect(cfg.LabelFilter).To(BeEmpty())
		Expect(cfg.SkipUpgradeSuite).To(BeFalse())
		Expect(cfg.Storage.StorageClass).To(BeEmpty())
	})

	It("keeps an explicit zero depth distinct from an absent one", func() {
		cfg, err := Load(writeConfigFile("depth: 0"))
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg.Depth).To(HaveValue(Equal(0)))
	})

	It("rejects unknown top-level fields", func() {
		_, err := Load(writeConfigFile("testDepth: 2"))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("testDepth"))
	})

	It("rejects unknown nested fields", func() {
		_, err := Load(writeConfigFile("postgres:\n  imagee: foo"))
		Expect(err).To(HaveOccurred())
	})

	It("rejects values of the wrong type", func() {
		_, err := Load(writeConfigFile("depth: high"))
		Expect(err).To(HaveOccurred())
	})

	It("rejects malformed YAML", func() {
		_, err := Load(writeConfigFile("::not yaml::"))
		Expect(err).To(HaveOccurred())
	})

	It("reports a missing file with os.ErrNotExist", func() {
		_, err := Load(filepath.Join(GinkgoT().TempDir(), "missing.yaml"))
		Expect(err).To(MatchError(os.ErrNotExist))
	})
})

var _ = Describe("Current", func() {
	AfterEach(func() {
		Set(nil)
	})

	It("returns the default configuration when nothing has been loaded", func() {
		Set(nil)
		Expect(Current().Postgres.Image).To(Equal(versions.DefaultImageName))
	})

	It("returns the installed configuration", func() {
		cfg := NewDefault()
		cfg.CloudVendor = "gke"
		Set(cfg)
		Expect(Current().CloudVendor).To(Equal("gke"))
	})
})

var _ = Describe("template variables", func() {
	It("registers and returns variables", func() {
		SetTemplateVariable("SNAPSHOT_NAME_PGDATA", "snap-1")
		Expect(TemplateVariables()).To(HaveKeyWithValue("SNAPSHOT_NAME_PGDATA", "snap-1"))
	})

	It("returns a copy that does not expose the internal map", func() {
		SetTemplateVariable("BACKUP_NAME", "backup-1")
		vars := TemplateVariables()
		vars["BACKUP_NAME"] = "tampered"
		Expect(TemplateVariables()).To(HaveKeyWithValue("BACKUP_NAME", "backup-1"))
	})

	It("supports concurrent writers and readers", func() {
		var wg sync.WaitGroup
		for range 50 {
			wg.Add(2)
			go func() {
				defer wg.Done()
				SetTemplateVariable("CONCURRENT", "value")
			}()
			go func() {
				defer wg.Done()
				_ = TemplateVariables()
			}()
		}
		wg.Wait()
	})
})
