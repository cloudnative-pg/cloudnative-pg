/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

package utils

import (
	"bytes"
	"errors"
	"os/exec"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/google/shlex"
	"github.com/onsi/ginkgo/v2"
)

// RunUnchecked executes a command and process the information
func RunUnchecked(command string) (stdout string, stderr string, err error) {
	tokens, err := shlex.Split(command)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("Error parsing command `%v`: %v\n", command, err)
		return "", "", err
	}

	var outBuffer, errBuffer bytes.Buffer
	cmd := exec.Command(tokens[0], tokens[1:]...) // #nosec G204
	cmd.Stdout, cmd.Stderr = &outBuffer, &errBuffer
	err = cmd.Run()
	stdout = outBuffer.String()
	stderr = errBuffer.String()
	return
}

// RunUncheckedRetry executes a command and process the information with retry
func RunUncheckedRetry(command string) (stdout string, stderr string, err error) {
	var tokens []string
	tokens, err = shlex.Split(command)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("Error parsing command `%v`: %v\n", command, err)
		return "", "", err
	}

	var outBuffer, errBuffer bytes.Buffer
	err = retry.Do(
		func() error {
			cmd := exec.Command(tokens[0], tokens[1:]...) // #nosec G204
			cmd.Stdout, cmd.Stderr = &outBuffer, &errBuffer
			err = cmd.Run()
			if err != nil {
				return err
			}
			return nil
		},
		retry.Delay(PollingTime*time.Second),
		retry.Attempts(RetryTimeout),
		retry.DelayType(retry.FixedDelay),
	)
	stdout = outBuffer.String()
	stderr = errBuffer.String()
	return
}

// Run executes a command and prints the output when terminates with an error
func Run(command string) (stdout string, stderr string, err error) {
	stdout, stderr, err = RunUnchecked(command)

	var exerr *exec.ExitError
	if errors.As(err, &exerr) {
		ginkgo.GinkgoWriter.Printf("RunCheck: %v\nExitCode: %v\n Out:\n%v\nErr:\n%v\n",
			command, exerr.ExitCode(), stdout, stderr)
	}
	return
}

// RunRetry executes a command with retry and prints the output when terminates with an error
func RunRetry(command string) (stdout string, stderr string, err error) {
	stdout, stderr, err = RunUncheckedRetry(command)

	var exerr *exec.ExitError
	if errors.As(err, &exerr) {
		ginkgo.GinkgoWriter.Printf("RunCheck: %v\nExitCode: %v\n Out:\n%v\nErr:\n%v\n",
			command, exerr.ExitCode(), stdout, stderr)
	}
	return
}
