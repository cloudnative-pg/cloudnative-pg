/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package tests

import (
	"os"
	"strconv"

	"github.com/EnterpriseDB/cloud-native-postgresql/tests/utils"
)

// Level - Define test importance. Each test should define its own importance
// level, and compare it with the test depth used to run the suite to choose
// if the test can be skipped.
type Level int

// Declare constants for each level
const (
	Highest Level = iota
	High
	Medium
	Low
	Lowest
)

// testDepthEnvVarName is the environment variable we expect the user to set
// to change the default test depth level
const testDepthEnvVarName = "TEST_DEPTH"

// By default we run tests with at least a medium level of importance
const defaultTestDepth = int(Medium)

// TestEnvLevel struct for operator testing
type TestEnvLevel struct {
	*utils.TestingEnvironment
	Depth int
}

// TestLevel creates the environment for testing
func TestLevel() (*TestEnvLevel, error) {
	env, err := utils.NewTestingEnvironment()
	if err != nil {
		return nil, err
	}
	if depthEnv, exists := os.LookupEnv(testDepthEnvVarName); exists {
		depth, err := strconv.Atoi(depthEnv)
		return &TestEnvLevel{env, depth}, err
	}

	return &TestEnvLevel{env, defaultTestDepth}, err
}
