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

// Package run contains functions to execute commands locally
package run

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/google/shlex"
	"github.com/onsi/ginkgo/v2"

	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
)

// Unchecked executes a command and process the information
func Unchecked(command string) (stdout string, stderr string, err error) {
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
	if err != nil {
		err = fmt.Errorf("%w - %v", err, stderr)
	}
	return stdout, stderr, err
}

// UncheckedRetry executes a command and process the information with retry
func UncheckedRetry(command string) (stdout string, stderr string, err error) {
	var tokens []string
	tokens, err = shlex.Split(command)
	if err != nil {
		ginkgo.GinkgoWriter.Printf("Error parsing command `%v`: %v\n", command, err)
		return "", "", err
	}

	var outBuffer, errBuffer bytes.Buffer
	err = retry.New(
		retry.Delay(objects.PollingTime*time.Second),
		retry.Attempts(objects.RetryAttempts),
		retry.DelayType(retry.FixedDelay)).
		Do(
			func() error {
				cmd := exec.Command(tokens[0], tokens[1:]...) // #nosec G204
				cmd.Stdout, cmd.Stderr = &outBuffer, &errBuffer
				return cmd.Run()
			},
		)
	stdout = outBuffer.String()
	stderr = errBuffer.String()
	if err != nil {
		err = fmt.Errorf("%w - %v", err, stderr)
	}
	return stdout, stderr, err
}

// Run executes a command and prints the output when terminates with an error
func Run(command string) (stdout string, stderr string, err error) {
	stdout, stderr, err = Unchecked(command)

	var exerr *exec.ExitError
	if errors.As(err, &exerr) {
		ginkgo.GinkgoWriter.Printf("RunCheck: %v\nExitCode: %v\n Out:\n%v\nErr:\n%v\n",
			command, exerr.ExitCode(), stdout, stderr)
	}
	return stdout, stderr, err
}
