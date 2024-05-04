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

// contextKey a type used to assign values inside the context
type contextKey string

// ContextKeyCluster is the context key holding cluster data
const ContextKeyCluster contextKey = "cluster"

// ContextKeyTLSConfig is the context key holding the TLS configuration
const ContextKeyTLSConfig contextKey = "tlsConfig"
