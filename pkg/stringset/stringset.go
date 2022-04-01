/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
