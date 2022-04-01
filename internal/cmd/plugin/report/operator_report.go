/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2022 EnterpriseDB Corporation.
*/

// Package report implements the kubectl-cnp report command
package report

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/fileutils"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin"
	"github.com/EnterpriseDB/cloud-native-postgresql/internal/cmd/plugin/report/deployments"
)

// report contains the data to be printed by the `report` plugin
type report struct {
	deployment  appsv1.Deployment
	operatorPod corev1.Pod
	secrets     []namedObject
	configs     []namedObject
	events      corev1.EventList
}

type namedObject struct {
	Name   string
	Object interface{}
}

// Operator implements the "report operator" subcommand
// Produces a zip file containing
//  - operator deployment
//  - operator pod definition
//  - operator configuration Configmap and Secret key (if any)
//  - events in the operator namespace
//  - kubernetes environment information (server part of `kubectl version`)
func Operator(ctx context.Context, format plugin.OutputFormat,
	file string, stopRedaction bool) error {
	secretRedactor := redactSecret
	configMapRedactor := redactConfigMap
	if stopRedaction {
		secretRedactor = passSecret
		configMapRedactor = passConfigMap
		fmt.Println("WARNING: secret Redaction is OFF. Use it with caution")
	}

	operatorDeployment, err := deployments.GetOperatorDeployment(ctx)
	if err != nil {
		return fmt.Errorf("could not get operator deployment: %w", err)
	}

	operatorPod, err := deployments.GetOperatorPod(ctx)
	if err != nil {
		return fmt.Errorf("could not get operator pod: %w", err)
	}

	// TODO: parse configmap and secrets names from the deployment, as the client
	// may have overridden the defaults.
	// Currently we're getting the defaults only
	defaultSecrets := []string{
		"postgresql-operator-ca-secret",
		"postgresql-operator-webhook-cert",
		"postgresql-operator-controller-manager-config",
	}
	secrets := make([]namedObject, 0, len(defaultSecrets))
	for _, ss := range defaultSecrets {
		var secret corev1.Secret

		err := plugin.Client.Get(ctx, types.NamespacedName{Name: ss, Namespace: operatorPod.Namespace}, &secret)
		if err != nil {
			e1, ok := err.(*errors.StatusError)
			if ok && metav1.StatusReasonNotFound == e1.ErrStatus.Reason {
				continue
			}
			return fmt.Errorf("could not get secret '%s': %v", ss, err)
		}
		secrets = append(secrets, namedObject{Name: ss, Object: secretRedactor(secret)})
	}

	configMaps := []string{"postgresql-operator-controller-manager-config"}
	configs := make([]namedObject, 0, len(configMaps))
	for _, cm := range configMaps {
		var config corev1.ConfigMap

		err := plugin.Client.Get(ctx, types.NamespacedName{Name: cm, Namespace: operatorPod.Namespace}, &config)
		if err != nil {
			e1, ok := err.(*errors.StatusError)
			if ok && metav1.StatusReasonNotFound == e1.ErrStatus.Reason {
				continue
			}
			return fmt.Errorf("could not get config '%s': %v", cm, err)
		}

		configs = append(configs, namedObject{Name: cm, Object: configMapRedactor(config)})
	}

	var events corev1.EventList
	err = plugin.Client.List(ctx, &events, client.InNamespace(operatorPod.Namespace))
	if err != nil {
		return fmt.Errorf("could not get events: %w", err)
	}

	rep := report{
		deployment:  operatorDeployment,
		operatorPod: operatorPod,
		secrets:     secrets,
		configs:     configs,
		events:      events,
	}

	err = writeReport(rep, format, file)
	if err != nil {
		return fmt.Errorf("could not write report: %w", err)
	}

	return nil
}

// writerReport writes a zip with the various report parts to file
func writeReport(rep report, format plugin.OutputFormat, file string) (err error) {
	var outputFile *os.File

	if exist, _ := fileutils.FileExists(file); exist {
		return fmt.Errorf("file already exist will not overwrite")
	}

	outputFile, err = os.Create(filepath.Clean(file))
	if err != nil {
		return fmt.Errorf("could not create zip file: %w", err)
	}

	defer func() {
		errF := outputFile.Sync()
		if errF != nil && err == nil {
			err = fmt.Errorf("could not flush the zip file: %w", errF)
		}

		errF = outputFile.Close()
		if errF != nil && err == nil {
			err = fmt.Errorf("could not close the zip file: %w", errF)
		}
	}()

	zipper := zip.NewWriter(outputFile)
	defer func() {
		var errZ error
		if errZ = zipper.Flush(); errZ != nil {
			if err == nil {
				err = fmt.Errorf("could not flush the zip: %w", errZ)
			}
		}

		if errZ = zipper.Close(); errZ != nil {
			if err == nil {
				err = fmt.Errorf("could not close the zip: %w", errZ)
			}
		}
	}()

	err = generateZipContent(rep, zipper, format)

	return err
}

func generateZipContent(rep report, zipper *zip.Writer, format plugin.OutputFormat) (err error) {
	err = addContentToZip(rep.deployment, "deployment", zipper, format)
	if err != nil {
		return
	}
	err = addContentToZip(rep.operatorPod, "operator-pod", zipper, format)
	if err != nil {
		return
	}
	err = addObjectsToZip(rep.configs, zipper, format)
	if err != nil {
		return
	}
	err = addObjectsToZip(rep.secrets, zipper, format)
	if err != nil {
		return
	}
	err = addContentToZip(rep.events, "events", zipper, format)
	if err != nil {
		return
	}

	return
}

func addContentToZip(c interface{}, name string, zipper *zip.Writer, format plugin.OutputFormat) error {
	var writer io.Writer
	writer, err := zipper.Create(name + "." + string(format))
	if err != nil {
		return fmt.Errorf("could not add '%s' to zip: %w", name, err)
	}

	if err = plugin.Print(c, format, writer); err != nil {
		return fmt.Errorf("could not print '%s': %w", name, err)
	}
	return nil
}

func addObjectsToZip(objects []namedObject, zipper *zip.Writer, format plugin.OutputFormat) error {
	for _, obj := range objects {
		var objF io.Writer
		objF, err := zipper.Create(obj.Name + "." + string(format))
		if err != nil {
			return fmt.Errorf("could not add object '%s' to zip: %w", obj, err)
		}

		if err = plugin.Print(obj.Object, format, objF); err != nil {
			return fmt.Errorf("could not print: %w", err)
		}
	}
	return nil
}
