/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

// CollectDifferencesFromMaps returns a map of the differences (as slice of strings) of the values of two given maps.
// Map result values are added when a key is present just in one of the input maps, or if the values are different
// given the same key
func CollectDifferencesFromMaps(p1 map[string]string, p2 map[string]string) map[string][]string {
	diff := make(map[string][]string)
	totalKeys := make(map[string]bool)
	for k := range p1 {
		totalKeys[k] = true
	}
	for k := range p2 {
		totalKeys[k] = true
	}
	for k := range totalKeys {
		v1, ok1 := p1[k]
		v2, ok2 := p2[k]
		if ok1 && ok2 && v1 == v2 {
			continue
		}
		diff[k] = []string{v1, v2}
	}
	if len(diff) > 0 {
		return diff
	}
	return nil
}
