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
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	machineryYaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/versions"
	"github.com/cloudnative-pg/cloudnative-pg/releases"
)

// installationResource is a resource part of the CNPG installation
type installationResource struct {
	// obj is the client object that is part of the installation
	obj client.Object
	// isClusterWide indicates a resource not affected by the namespace
	isClusterWide bool
}

type generateExecutor struct {
	ctx                  context.Context
	watchNamespace       string
	namespace            string
	replicas             int32
	userRequestedVersion string
	postgresImage        string
	logFieldLevel        string
	logFieldTimestamp    string
}

func newGenerateCmd() *cobra.Command {
	var version, watchNamespaces, postgresImage, logFieldLevel, logFieldTimestamp string
	var replicas int32
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "generates the YAML manifests needed to install the CloudNativePG operator",
		RunE: func(cmd *cobra.Command, args []string) error {
			// we consider the namespace only if explicitly passed for this command
			namespace := ""
			if plugin.NamespaceExplicitlyPassed {
				namespace = plugin.Namespace
			}

			command := generateExecutor{
				ctx:                  cmd.Context(),
				watchNamespace:       watchNamespaces,
				namespace:            namespace,
				replicas:             replicas,
				userRequestedVersion: version,
				postgresImage:        postgresImage,
				logFieldLevel:        logFieldLevel,
				logFieldTimestamp:    logFieldTimestamp,
			}
			return command.execute()
		},
	}

	cmd.Flags().StringVar(
		&version,
		"version",
		versions.Version,
		"The version of the operator to install, specified in the '<major>.<minor>.<patch>' format (e.g. 1.17.0). "+
			"The default empty value installs the same version of the used plugin.",
	)

	cmd.Flags().StringVar(
		&watchNamespaces,
		"watch-namespace",
		"",
		"Limit the namespaces to watch. You can pass a list of namespaces through a comma separated string. "+
			"When empty, the operator watches all namespaces",
	)

	cmd.Flags().Int32Var(
		&replicas,
		"replicas",
		0,
		"Number of replicas in the deployment. Default is zero, meaning that no override is applied on the "+
			"installation manifest (normally it is a single replica deployment)",
	)

	cmd.Flags().StringVar(
		&postgresImage,
		"image",
		"",
		"Optional flag to specify a PostgreSQL image to use. If not specified, the default image is used",
	)

	cmd.Flags().StringVar(
		&logFieldLevel,
		"log-field-level",
		"level",
		"JSON log field to report severity in (default: level)",
	)

	cmd.Flags().StringVar(
		&logFieldTimestamp,
		"log-field-timestamp",
		"ts",
		"JSON log field to report timestamp in (default: ts)",
	)

	return cmd
}

func (cmd *generateExecutor) execute() error {
	manifest, err := cmd.getInstallationYAML()
	if err != nil {
		return err
	}

	irs, err := cmd.getInstallationResourcesFromYAML(manifest)
	if err != nil {
		return nil
	}

	cmd.reconcileNamespaceMetadata(irs)

	if err = cmd.reconcileResources(irs); err != nil {
		return err
	}

	return cmd.printResources(irs)
}

func (cmd *generateExecutor) reconcileResources(irs []installationResource) error {
	for _, ir := range irs {
		switch irObjectType := ir.obj.(type) {
		case *appsv1.Deployment:
			if err := cmd.reconcileOperatorDeployment(irObjectType); err != nil {
				return err
			}
		case *corev1.ConfigMap:
			if err := cmd.reconcileOperatorConfigMap(irObjectType); err != nil {
				return err
			}
		case *rbacv1.ClusterRoleBinding:
			if err := cmd.reconcileClusterRoleBinding(irObjectType); err != nil {
				return err
			}
		case *corev1.Namespace:
			if err := cmd.reconcileNamespaceResource(irObjectType); err != nil {
				return err
			}
		case *admissionregistrationv1.ValidatingWebhookConfiguration:
			if err := cmd.reconcileValidatingWebhook(irObjectType); err != nil {
				return err
			}
		case *admissionregistrationv1.MutatingWebhookConfiguration:
			if err := cmd.reconcileMutatingWebhook(irObjectType); err != nil {
				return err
			}
		}
	}
	return nil
}

func (cmd *generateExecutor) printResources(irs []installationResource) error {
	for _, ir := range irs {
		b, err := yaml.Marshal(ir.obj)
		if err != nil {
			return err
		}
		fmt.Print(string(b))
		fmt.Println("---")
	}
	return nil
}

func (cmd *generateExecutor) getInstallationYAML() ([]byte, error) {
	fileName := fmt.Sprintf("cnpg-%s.yaml", cmd.userRequestedVersion)

	out, err := releases.OperatorManifests.ReadFile(fileName)
	if err != nil {
		return nil, fmt.Errorf("release version %s not found", cmd.userRequestedVersion)
	}

	return out, nil
}

func (cmd *generateExecutor) getInstallationResourcesFromYAML(rawYaml []byte) ([]installationResource, error) {
	var irs []installationResource
	reader := bufio.NewReader(bytes.NewReader(rawYaml))
	yamlReader := machineryYaml.NewYAMLReader(reader)

	for {
		document, err := yamlReader.Read()
		switch err {
		case nil:
			ir, err := cmd.getResourceFromDocument(document)
			if err != nil {
				return nil, err
			}
			irs = append(irs, ir)
		case io.EOF:
			return irs, nil
		default:
			return nil, err
		}
	}
}

// getResourceFromDocument returns the installation resource from the given document
func (cmd *generateExecutor) getResourceFromDocument(document []byte) (installationResource, error) {
	/*
		The document is a YAML file that contains a single Kubernetes resource. We need to check if that resource is
		any of the supported resources and, if it is we unmarshal it into the client.Object superclass contained in the
		installationResource struct. This is done to avoid having to create a separate unmarshaler for each resource type.
	*/
	supportedResources := map[string]installationResource{
		"Namespace":                      {obj: &corev1.Namespace{}, isClusterWide: true},
		"ServiceAccount":                 {obj: &corev1.ServiceAccount{}},
		"Service":                        {obj: &corev1.Service{}},
		"ConfigMap":                      {obj: &corev1.ConfigMap{}},
		"ClusterRole":                    {obj: &rbacv1.ClusterRole{}, isClusterWide: true},
		"ClusterRoleBinding":             {obj: &rbacv1.ClusterRoleBinding{}, isClusterWide: true},
		"Deployment":                     {obj: &appsv1.Deployment{}},
		"MutatingWebhookConfiguration":   {obj: &admissionregistrationv1.MutatingWebhookConfiguration{}},
		"ValidatingWebhookConfiguration": {obj: &admissionregistrationv1.ValidatingWebhookConfiguration{}},
		"CustomResourceDefinition":       {obj: &apiextensionsv1.CustomResourceDefinition{}},
	}

	gvk, err := yamlserializer.DefaultMetaFactory.Interpret(document)
	if err != nil {
		return installationResource{}, err
	}

	supportedResource, ok := supportedResources[gvk.Kind]
	if !ok {
		return installationResource{}, fmt.Errorf("unsupported yaml resource: %s", gvk.Kind)
	}

	return supportedResource, machineryYaml.UnmarshalStrict(document, supportedResource.obj)
}

func (cmd *generateExecutor) reconcileOperatorDeployment(dep *appsv1.Deployment) error {
	args := dep.Spec.Template.Spec.Containers[0].Args
	args = append(args, fmt.Sprintf("--log-field-level=%s", cmd.logFieldLevel))
	args = append(args, fmt.Sprintf("--log-field-timestamp=%s", cmd.logFieldTimestamp))

	dep.Spec.Template.Spec.Containers[0].Args = args

	if cmd.replicas == 0 {
		return nil
	}
	dep.Spec.Replicas = &cmd.replicas

	return nil
}

func (cmd *generateExecutor) reconcileOperatorConfigMap(cm *corev1.ConfigMap) error {
	if cmd.watchNamespace == "" {
		return nil
	}
	// means it's not the operator configuration configmap
	if cm.Data["POSTGRES_IMAGE_NAME"] == "" {
		return nil
	}

	cm.Data["WATCH_NAMESPACE"] = cmd.watchNamespace

	if cmd.postgresImage != "" {
		cm.Data["POSTGRES_IMAGE_NAME"] = cmd.postgresImage
	}

	return nil
}

func (cmd *generateExecutor) reconcileClusterRoleBinding(crb *rbacv1.ClusterRoleBinding) error {
	if cmd.isNamespaceEmpty() {
		return nil
	}

	for idx := range crb.Subjects {
		crb.Subjects[idx].Namespace = cmd.namespace
	}

	return nil
}

func (cmd *generateExecutor) reconcileNamespaceMetadata(irs []installationResource) {
	if cmd.isNamespaceEmpty() {
		return
	}

	for _, ir := range irs {
		if ir.isClusterWide {
			continue
		}
		ir.obj.SetNamespace(cmd.namespace)
	}
}

func (cmd *generateExecutor) reconcileNamespaceResource(ns *corev1.Namespace) error {
	if cmd.isNamespaceEmpty() {
		return nil
	}

	ns.Name = cmd.namespace

	return nil
}

func (cmd *generateExecutor) reconcileValidatingWebhook(
	wh *admissionregistrationv1.ValidatingWebhookConfiguration,
) error {
	if cmd.isNamespaceEmpty() {
		return nil
	}

	for i := range wh.Webhooks {
		wh.Webhooks[i].ClientConfig.Service.Namespace = cmd.namespace
	}
	return nil
}

func (cmd *generateExecutor) reconcileMutatingWebhook(wh *admissionregistrationv1.MutatingWebhookConfiguration) error {
	if cmd.isNamespaceEmpty() {
		return nil
	}

	for i := range wh.Webhooks {
		wh.Webhooks[i].ClientConfig.Service.Namespace = cmd.namespace
	}
	return nil
}

func (cmd *generateExecutor) isNamespaceEmpty() bool {
	return cmd.namespace == ""
}
