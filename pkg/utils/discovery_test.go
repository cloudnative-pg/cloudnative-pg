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

package utils

import (
	"k8s.io/apimachinery/pkg/version"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = DescribeTable("Kubernetes minor version detection",
	func(info *version.Info, detectedMinorVersion int, shouldSucceed bool) {
		result, err := extractK8sMinorVersion(info)
		Expect(result).To(Equal(detectedMinorVersion))
		Expect(err == nil).To(Equal(shouldSucceed))
	},
	Entry("When minor version is an integer", &version.Info{Minor: "25"}, 25, true),
	Entry("When minor version indicate backported patches", &version.Info{Minor: "21+"}, 21, true),
	Entry("When minor version is wrong", &version.Info{Minor: "c3p0"}, 0, false),
)
