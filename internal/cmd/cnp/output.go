/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package cnp

// OutputFormat represent the output format supported by this command
type OutputFormat string

const (
	// OutputFormatText means just use a human-readable output
	OutputFormatText = "text"

	// OutputFormatJSON means use machine-readable JSON output
	OutputFormatJSON = "json"

	// OutputFormatYAML means use machine-readable JSON output
	OutputFormatYAML = "yaml"
)
