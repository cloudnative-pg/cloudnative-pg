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

package logpipe

// CSVRecordParser is implemented by structs that can be filled when parsing a CSV line.
// The FromCSV method just stores the CSV record fields inside the struct fields.
// A validation check of the CSV fields should be performed by the caller.
// Also handling recover from panic should be provided by the caller, in order to take care of runtime error,
// e.g. index out of range because of CSV malformation
type CSVRecordParser interface {
	FromCSV(content []string) NamedRecord
	NamedRecord
}

// NamedRecord is the interface for structs that have a name
type NamedRecord interface {
	GetName() string
}
