/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package envsubst

import (
	"bytes"
	"errors"
	"fmt"
)

// ErrEnvVarNotFound is thrown when a SHELL-FORMAT var in a file does not
// have a matching env variable
var ErrEnvVarNotFound = errors.New("could not find environment variable")

// Envsubst substitutes any SHELL-FORMAT variables embedded in a file
// by the value of the corresponding environment variable, provided in `vars`
//
// SHELL-FORMAT is, `${foo}`  BUT NOT `$foo`, for simplicity of implementation
//
// NOTE: If a variable embedded in the file is not provided in the `vars`
// argument, this function will error out. This is different from the behavior
// of the `envsubst` shell command: in testing we should avoid silent failures
func Envsubst(vars map[string]string, data []byte) ([]byte, error) {
	embeddedVars := findEmbeddedVars(data)
	for _, v := range embeddedVars {
		value, found := vars[v]
		if !found || value == "" {
			return nil, fmt.Errorf("var %s: %w", v, ErrEnvVarNotFound)
		}
	}
	var replaced []byte
	replaced = data
	for key, value := range vars {
		replaced = bytes.ReplaceAll(replaced, []byte("${"+key+"}"), []byte(value))
	}
	return replaced, nil
}

// findEmbeddedVars lists any SHELL-FORMAT ${my-var} variables embedded in the
// text. It only counts variables once i.e. it de-duplicates variables
func findEmbeddedVars(text []byte) []string {
	envVars := make(map[string]bool)
	subtext := text
	fst := bytes.Index(subtext, []byte("${"))
	for fst != -1 && len(subtext) > 0 {
		lst := bytes.Index(subtext[fst:], []byte("}"))
		if lst != -1 {
			envVars[string(subtext[fst+2:(fst+lst)])] = true
		}
		subtext = subtext[(fst + lst):]
		fst = bytes.Index(subtext, []byte("${"))
	}
	out := make([]string, len(envVars))
	i := 0
	for k := range envVars {
		out[i] = k
		i++
	}
	return out
}
