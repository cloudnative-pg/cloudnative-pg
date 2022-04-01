/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package catalog

// Len implements sort.Interface
func (catalog *Catalog) Len() int {
	return len(catalog.List)
}

// Less implements sort.Interface
func (catalog *Catalog) Less(i, j int) bool {
	if catalog.List[i].BeginTime.IsZero() {
		// backups which are not completed go to the bottom
		return false
	}

	if catalog.List[i].EndTime.IsZero() {
		// backups which are not completed go to the bottom
		return true
	}

	return catalog.List[i].BeginTime.Before(catalog.List[j].EndTime)
}

// Swap implements sort.Interface
func (catalog *Catalog) Swap(i, j int) {
	catalog.List[j], catalog.List[i] = catalog.List[i], catalog.List[j]
}
