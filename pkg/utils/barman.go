/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"
	"regexp"
)

var regexPolicy = regexp.MustCompile(`([1-9][0-9]*)([dwm])$`)

// ParsePolicy ensure that the policy string follows the
// rules required by Barman
func ParsePolicy(policy string) (string, error) {
	unitName := map[string]string{
		"d": "DAYS",
		"w": "WEEKS",
		"m": "MONTHS",
	}
	matches := regexPolicy.FindStringSubmatch(policy)
	if len(matches) < 3 {
		return "", fmt.Errorf("not a valid policy")
	}

	return fmt.Sprintf("RECOVERY WINDOW OF %v %v", matches[1], unitName[matches[2]]), nil
}
