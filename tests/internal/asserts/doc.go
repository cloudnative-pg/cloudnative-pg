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

// Package asserts is the root for composed end-to-end assertions used by the
// Ginkgo specs in tests/e2e. Each topic lives in its own subpackage
// (cluster, backup, replication, ...) so it can be imported from per-topic
// e2e test directories. Asserts packages may dot-import ginkgo/gomega; the
// caller is expected to pass the *environment.TestingEnvironment and any
// required timeouts map explicitly rather than reach for package-level state.
//
// Layering rules:
//   - tests/utils/<x>/   primitives over k8s / postgres state
//   - tests/internal/asserts/<x>/ composed assertions, may depend on tests/utils
//   - tests/e2e/         specs only, depend on both
package asserts
