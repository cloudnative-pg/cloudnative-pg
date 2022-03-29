/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package plugin

import (
	"encoding/json"
	"io"

	"sigs.k8s.io/yaml"
)

// Print output an object via an io.Writer in a machine-readable way
func Print(o interface{}, format OutputFormat, writer io.Writer) error {
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
