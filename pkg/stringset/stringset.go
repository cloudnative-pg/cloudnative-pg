/*
Copyright 2019-2022 The CloudNativePG Contributors

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

// Package stringset implements a basic set of strings
package stringset

// Data represent a set of strings
type Data struct {
	innerMap map[string]struct{}
}

// New create a new empty set of strings
func New() *Data {
	return &Data{
		innerMap: make(map[string]struct{}),
	}
}

// From create a empty set of strings given
// a slice of strings
func From(strings []string) *Data {
	result := New()
	for _, value := range strings {
		result.Put(value)
	}
	return result
}

// Put a string in the set
func (set *Data) Put(key string) {
	set.innerMap[key] = struct{}{}
}

// Delete deletes a string from the set. If the string doesn't exist
// this is a no-op
func (set *Data) Delete(key string) {
	delete(set.innerMap, key)
}

// Has check if a string is in the set or not
func (set *Data) Has(key string) bool {
	_, ok := set.innerMap[key]
	return ok
}

// Len returns the map of the set
func (set *Data) Len() int {
	return len(set.innerMap)
}

// ToList returns the strings contained in this set as
// a string slice
func (set *Data) ToList() (result []string) {
	result = make([]string, 0, len(set.innerMap))
	for key := range set.innerMap {
		result = append(result, key)
	}
	return
}
