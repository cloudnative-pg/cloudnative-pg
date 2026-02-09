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

package utils

const (
	// DatabaseFinalizerName is the name of the finalizer
	// triggering the deletion of the database
	DatabaseFinalizerName = MetadataNamespace + "/deleteDatabase"

	// PublicationFinalizerName is the name of the finalizer
	// triggering the deletion of the publication
	PublicationFinalizerName = MetadataNamespace + "/deletePublication"

	// SubscriptionFinalizerName is the name of the finalizer
	// triggering the deletion of the subscription
	SubscriptionFinalizerName = MetadataNamespace + "/deleteSubscription"

	// PluginFinalizerName is the name of the finalizer
	// triggering the cleanup of a plugin when its service is deleted
	PluginFinalizerName = MetadataNamespace + "/cleanupPlugin"
)
