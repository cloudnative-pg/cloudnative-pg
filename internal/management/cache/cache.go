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

package cache

import (
	"slices"
	"sync"
)

var cache sync.Map

// Store write an object into the local cache
func Store(c string, v any) {
	cache.Store(c, v)
}

// Delete an object from the local cache
func Delete(c string) {
	cache.Delete(c)
}

// LoadEnv loads a key from the local cache
func LoadEnv(c string) ([]string, error) {
	value, ok := cache.Load(c)
	if !ok {
		return nil, ErrCacheMiss
	}

	if v, ok := value.([]string); ok {
		return slices.Clone(v), nil
	}

	return nil, ErrUnsupportedObject
}
