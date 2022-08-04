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
