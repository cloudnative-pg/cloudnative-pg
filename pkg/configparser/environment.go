/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package configparser

import (
	"os"
)

// EnvironmentSource is an interface to identify an environment values source.
type EnvironmentSource interface {
	Getenv(key string) string
}

// OsEnvironment is an EnvironmentSource that fetch data from the OS environment.
type OsEnvironment struct{}

// Getenv retrieves the value of the environment variable named by the key.
func (OsEnvironment) Getenv(key string) string {
	return os.Getenv(key)
}
