/*
Copyright The CloudNativePG Contributors

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
