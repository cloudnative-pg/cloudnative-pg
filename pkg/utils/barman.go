/*
Copyright 2019-2022 The CloudNativePG Contributors

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
)

var regexPolicy = regexp.MustCompile(`([1-9][0-9]*)([dwm])$`)

// ParsePolicy ensure that the policy string follows the
// rules required by Barman
func ParsePolicy(policy string) (string, error) {
	unitName := map[string]string{
		"d": "DAYS",
		"w": "WEEKS",
		"m": "MONTHS",
	}
	matches := regexPolicy.FindStringSubmatch(policy)
	if len(matches) < 3 {
		return "", fmt.Errorf("not a valid policy")
	}

	return fmt.Sprintf("RECOVERY WINDOW OF %v %v", matches[1], unitName[matches[2]]), nil
}

// MapToBarmanTagsFormat will transform a map[string]string into the
// Barman tags format needed
func MapToBarmanTagsFormat(option string, mapTags map[string]string) []string {
	if len(mapTags) == 0 {
		return []string{}
	}

	tags := make([]string, 0, len(mapTags)+1)
	tags = append(tags, option)
	for k, v := range mapTags {
		tags = append(tags, fmt.Sprintf("%v,%v", k, v))
	}

	return tags
}
