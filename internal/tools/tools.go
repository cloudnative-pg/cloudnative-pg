//go:build tools
// +build tools

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

// Package tools is used to track dependencies of tools we use in our
// developement process. DO NOT REMOVE
package tools

import (
	_ "github.com/onsi/ginkgo/v2"
	_ "github.com/onsi/ginkgo/v2/ginkgo/generators"
	_ "github.com/onsi/ginkgo/v2/ginkgo/internal"
	_ "github.com/onsi/ginkgo/v2/ginkgo/labels"
)
