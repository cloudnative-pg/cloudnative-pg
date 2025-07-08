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
	"fmt"
	"os"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("buildPostmasterEnv", func() {
	var cluster apiv1.Cluster

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
	})

	Context("Extensions enabled, LD_LIBRARY_PATH undefined", func() {
		It("should be empty by default", func() {
			ldLibraryPath := getLibraryPathFromEnv(buildPostmasterEnv(&cluster))
			Expect(ldLibraryPath).To(BeEquivalentTo("LD_LIBRARY_PATH="))
		})
	})

	Context("Extensions enabled, LD_LIBRARY_PATH defined", func() {
		const finalPaths = ":/extensions/foo/system:/extensions/bar/system:/extensions/bar/sample"
		BeforeEach(func() {
			cluster.Spec.PostgresConfiguration.Extensions[0].LdLibraryPath = []string{"system"}
			cluster.Spec.PostgresConfiguration.Extensions[1].LdLibraryPath = []string{"system", "sample"}
		})

		It("should be defined", func() {
			ldLibraryPath := getLibraryPathFromEnv(buildPostmasterEnv(&cluster))
			Expect(ldLibraryPath).To(BeEquivalentTo(fmt.Sprintf("LD_LIBRARY_PATH=%s", finalPaths)))
		})
		It("should retain existing values", func() {
			err := os.Setenv("LD_LIBRARY_PATH", ":/my/library/path")
			Expect(err).ToNot(HaveOccurred())

			ldLibraryPath := getLibraryPathFromEnv(buildPostmasterEnv(&cluster))
			Expect(ldLibraryPath).To(BeEquivalentTo(fmt.Sprintf("LD_LIBRARY_PATH=:/my/library/path%s", finalPaths)))
		})
	})

	Context("Extensions disabled", func() {
		BeforeEach(func() {
			cluster.Spec.PostgresConfiguration.Extensions = []apiv1.ExtensionConfiguration{}
		})
		It("LD_LIBRARY_PATH should be empty", func() {
			ldLibraryPath := getLibraryPathFromEnv(buildPostmasterEnv(&cluster))
			Expect(ldLibraryPath).To(BeEquivalentTo("LD_LIBRARY_PATH="))
		})
	})
})

func getLibraryPathFromEnv(envs []string) string {
	var ldLibraryPath string
	for _, e := range envs {
		if strings.HasPrefix(e, "LD_LIBRARY_PATH=") {
			ldLibraryPath = e
			break
		}
	}

	return ldLibraryPath
}
