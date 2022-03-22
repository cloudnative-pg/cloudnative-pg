/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package capabilities stores the definition of the type for Barman capabilities
package capabilities

import (
	"github.com/blang/semver"
)

// Capabilities collects a set of boolean values that shows the possible capabilities of Barman and the version
type Capabilities struct {
	HasAzure                   bool
	HasS3                      bool
	HasGoogle                  bool
	HasRetentionPolicy         bool
	HasTags                    bool
	HasCheckWalArchive         bool
	HasSnappy                  bool
	HasErrorCodesForWALRestore bool
	Version                    *semver.Version
}
