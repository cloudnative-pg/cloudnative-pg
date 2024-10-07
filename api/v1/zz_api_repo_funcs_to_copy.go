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

package v1

// IMPORTANT:
// This file contains the functions that need to be copied from the api/v1 package to the cloudnative-pg/api
// repository. This is currently required because the controller-gen tool cannot generate DeepCopyInto for the
// regexp type. This will be removed once the controller-gen tool supports this feature.

// DeepCopyInto needs to be manually added for the controller-gen compiler to work correctly, given that it cannot
// generate the DeepCopyInto for the regexp type.
// The method is empty because we don't want to transfer the cache when invoking DeepCopyInto.
func (receiver synchronizeReplicasCache) DeepCopyInto(*synchronizeReplicasCache) {}
