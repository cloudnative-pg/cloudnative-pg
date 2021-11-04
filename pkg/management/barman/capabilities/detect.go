/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package capabilities

import (
	"fmt"
	"os/exec"
	"regexp"

	"github.com/blang/semver"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

// capabilities stores the current Barman capabilities
var capabilities *Capabilities

// Detect barman-cloud executables presence and store the Capabilities
// of the barman-cloud version it finds
func Detect() (*Capabilities, error) {
	version, err := getBarmanCloudVersion(BarmanCloudWalArchive)
	if err != nil {
		return nil, err
	}

	capabilities := new(Capabilities)

	if version == nil {
		log.Info("Missing Barman Cloud installation in the operand image")
		return capabilities, nil
	}

	capabilities.Version = version

	switch {
	case version.GE(semver.Version{Major: 2, Minor: 14}):
		// Retention policy support, added in Barman >= 2.14
		capabilities.HasRetentionPolicy = true
		fallthrough
	case version.GE(semver.Version{Major: 2, Minor: 13}):
		// Cloud providers support, added in Barman >= 2.13
		capabilities.HasAzure = true
		capabilities.HasS3 = true
	}

	log.Info("Detected Barman installation", "capabilities", capabilities)

	return capabilities, nil
}

// barmanCloudVersionRegex is a regular expression to parse the output of
// any barman cloud command when invoked with `--version` option
var barmanCloudVersionRegex = regexp.MustCompile("barman-cloud.* (?P<Version>.*)")

// getBarmanCloudVersion retrieves a barman-cloud subcommand version.
// e.g. barman-cloud-backup, barman-cloud-wal-archive
// It returns nil if the command is not found
func getBarmanCloudVersion(command string) (*semver.Version, error) {
	_, err := exec.LookPath(command)
	if err != nil {
		return nil, nil
	}

	cmd := exec.Command(command, "--version") // #nosec G204
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("while checking %s version: %w", command, err)
	}
	if cmd.ProcessState.ExitCode() != 0 {
		return nil, fmt.Errorf("exit code different from zero checking %s version", command)
	}

	matches := barmanCloudVersionRegex.FindStringSubmatch(string(out))
	version, err := semver.ParseTolerant(matches[1])
	if err != nil {
		return nil, fmt.Errorf("while parsing %s version: %w", command, err)
	}

	return &version, err
}

// CurrentCapabilities retrieves the capabilities of local barman installation,
// retrieving it from the cache if available.
func CurrentCapabilities() (*Capabilities, error) {
	if capabilities == nil {
		var err error
		capabilities, err = Detect()
		if err != nil {
			log.Error(err, "Failed to detect Barman capabilities")
			return nil, err
		}
	}

	return capabilities, nil
}
