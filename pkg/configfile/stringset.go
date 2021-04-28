/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configfile

// StringSet represent a set of strings
type StringSet struct {
	innerMap map[string]struct{}
}

// NewStringSet create a new empty set of strings
func NewStringSet() *StringSet {
	return &StringSet{
		innerMap: make(map[string]struct{}),
	}
}

// Put a string in the set
func (set *StringSet) Put(key string) {
	set.innerMap[key] = struct{}{}
}

// Has check if a string is in the set or not
func (set *StringSet) Has(key string) bool {
	_, ok := set.innerMap[key]
	return ok
}

// Len returns the map of the set
func (set *StringSet) Len() int {
	return len(set.innerMap)
}
