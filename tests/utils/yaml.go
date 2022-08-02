package utils

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// CreateObjectsFromYAML creates kubernetes objects defined in a YAML file, in the namespace
// NOTE: this function is meant to be used in Tests. It only recognizes a subset of
//  kubernetes and CNPG object types, throwing errors for any other object types
func CreateObjectsFromYAML(yamlPath, namespace string) ([]client.Object, error) {
	data, err := ioutil.ReadFile(filepath.Clean(yamlPath))
	if err != nil {
		return nil, err
	}

	sections := bytes.Split(data, []byte("---"))
	retVal := make([]client.Object, 0, len(sections))
	decoder, err := getDecoder()
	if err != nil {
		return nil, err
	}

	for _, section := range sections {
		if string(bytes.TrimSpace(section)) == "\n" || len(bytes.TrimSpace(section)) == 0 {
			continue
		}

		obj, _, err := decoder.Decode(section, nil, nil)
		if err != nil {
			log.Printf("ERROR CreateObjectsFromYAML decoding: %s", err)
			return nil, err
		}
		switch o := obj.(type) {
		case *apiv1.Cluster:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		case *apiv1.Backup:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		case *apiv1.ScheduledBackup:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		case *apiv1.Pooler:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		case *corev1.ConfigMap:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		case *corev1.Secret:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		case *corev1.Service:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		case *batchv1.Job:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		case *appsv1.Deployment:
			o.SetNamespace(namespace)
			retVal = append(retVal, o)
		default:
			err := fmt.Errorf("while parsing yaml file. Unexpected object kind: %v",
				o.GetObjectKind())
			log.Printf("ERROR creating objects from YAML: %v", err)
			return nil, err
		}
	}
	return retVal, nil
}

// getDecoder returns a Kubernetes parser
func getDecoder() (runtime.Decoder, error) {
	err := apiv1.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, err
	}
	return scheme.Codecs.UniversalDeserializer(), nil
}
