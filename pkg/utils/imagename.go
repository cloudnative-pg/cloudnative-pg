/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	digestRegex = regexp.MustCompile(`@sha256:(?P<sha256>[a-fA-F0-9]+)$`)
	tagRegex    = regexp.MustCompile(`:(?P<tag>[^/]+)$`)
	hostRegex   = regexp.MustCompile(`^[^./:]+((\.[^./:]+)+(:[0-9]+)?|:[0-9]+)/`)
)

// Reference .
type Reference struct {
	Name   string
	Tag    string
	Digest string
}

// GetNormalizedName returns the normalized name of a reference
func (r *Reference) GetNormalizedName() (name string) {
	name = r.Name
	if r.Tag != "" {
		name = fmt.Sprintf("%s:%s", name, r.Tag)
	}
	if r.Digest != "" {
		name = fmt.Sprintf("%s@sha256:%s", name, r.Digest)
	}
	return name
}

// NewReference parses the image name and returns an error if the name is invalid.
func NewReference(name string) *Reference {
	reference := &Reference{}

	if !strings.Contains(name, "/") {
		name = "docker.io/library/" + name
	} else if !hostRegex.MatchString(name) {
		name = "docker.io/" + name
	}

	if digestRegex.MatchString(name) {
		res := digestRegex.FindStringSubmatch(name)
		reference.Digest = res[1] // digest capture group index
		name = strings.TrimSuffix(name, res[0])
	}

	if tagRegex.MatchString(name) {
		res := tagRegex.FindStringSubmatch(name)
		reference.Tag = res[1] // tag capture group index
		name = strings.TrimSuffix(name, res[0])
	} else if reference.Digest == "" {
		reference.Tag = "latest"
	}

	// everything else is the name
	reference.Name = name

	return reference
}

// GetImageTag gets the image tag from a full image string.
// Example:
//
//     GetImageTag("postgres") == "latest"
//     GetImageTag("quay.io/test/postgres:12.3") == "12.3"
//
func GetImageTag(imageName string) string {
	ref := NewReference(imageName)
	return ref.Tag
}
