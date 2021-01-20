/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package tests contains the e2e test infrastructure of the cloud native PostgreSQL
// operator
package tests

import (
	"bytes"
	"log"
	"os/exec"

	"github.com/google/shlex"
	corev1 "k8s.io/api/core/v1"
)

// Run executes a command and process the information
//nolint:unparam,gosec
func Run(command string) (stdout string, stderr string, err error) {
	tokens, err := shlex.Split(command)
	if err != nil {
		log.Printf("Error parsing command `%v`: %v\n", command, err)
		return "", "", err
	}

	var outBuffer, errBuffer bytes.Buffer
	cmd := exec.Command(tokens[0], tokens[1:]...)
	cmd.Stdout, cmd.Stderr = &outBuffer, &errBuffer
	if err = cmd.Run(); err != nil {
		return "", errBuffer.String(), err
	}

	return outBuffer.String(), errBuffer.String(), nil
}

// FirstEndpointIP returns the IP of first Address in the Endpoint
func FirstEndpointIP(endpoint *corev1.Endpoints) string {
	if endpoint == nil {
		return ""
	}
	if len(endpoint.Subsets) == 0 || len(endpoint.Subsets[0].Addresses) == 0 {
		return ""
	}
	return endpoint.Subsets[0].Addresses[0].IP
}
