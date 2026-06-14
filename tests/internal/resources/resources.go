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

// Package resources provides helpers that materialise Kubernetes objects
// described by sample/template YAML files into a running cluster.
package resources

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/envsubst"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/yaml"

	. "github.com/onsi/ginkgo/v2" //nolint
	. "github.com/onsi/gomega"    //nolint
)

// CreateResourceFromFile creates the Kubernetes objects defined in a YAML
// sample file inside the namespace, retrying transient failures.
func CreateResourceFromFile(env *environment.TestingEnvironment, namespace, sampleFilePath string) {
	GinkgoHelper()
	Eventually(func() error {
		return CreateResourcesFromFileWithError(env, namespace, sampleFilePath)
	}, environment.RetryTimeout, objects.PollingTime).Should(Succeed())
}

// CreateResourcesFromFileWithError parses the YAML at sampleFilePath and
// creates each contained object, returning the first error encountered.
func CreateResourcesFromFileWithError(env *environment.TestingEnvironment, namespace, sampleFilePath string) error {
	wrapErr := func(err error) error { return fmt.Errorf("on CreateResourcesFromFileWithError: %w", err) }
	yamlContent, err := GetYAMLContent(env, sampleFilePath)
	if err != nil {
		return wrapErr(err)
	}

	parsedObjects, err := yaml.ParseObjectsFromYAML(yamlContent, namespace)
	if err != nil {
		return wrapErr(err)
	}
	for _, obj := range parsedObjects {
		if cluster, ok := obj.(*apiv1.Cluster); ok {
			clusterutils.AddTopologySpreadConstraint(cluster)
		}
		_, err := objects.Create(env.Ctx, env.Client, obj)
		if err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

// DeleteResourcesFromFile deletes every Kubernetes object described in the
// YAML at sampleFilePath, returning the first error encountered.
func DeleteResourcesFromFile(env *environment.TestingEnvironment, namespace, sampleFilePath string) error {
	wrapErr := func(err error) error { return fmt.Errorf("in DeleteResourcesFromFile: %w", err) }
	yamlContent, err := GetYAMLContent(env, sampleFilePath)
	if err != nil {
		return wrapErr(err)
	}

	parsedObjects, err := yaml.ParseObjectsFromYAML(yamlContent, namespace)
	if err != nil {
		return wrapErr(err)
	}
	for _, obj := range parsedObjects {
		if err := objects.Delete(env.Ctx, env.Client, obj); err != nil {
			return wrapErr(err)
		}
	}
	return nil
}

// GetYAMLContent reads a .yaml or .template file and returns its content.
//
// .template files are run through envsubst so SHELL-FORMAT variables are
// substituted using both the process environment and a small set of e2e
// defaults derived from env.
func GetYAMLContent(env *environment.TestingEnvironment, sampleFilePath string) ([]byte, error) {
	wrapErr := func(err error) error { return fmt.Errorf("in GetYAMLContent: %w", err) }
	cleanPath := filepath.Clean(sampleFilePath)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return nil, wrapErr(err)
	}
	yamlContent := data

	if filepath.Ext(cleanPath) == ".template" {
		preRollingUpdateImg := os.Getenv("E2E_PRE_ROLLING_UPDATE_IMG")
		if preRollingUpdateImg == "" {
			preRollingUpdateImg = os.Getenv("POSTGRES_IMG")
		}
		csiStorageClass := os.Getenv("E2E_CSI_STORAGE_CLASS")
		if csiStorageClass == "" {
			csiStorageClass = os.Getenv("E2E_DEFAULT_STORAGE_CLASS")
		}
		envVars := buildTemplateEnvs(map[string]string{
			"E2E_PRE_ROLLING_UPDATE_IMG": preRollingUpdateImg,
			"E2E_CSI_STORAGE_CLASS":      csiStorageClass,
			"PG_MAJOR":                   strconv.FormatUint(env.PostgresVersion, 10),
		})

		if serverName := os.Getenv("SERVER_NAME"); serverName != "" {
			envVars["SERVER_NAME"] = serverName
		}

		yamlContent, err = envsubst.Envsubst(envVars, data)
		if err != nil {
			return nil, wrapErr(err)
		}
	}
	return yamlContent, nil
}

func buildTemplateEnvs(additionalEnvs map[string]string) map[string]string {
	envs := make(map[string]string)
	for _, s := range os.Environ() {
		keyValue := strings.SplitN(s, "=", 2)
		if len(keyValue) < 2 {
			continue
		}
		envs[keyValue[0]] = keyValue[1]
	}

	for key, value := range additionalEnvs {
		envs[key] = value
	}

	return envs
}
