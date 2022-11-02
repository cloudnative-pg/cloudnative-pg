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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/spf13/cobra"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	machineryYaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/cloudnative-pg/cloudnative-pg/internal/cmd/plugin"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/management/log"
)

// installationResource is a resource part of the CNPG installation
type installationResource struct {
	// obj is the client object that is part of the installation
	obj client.Object
	// isClusterWide indicates a resource not affected by the namespace
	isClusterWide bool
	// referenceKind is needed to ensure that the yaml was correctly parsed
	referenceKind string
}

type generateExecutor struct {
	ctx                  context.Context
	watchNamespace       string
	namespace            string
	replicas             int32
	userRequestedVersion string
}

func newGenerateCmd() *cobra.Command {
	var version, watchNamespaces string
	var replicas int32
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "generates the manifests needed for the CNPG operator installation",
		RunE: func(cmd *cobra.Command, args []string) error {
			// we consider the namespace only if explicitly passed for this command
			namespace := ""
			if plugin.NamespaceExplicitlyPassed {
				namespace = plugin.Namespace
			}

			command := generateExecutor{
				userRequestedVersion: version,
				namespace:            namespace,
				watchNamespace:       watchNamespaces,
				replicas:             replicas,
				ctx:                  cmd.Context(),
			}
			return command.execute()
		},
	}

	cmd.Flags().StringVar(
		&version,
		"version",
		"",
		"The operator version (<major>.<minor>, e.g. 1.17, 1.16) to install, If not passed defaults to the latest version",
	)
	cmd.Flags().StringVar(
		&watchNamespaces,
		"watch-namespace",
		"",
		"makes the operator watch a single namespace. If empty watches all the namespaces",
	)
	cmd.Flags().Int32Var(
		&replicas,
		"replicas",
		0,
		"Overrides the operator deployment replicas. If zero applies the defaults value from the installation manifest",
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

	if err := applyReconcile(irs, cmd.reconcileNamespaceResource); err != nil {
		return err
	}

	if err := applyReconcile(irs, cmd.reconcileOperatorDeployment); err != nil {
		return err
	}

	if err := applyReconcile(irs, cmd.reconcileOperatorConfig); err != nil {
		return err
	}

	if err := applyReconcile(irs, cmd.reconcileClusterRoleBind); err != nil {
		return err
	}

	if err := applyReconcile(irs, cmd.reconcileMutatingWebhook); err != nil {
		return err
	}

	if err := applyReconcile(irs, cmd.reconcileValidatingWebhook); err != nil {
		return err
	}

	return cmd.printResources(irs)
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
	contextLogger := log.FromContext(cmd.ctx)

	version, err := cmd.getVersion()
	if err != nil {
		return nil, err
	}

	manifestURL := fmt.Sprintf(
		"https://raw.githubusercontent.com/cloudnative-pg/artifacts/%s/manifests/operator-manifest.yaml",
		version,
	)
	contextLogger.Info(
		"fetching installation manifests",
		"branch", version,
		"url", manifestURL,
	)

	return executeGetRequest(cmd.ctx, manifestURL)
}

func (cmd *generateExecutor) getInstallationResourcesFromYAML(rawYaml []byte) ([]installationResource, error) {
	var irs []installationResource
	reader := bufio.NewReader(bytes.NewReader(rawYaml))
	yamlReader := machineryYaml.NewYAMLReader(reader)
	for {
		document, err := yamlReader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		ir, err := cmd.getResourceFromDocument(document)
		if err != nil {
			return nil, err
		}

		irs = append(irs, ir)
	}

	return irs, nil
}

func (cmd *generateExecutor) getResourceFromDocument(document []byte) (installationResource, error) {
	contextLogger := log.FromContext(cmd.ctx)
	supportedResources := []installationResource{
		{obj: &corev1.Namespace{}, isClusterWide: true, referenceKind: "Namespace"},
		{obj: &corev1.ServiceAccount{}, referenceKind: "ServiceAccount"},
		{obj: &corev1.Service{}, referenceKind: "Service"},
		{obj: &corev1.ConfigMap{}, referenceKind: "ConfigMap"},
		{obj: &rbacv1.ClusterRole{}, isClusterWide: true, referenceKind: "ClusterRole"},
		{obj: &rbacv1.ClusterRoleBinding{}, isClusterWide: true, referenceKind: "ClusterRoleBinding"},
		{obj: &appsv1.Deployment{}, referenceKind: "Deployment"},
		{obj: &admissionregistrationv1.MutatingWebhookConfiguration{}, referenceKind: "MutatingWebhookConfiguration"},
		{obj: &admissionregistrationv1.ValidatingWebhookConfiguration{}, referenceKind: "ValidatingWebhookConfiguration"},
		{obj: &apiextensionsv1.CustomResourceDefinition{}, referenceKind: "CustomResourceDefinition"},
	}

	for _, ir := range supportedResources {
		ir := ir
		err := machineryYaml.UnmarshalStrict(document, ir.obj)
		if err != nil {
			continue
		}
		if ir.referenceKind != ir.obj.GetObjectKind().GroupVersionKind().Kind {
			continue
		}

		return ir, nil
	}
	err := errors.New("unsupported yaml resource")
	contextLogger.Error(err, "Could not parse the yaml document", "document", string(document))
	return installationResource{}, err
}

func (cmd *generateExecutor) reconcileOperatorDeployment(dep *appsv1.Deployment) error {
	if cmd.replicas == 0 {
		return nil
	}
	dep.Spec.Replicas = &cmd.replicas
	return nil
}

func (cmd *generateExecutor) reconcileOperatorConfig(cm *corev1.ConfigMap) error {
	if cmd.watchNamespace == "" {
		return nil
	}
	// means it's not the operator configuration configmap
	if cm.Data["POSTGRES_IMAGE_NAME"] == "" {
		return nil
	}

	cm.Data["WATCH_NAMESPACE"] = cmd.watchNamespace

	return nil
}

func (cmd *generateExecutor) reconcileClusterRoleBind(crb *rbacv1.ClusterRoleBinding) error {
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

func (cmd *generateExecutor) getVersion() (string, error) {
	if cmd.userRequestedVersion != "" {
		return fmt.Sprintf("release-%s", cmd.userRequestedVersion), nil
	}

	return cmd.getLatestOperatorVersion()
}

// Branch is an object returned by gitHub query
type Branch struct {
	Name string `json:"name,omitempty"`
}

func (cmd *generateExecutor) getLatestOperatorVersion() (string, error) {
	url := "https://api.github.com/repos/cloudnative-pg/artifacts/branches"
	body, err := executeGetRequest(cmd.ctx, url)
	if err != nil {
		return "", err
	}

	var tags []Branch
	if err := json.Unmarshal(body, &tags); err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", fmt.Errorf("no branches found")
	}

	// we order the slice in reverse order, so the latest version is the first element
	sort.Slice(tags, func(i, j int) bool {
		return tags[i].Name > tags[j].Name
	})

	return tags[0].Name, nil
}

func executeGetRequest(ctx context.Context, url string) ([]byte, error) {
	contextLogger := log.FromContext(ctx)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		contextLogger.Error(err, "Error while visiting url", "url", url)
	}
	defer func() {
		err = resp.Body.Close()
		if err != nil {
			contextLogger.Error(err, "Can't close the connection",
				"url", url,
				"statusCode", resp.StatusCode,
			)
		}
	}()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		contextLogger.Error(err, "Error while reading status response body",
			"url", url,
			"statusCode", resp.StatusCode,
		)
		return nil, err
	}
	if resp.StatusCode > 299 {
		return nil, fmt.Errorf("statusCode=%v while visiting url: %v",
			resp.StatusCode, url)
	}
	return body, nil
}

type reconcileResourceCallback[T client.Object] func(obj T) error

// applyReconcile invokes the passed reconciler for the resources matching T
func applyReconcile[T client.Object](
	irs []installationResource,
	reconciler reconcileResourceCallback[T],
) error {
	for _, ir := range irs {
		if t, ok := ir.obj.(T); ok {
			if err := reconciler(t); err != nil {
				return err
			}
		}
	}
	return nil
}

func (cmd *generateExecutor) isNamespaceEmpty() bool {
	return cmd.namespace == ""
}
