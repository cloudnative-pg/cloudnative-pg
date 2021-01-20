/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

package utils

import (
	"fmt"
	"strings"
)

// IsImageNameEqual compares two image names for equality. Two image names
// could be equal even if their string name is not the same. I.e.
// "myimage:2.3" is equal to "docker.io/library/myimage:2.3"
func IsImageNameEqual(image1, image2 string) bool {
	return NormaliseImageName(image1) == NormaliseImageName(image2)
}

// NormaliseImageName normalise an image name considering his docker.io prefix
func NormaliseImageName(imageName string) string {
	result := imageName

	switch strings.Count(imageName, "/") {
	case 0:
		result = fmt.Sprintf("docker.io/library/%v", imageName)
	case 1:
		result = fmt.Sprintf("docker.io/%v", imageName)
	}

	if !strings.Contains(imageName, ":") {
		result = fmt.Sprintf("%v:latest", result)
	}

	return result
}

// GetImageTag gets the image tag from a full image string.
// Example:
//
//     GetImageTag("postgres") == "latest"
//     GetImageTag("quay.io/test/postgres:12.3") == "12.3"
//
func GetImageTag(imageName string) string {
	splittedTag := strings.Split(
		NormaliseImageName(imageName), ":")
	return splittedTag[1]
}
