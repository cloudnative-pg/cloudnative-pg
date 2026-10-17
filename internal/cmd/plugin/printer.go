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

package plugin

import (
	"encoding/json"
	"io"

	"sigs.k8s.io/yaml"
)

// Print output an object via an io.Writer in a machine-readable way
func Print(o any, format OutputFormat, writer io.Writer) error {
	switch format {
	case OutputFormatJSON:
		data, err := json.MarshalIndent(o, "", "  ")
		if err != nil {
			return err
		}

		_, err = writer.Write(data)
		if err != nil {
			return err
		}

		// json.MarshalIndent doesn't add the final newline
		_, err = io.WriteString(writer, "\n")
		if err != nil {
			return err
		}

	case OutputFormatYAML:
		data, err := yaml.Marshal(o)
		if err != nil {
			return err
		}

		_, err = writer.Write(data)
		if err != nil {
			return err
		}
	}

	return nil
}
