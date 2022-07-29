package utils

import (
	"io/ioutil"
	"log"
	"path/filepath"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"

	"k8s.io/client-go/kubernetes/scheme"
)

// GetObjectFromYaml convert the kubernetes object defined in yaml file to kubernetes client object
func GetObjectFromYaml(yamlPath, namespace string) ([]client.Object, error) {
	data, err := ioutil.ReadFile(filepath.Clean(yamlPath))
	if err != nil {
		return nil, err
	}

	fileAsString := string(data)
	yamlContent := strings.Split(fileAsString, "---")
	retVal := make([]client.Object, 0, len(yamlContent))
	decoder, err := getDecoder()
	if err != nil {
		return nil, err
	}

	for _, f := range yamlContent {
		if f == "\n" || f == "" {
			continue
		}

		obj, _, err := decoder.Decode([]byte(f), nil, nil)
		if err != nil {
			log.Printf("Error while decoding. Err was: %s", err)
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
			log.Fatalf("Unknown kind in yaml file %s", o.GetObjectKind())
		}
	}
	return retVal, nil
}

func getDecoder() (runtime.Decoder, error) {
	err := apiv1.AddToScheme(scheme.Scheme)
	if err != nil {
		return nil, err
	}
	return scheme.Codecs.UniversalDeserializer(), nil
}
