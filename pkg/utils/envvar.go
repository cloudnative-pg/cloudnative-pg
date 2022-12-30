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

import v1 "k8s.io/api/core/v1"

func envVarToMap(envs []v1.EnvVar) map[string]string {
	envsMapoped := map[string]string{}
	for _, env := range envs {
		envsMapoped[env.Name] = env.Value
	}
	return envsMapoped
}

// envExists checks if `env` exists in the slices of `envs`
func envExists(envs []v1.EnvVar, env v1.EnvVar) bool {
	mappedEnvs := envVarToMap(envs)
	_, ok := mappedEnvs[env.Name]
	return ok
}

// MergeEnvVarSlices merge two EnvVar slices without overwriting the ones on `s1`
func MergeEnvVarSlices(s1 []v1.EnvVar, s2 []v1.EnvVar) []v1.EnvVar {
	for _, env := range s2 {
		if !envExists(s1, env) {
			s1 = append(s1, env)
		}
	}
	return s1
}
