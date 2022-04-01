//go:build tools
// +build tools

/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package tools is used to track dependencies of tools
package tools

import (
	_ "github.com/onsi/ginkgo/v2"
	_ "github.com/onsi/ginkgo/v2/ginkgo/generators"
	_ "github.com/onsi/ginkgo/v2/ginkgo/internal"
	_ "github.com/onsi/ginkgo/v2/ginkgo/labels"
)
