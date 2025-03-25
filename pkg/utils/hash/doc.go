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

// Package hash allows the user to get a hash number for a given Kubernetes
// object. This is useful to detect when a derived resource need to be
// changed too.
//
// The code in this package is adapted from:
//
// https://github.com/kubernetes/kubernetes/blob/master/pkg/util/hash/hash.go   // wokeignore:rule=master
// https://github.com/kubernetes/kubernetes/blob/ea07644/pkg/controller/controller_utils.go#L1189
package hash
