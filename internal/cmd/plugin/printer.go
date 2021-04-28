/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package plugin

import (
	"encoding/json"
	"os"

	"sigs.k8s.io/yaml"
)

// Print output an object to stdout in a machine-readable way
func Print(o interface{}, format OutputFormat) error {
	switch format {
	case OutputFormatJSON:
		data, err := json.MarshalIndent(o, "", "  ")
		if err != nil {
			return err
		}

		_, err = os.Stdout.Write(data)
		if err != nil {
			return err
		}

	case OutputFormatYAML:
		data, err := yaml.Marshal(o)
		if err != nil {
			return err
		}

		_, err = os.Stdout.Write(data)
		if err != nil {
			return err
		}
	}

	return nil
}
