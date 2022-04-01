/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// stringifyPgBouncerParameters will take map of PgBouncer parameters and emit
// the relative configuration. We are using a function instead of using the template
// because we want the order of the parameters to be stable to avoid doing rolling
// out new PgBouncer Pods when it's not really needed
func stringifyPgBouncerParameters(parameters map[string]string) (paramsString string) {
	keys := make([]string, 0, len(parameters))
	for k := range parameters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		paramsString += fmt.Sprintf("%s = %s\n", k, parameters[k])
	}
	return paramsString
}

// buildPgBouncerParameters will build a PgBouncer configuration applying any
// default parameters and forcing any required parameter needed for the
// controller to work correctly
func buildPgBouncerParameters(userParameters map[string]string) map[string]string {
	params := make(map[string]string, len(userParameters))

	for k, v := range userParameters {
		params[k] = cleanupPgBouncerValue(v)
	}

	for k, defaultValue := range defaultPgBouncerParameters {
		if userValue, ok := params[k]; ok {
			if k == ignoreStartupParametersKey {
				params[k] = strings.Join([]string{defaultValue, userValue}, ",")
			}
			continue
		}
		params[k] = defaultValue
	}

	for k, v := range forcedPgBouncerParameters {
		params[k] = v
	}

	return params
}

// The following regexp will match any newline character. PgBouncer
// doesn't admit newlines inside the configuration at all
var newlineRegexp = regexp.MustCompile(`\r\n|[\r\n\v\f\x{0085}\x{2028}\x{2029}]`)

// cleanupPgBouncerValue removes any newline character from a configuration value.
// The parser used by libusual doesn't support that.
func cleanupPgBouncerValue(parameter string) (escaped string) {
	// See:
	// https://github.com/libusual/libusual/blob/master/usual/cfparser.c  //wokeignore:rule=master
	//
	// The PgBouncer ini file parser doesn't admit any newline character
	// so we are just removing from the value
	return newlineRegexp.ReplaceAllString(parameter, "")
}
