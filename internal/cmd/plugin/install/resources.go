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

package install

import (
	"context"
	"github.com/google/go-github/v48/github"
	"sort"
)

func getLatestOperatorVersion(ctx context.Context) (string, error) {
	client := github.NewClient(nil)
	branches, _, err := client.Repositories.ListBranches(ctx, "cloudnative-pg", "artifacts", nil)
	if err != nil {
		return "", err
	}

	// Sort the branches by name
	sort.Slice(branches, func(i, j int) bool {
		return branches[i].GetName() > branches[j].GetName()
	})
	return branches[0].GetName(), nil

}
