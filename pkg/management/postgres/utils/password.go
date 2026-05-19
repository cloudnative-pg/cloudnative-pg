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

package utils

import (
	"fmt"

	"github.com/cloudnative-pg/machinery/pkg/postgres/password"
	"github.com/cloudnative-pg/machinery/pkg/postgres/scram"
)

// EnsureEncryptedPassword returns p unchanged if PostgreSQL would
// recognize it as already encrypted; otherwise it SCRAM-SHA-256 encodes
// it using PostgreSQL's default parameters.
//
// It is used before emitting "ALTER ROLE ... PASSWORD '...'" so the
// literal in the statement is never cleartext.
func EnsureEncryptedPassword(p string) (string, error) {
	if password.GetType(p) != password.Plaintext {
		return p, nil
	}

	hashed, err := (&scram.GenerateOptions{PlainText: p}).Generate()
	if err != nil {
		return "", fmt.Errorf("encrypting password as SCRAM-SHA-256: %w", err)
	}
	return hashed, nil
}
