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
	"bytes"
	"fmt"
	"log"

	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// ParseObjectsFromYAML parses a series of kubernetes objects defined in a YAML payload,
// into the specified namespace
func ParseObjectsFromYAML(data []byte, namespace string) ([]client.Object, error) {
	wrapErr := func(err error) error { return fmt.Errorf("while parsingObjectsFromYAML: %w", err) }
	err := apiv1.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, wrapErr(err)
	}
	decoder := scheme.Codecs.UniversalDeserializer()

	sections := bytes.Split(data, []byte("---"))
	objects := make([]client.Object, 0, len(sections))

	for _, section := range sections {
		if string(bytes.TrimSpace(section)) == "\n" || len(bytes.TrimSpace(section)) == 0 {
			continue
		}

		obj, _, err := decoder.Decode(section, nil, nil)
		if err != nil {
			log.Printf("ERROR decoding YAML: %v", err)
			return nil, wrapErr(err)
		}
		o, ok := obj.(client.Object)
		if !ok {
			err = fmt.Errorf("could not cast to client.Object: %v", obj)
			log.Printf("ERROR %v", err)
			return nil, wrapErr(err)
		}
		o.SetNamespace(namespace)
		objects = append(objects, o)
	}
	return objects, nil
}
