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

package logicalimport

import (
	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("pg_restore options management", func() {
	var importBootstrap *apiv1.Import

	BeforeEach(func() {
		importBootstrap = &apiv1.Import{
			PgRestoreExtraOptions:    []string{"--extra-options"},
			PgRestorePredataOptions:  []string{"--predata-options"},
			PgRestoreDataOptions:     []string{"--data-options"},
			PgRestorePostdataOptions: []string{"--postdata-options"},
		}
	})

	It("creates the options from the import bootstrap method", func() {
		options := buildPgRestoreSectionOptions(importBootstrap)
		Expect(options).NotTo(BeNil())
		Expect(options.forSection(sectionPreData)).To(ConsistOf("--predata-options"))
		Expect(options.forSection(sectionData)).To(ConsistOf("--data-options"))
		Expect(options.forSection(sectionPostData)).To(ConsistOf("--postdata-options"))
	})

	It("defaults a section to the common options, when the specific options are empty", func() {
		importBootstrap.PgRestoreDataOptions = nil
		options := buildPgRestoreSectionOptions(importBootstrap)
		Expect(options).NotTo(BeNil())
		Expect(options.forSection(sectionPreData)).To(ConsistOf("--predata-options"))
		Expect(options.forSection(sectionData)).To(ConsistOf("--extra-options"))
		Expect(options.forSection(sectionPostData)).To(ConsistOf("--postdata-options"))
	})

	It("defaults every section to the common options when needed", func() {
		importBootstrap = &apiv1.Import{
			PgRestoreExtraOptions: []string{"--extra-options"},
		}
		options := buildPgRestoreSectionOptions(importBootstrap)
		Expect(options).NotTo(BeNil())
		Expect(options.forSection(sectionPreData)).To(ConsistOf("--extra-options"))
		Expect(options.forSection(sectionData)).To(ConsistOf("--extra-options"))
		Expect(options.forSection(sectionPostData)).To(ConsistOf("--extra-options"))
	})
})
