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

// Package majorupgrade provides the logic for upgrading a PostgreSQL cluster
// to a new major version.
//
// The upgrade process consists of the following steps:
//
//  1. Delete all Pods in the cluster.
//  2. Create and initiate the major upgrade job.
//  3. Wait for the job to complete.
//  4. If the upgrade job completes successfully, start new Pods for the upgraded version.
//     Otherwise, stop and wait for input by the user.
package majorupgrade
