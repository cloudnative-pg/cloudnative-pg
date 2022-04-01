/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
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
