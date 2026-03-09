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

package hba

import (
	"net"
	"regexp"
	"strings"

	"github.com/cloudnative-pg/machinery/pkg/stringset"
)

// PodSelectorReference is the type of references to a Pod selector.
const PodSelectorReference = "podselector"

// ErrUnknownDescriptorType is returned when an HBA line contains
// a descriptor reference with an unknown type.
type ErrUnknownDescriptorType struct {
	DescriptorType string
}

func (e *ErrUnknownDescriptorType) Error() string {
	return "unknown descriptor type: " + e.DescriptorType
}

// ErrMultiplePodSelectorReferences is returned when an HBA line contains
// more than one podselector reference.
type ErrMultiplePodSelectorReferences struct {
	Count int
}

func (e *ErrMultiplePodSelectorReferences) Error() string {
	return "at most one podselector reference is allowed per line"
}

// ErrPodSelectorNotFound is returned when an HBA line references a
// pod selector that is not present in the provided map.
type ErrPodSelectorNotFound struct {
	SelectorName string
}

func (e *ErrPodSelectorNotFound) Error() string {
	return "pod selector not found: " + e.SelectorName
}

// descriptorReference is the syntax of descriptor references
// in PostgreSQL host-based-access lines.
var descriptorReference = regexp.MustCompile(`\${([^:]+):([^}]+)}`)

// ExpandLine expands descriptor references in an HBA line.
// Only the first descriptor reference is expanded; lines with multiple
// references should be rejected by ValidateLine before reaching this point.
// Returns one line per expanded value.
// If the line has no descriptor reference, returns a slice
// containing just the original line.
// If an error occurs, the line is returned as a comment with
// the error message appended.
// This function is also registered as a Go template FuncMap entry
// ("expandRule") for inline expansion in the pg_hba.conf template.
func ExpandLine(line string, selectorIPs map[string][]string) []string {
	match := descriptorReference.FindStringSubmatchIndex(line)
	if match == nil {
		return []string{line}
	}

	descriptorType := line[match[2]:match[3]]
	if descriptorType != PodSelectorReference {
		err := &ErrUnknownDescriptorType{DescriptorType: descriptorType}
		return []string{commentWithError(line, err)}
	}

	selectorName := line[match[4]:match[5]]
	ips, ok := selectorIPs[selectorName]
	if !ok {
		err := &ErrPodSelectorNotFound{SelectorName: selectorName}
		return []string{commentWithError(line, err)}
	}

	result := make([]string, len(ips))
	for i, ip := range ips {
		result[i] = line[:match[0]] + ensureCIDR(ip) + line[match[1]:]
	}

	return result
}

// ensureCIDR ensures the IP has a CIDR suffix.
// Returns the IP with /32 for IPv4 or /128 for IPv6 if no mask is present.
func ensureCIDR(ip string) string {
	if strings.Contains(ip, "/") {
		return ip
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ip
	}

	if parsed.To4() != nil {
		return ip + "/32"
	}

	return ip + "/128"
}

func commentWithError(line string, err error) string {
	return "# " + line + " # " + err.Error()
}

// ValidateLine validates a PostgreSQL HBA line, checking that all
// descriptor references are of known types, that at most one
// podselector reference is present, and that referenced selectors exist.
func ValidateLine(line string, knownSelectors *stringset.Data) error {
	matches := descriptorReference.FindAllStringSubmatch(line, -1)
	podSelectorCount := 0

	for _, match := range matches {
		descriptorType := match[1]
		selectorName := match[2]

		switch descriptorType {
		case PodSelectorReference:
			podSelectorCount++
			if !knownSelectors.Has(selectorName) {
				return &ErrPodSelectorNotFound{SelectorName: selectorName}
			}
		default:
			return &ErrUnknownDescriptorType{DescriptorType: descriptorType}
		}
	}

	if podSelectorCount > 1 {
		return &ErrMultiplePodSelectorReferences{Count: podSelectorCount}
	}

	return nil
}
