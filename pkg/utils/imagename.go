/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
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
//	GetImageTag("postgres") == "latest"
//	GetImageTag("ghcr.io/cloudnative-pg/postgresql:12.3") == "12.3"
func GetImageTag(imageName string) string {
	ref := NewReference(imageName)
	return ref.Tag
}
