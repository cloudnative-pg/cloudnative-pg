/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configfile

import "strings"

// SplitLines split the passed content into lines, returning an empty slice
// when the content is empty
func SplitLines(content string) []string {
	content = strings.TrimSuffix(content, "\n")
	if content != "" {
		return strings.Split(content, "\n")
	}
	return []string{}
}
