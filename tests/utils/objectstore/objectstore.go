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

// Package objectstore contains the functions to deploy the S3-compatible
// object storage service used by the e2e tests (currently RustFS) and to
// query it through an AWS CLI client pod
package objectstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/avast/retry-go/v5"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/pods"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
)

const (
	// rustfsImage is the image used to run a RustFS server
	rustfsImage = "docker.io/rustfs/rustfs:1.0.0-beta.8"
	// awsCliImage is the image used to run the AWS CLI S3 client
	awsCliImage = "docker.io/amazon/aws-cli:2.35.1"

	// AccessKeyID is the access key used to authenticate against the object store
	AccessKeyID = "objectstore"
	// SecretAccessKey is the secret key used to authenticate against the object store
	SecretAccessKey = "objectstore123"
)

// Env contains all the information related or required by the object storage
// deployment and used by the functions on every test
type Env struct {
	Client       *corev1.Pod
	CaPair       *certs.KeyPair
	CaSecretObj  corev1.Secret
	ServiceName  string
	Namespace    string
	CaSecretName string
	TLSSecret    string
	Timeout      uint
}

// Setup contains the resources needed for a working object storage server
// deployment: a PersistentVolumeClaim, a Deployment and a Service
type Setup struct {
	PersistentVolumeClaim corev1.PersistentVolumeClaim
	Deployment            appsv1.Deployment
	Service               corev1.Service
}

// TagSet contains the tags of an object in the object store
type TagSet struct {
	Tags map[string]string
}

// installObjectStore installs the object storage server in a given namespace
func installObjectStore(
	env *environment.TestingEnvironment,
	setup Setup,
	timeoutSeconds uint,
) error {
	if err := env.Client.Create(env.Ctx, &setup.PersistentVolumeClaim); err != nil {
		return err
	}
	if err := env.Client.Create(env.Ctx, &setup.Deployment); err != nil {
		return err
	}
	err := retry.New(
		retry.Attempts(timeoutSeconds),
		retry.Delay(time.Second),
		retry.DelayType(retry.FixedDelay)).
		Do(
			func() error {
				deployment := &appsv1.Deployment{}
				if err := env.Client.Get(
					env.Ctx,
					client.ObjectKey{Namespace: setup.Deployment.Namespace, Name: setup.Deployment.Name},
					deployment,
				); err != nil {
					return err
				}
				if deployment.Status.ReadyReplicas != *setup.Deployment.Spec.Replicas {
					return fmt.Errorf("not all replicas are ready. Expected %v, found %v",
						*setup.Deployment.Spec.Replicas,
						deployment.Status.ReadyReplicas,
					)
				}
				return nil
			},
		)
	if err != nil {
		return err
	}
	err = env.Client.Create(env.Ctx, &setup.Service)
	return err
}

// defaultSetup returns the definition for the default object storage setup
func defaultSetup(namespace string) (Setup, error) {
	pvc, err := defaultPVC(namespace)
	if err != nil {
		return Setup{}, err
	}
	deployment := defaultDeployment(namespace, pvc)
	service := defaultSVC(namespace)
	setup := Setup{
		PersistentVolumeClaim: pvc,
		Deployment:            deployment,
		Service:               service,
	}
	return setup, nil
}

// defaultDeployment returns a default Deployment for the RustFS server
func defaultDeployment(namespace string, pvc corev1.PersistentVolumeClaim) appsv1.Deployment {
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}

	deployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "object-store",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "object-store"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "object-store"},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvc.Name,
								},
							},
						},
						{
							Name: "logs",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "object-store",
							Image:   rustfsImage,
							Command: []string{"/usr/bin/rustfs"},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 9000,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "RUSTFS_ADDRESS",
									Value: ":9000",
								},
								{
									Name:  "RUSTFS_VOLUMES",
									Value: "/data",
								},
								{
									Name:  "RUSTFS_REGION",
									Value: "us-east-1",
								},
								{
									Name:  "RUSTFS_ACCESS_KEY",
									Value: AccessKeyID,
								},
								{
									Name:  "RUSTFS_SECRET_KEY",
									Value: SecretAccessKey,
								},
								{
									Name:  "RUSTFS_CONSOLE_ENABLE",
									Value: "false",
								},
								{
									Name:  "RUSTFS_OBS_LOG_DIRECTORY",
									Value: "/logs",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
								{
									Name:      "logs",
									MountPath: "/logs",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.IntOrString{
											IntVal: 9000,
										},
									},
								},
								InitialDelaySeconds: 30,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.IntOrString{
											IntVal: 9000,
										},
									},
								},
								InitialDelaySeconds: 30,
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								SeccompProfile:           seccompProfile,
							},
						},
					},
					SecurityContext: &corev1.PodSecurityContext{
						SeccompProfile: seccompProfile,
						// The RustFS image runs as the non-root `rustfs`
						// user (10001): make the data volume and the
						// projected TLS certificates group-accessible
						FSGroup: ptr.To(int64(10001)),
					},
				},
			},
		},
	}
	return deployment
}

// defaultSVC returns a default Service for the object storage server
func defaultSVC(namespace string) corev1.Service {
	service := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "object-store",
			Namespace: namespace,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Port: 9000,
					TargetPort: intstr.IntOrString{
						IntVal: 9000,
					},
					Protocol: corev1.ProtocolTCP,
				},
			},
			Selector: map[string]string{"app": "object-store"},
		},
	}
	return service
}

// defaultPVC returns a default PVC for the object storage server
func defaultPVC(namespace string) (corev1.PersistentVolumeClaim, error) {
	const claimName = "object-store-pv-claim"
	storageClass, ok := os.LookupEnv("E2E_DEFAULT_STORAGE_CLASS")
	if !ok {
		return corev1.PersistentVolumeClaim{}, fmt.Errorf("storage class not defined")
	}

	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": resource.MustParse("4Gi"),
				},
			},
			StorageClassName: &storageClass,
		},
	}
	return pvc, nil
}

// sslSetup returns the definition for an object storage setup using SSL
func sslSetup(namespace string) (Setup, error) {
	setup, err := defaultSetup(namespace)
	if err != nil {
		return Setup{}, err
	}
	const tlsVolumeName = "secret-volume"
	const tlsVolumeMountPath = "/etc/secrets/certs"
	var secretMode int32 = 0o600
	// RustFS enables TLS when it finds `rustfs_cert.pem` and `rustfs_key.pem`
	// in the directory pointed to by RUSTFS_TLS_PATH
	setup.Deployment.Spec.Template.Spec.Containers[0].Env = append(
		setup.Deployment.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{
			Name:  "RUSTFS_TLS_PATH",
			Value: tlsVolumeMountPath,
		})
	setup.Deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(
		setup.Deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      tlsVolumeName,
			MountPath: tlsVolumeMountPath,
		})
	setup.Deployment.Spec.Template.Spec.Volumes = append(
		setup.Deployment.Spec.Template.Spec.Volumes,
		corev1.Volume{
			Name: tlsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "object-store-tls-secret",
								},
								Items: []corev1.KeyToPath{
									{
										Key:  "tls.crt",
										Path: "rustfs_cert.pem",
									},
									{
										Key:  "tls.key",
										Path: "rustfs_key.pem",
									},
								},
							},
						},
					},
					DefaultMode: &secretMode,
				},
			},
		},
	)
	// We also need to set the probes to HTTPS. Kubernetes will not verify
	// the certificates, but this way we can connect
	setup.Deployment.Spec.Template.Spec.Containers[0].LivenessProbe.HTTPGet.Scheme = corev1.URISchemeHTTPS
	setup.Deployment.Spec.Template.Spec.Containers[0].ReadinessProbe.HTTPGet.Scheme = corev1.URISchemeHTTPS
	return setup, nil
}

// defaultClient returns the default Pod definition for the S3 client
func defaultClient(namespace string) corev1.Pod {
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}

	clientPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "s3-client",
			Labels:    map[string]string{"run": "s3-client"},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "s3-client",
					Image: awsCliImage,
					Env: []corev1.EnvVar{
						{
							Name:  "AWS_ENDPOINT_URL",
							Value: "http://object-store.object-store:9000",
						},
						{
							Name:  "AWS_ACCESS_KEY_ID",
							Value: AccessKeyID,
						},
						{
							Name:  "AWS_SECRET_ACCESS_KEY",
							Value: SecretAccessKey,
						},
						{
							Name:  "AWS_DEFAULT_REGION",
							Value: "us-east-1",
						},
						// The CRC-based default checksums introduced in
						// AWS CLI 2.23 are not supported by every
						// S3-compatible object store
						{
							Name:  "AWS_REQUEST_CHECKSUM_CALCULATION",
							Value: "when_required",
						},
						{
							Name:  "AWS_RESPONSE_CHECKSUM_VALIDATION",
							Value: "when_required",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						SeccompProfile:           seccompProfile,
					},
					Command: []string{"sleep", "3600"},
				},
			},
			SecurityContext: &corev1.PodSecurityContext{
				SeccompProfile: seccompProfile,
			},
			DNSPolicy:     corev1.DNSClusterFirst,
			RestartPolicy: corev1.RestartPolicyAlways,
		},
	}
	return clientPod
}

// sslClient returns the Pod definition for an S3 client using SSL
func sslClient(namespace string) corev1.Pod {
	const (
		caSecretName       = "object-store-ca-secret"
		tlsVolumeName      = "secret-volume"
		tlsVolumeMountPath = "/etc/secrets/ca"
	)
	var secretMode int32 = 0o600

	clientPod := defaultClient(namespace)
	clientPod.Spec.Volumes = append(clientPod.Spec.Volumes,
		corev1.Volume{
			Name: tlsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  caSecretName,
					DefaultMode: &secretMode,
				},
			},
		},
	)
	clientPod.Spec.Containers[0].VolumeMounts = append(
		clientPod.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      tlsVolumeName,
			MountPath: tlsVolumeMountPath,
		},
	)
	clientPod.Spec.Containers[0].Env[0].Value = "https://object-store.object-store:9000"
	clientPod.Spec.Containers[0].Env = append(
		clientPod.Spec.Containers[0].Env,
		corev1.EnvVar{
			Name:  "AWS_CA_BUNDLE",
			Value: tlsVolumeMountPath + "/ca.crt",
		},
	)

	return clientPod
}

// Deploy will create a full object storage deployment defined in the storeEnv variable
func Deploy(storeEnv *Env, env *environment.TestingEnvironment) (*corev1.Pod, error) {
	var err error
	storeEnv.CaPair, err = certs.CreateRootCA(storeEnv.Namespace, "object-store")
	if err != nil {
		return nil, err
	}

	storeEnv.CaSecretObj = *storeEnv.CaPair.GenerateCASecret(storeEnv.Namespace, storeEnv.CaSecretName)
	if _, err = objects.Create(env.Ctx, env.Client, &storeEnv.CaSecretObj); err != nil {
		return nil, err
	}

	// sign and create secret using CA certificate and key
	serverPair, err := storeEnv.CaPair.CreateAndSignPair("object-store", certs.CertTypeServer,
		[]string{"object-store.useless.domain.not.verified", "object-store.object-store"},
	)
	if err != nil {
		return nil, err
	}

	serverSecret := serverPair.GenerateCertificateSecret(storeEnv.Namespace, storeEnv.TLSSecret)
	if err = env.Client.Create(env.Ctx, serverSecret); err != nil {
		return nil, err
	}

	setup, err := sslSetup(storeEnv.Namespace)
	if err != nil {
		return nil, err
	}
	if err = installObjectStore(env, setup, storeEnv.Timeout); err != nil {
		return nil, err
	}

	clientPod := sslClient(storeEnv.Namespace)

	return &clientPod, pods.CreateAndWaitForReady(env.Ctx, env.Client, &clientPod, 240)
}

func (m *Env) getCaSecret(env *environment.TestingEnvironment, namespace string) (*corev1.Secret, error) {
	var certSecret corev1.Secret
	if err := env.Client.Get(env.Ctx,
		types.NamespacedName{
			Namespace: m.Namespace,
			Name:      m.CaSecretName,
		}, &certSecret); err != nil {
		return nil, err
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.CaSecretName,
			Namespace: namespace,
		},
		Data: certSecret.Data,
		Type: certSecret.Type,
	}, nil
}

// CreateCaSecret creates the certificates required to authenticate against the object storage service
func (m *Env) CreateCaSecret(env *environment.TestingEnvironment, namespace string) error {
	caSecret, err := m.getCaSecret(env, namespace)
	if err != nil {
		return err
	}
	_, err = objects.Create(env.Ctx, env.Client, caSecret)
	return err
}

// CountFiles uses the client pod to count the amount of files matching the
// given `path`
func CountFiles(storeEnv *Env, path string) (value int, err error) {
	var stdout string
	stdout, _, err = run.Unchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		storeEnv.Namespace,
		storeEnv.Client.Name,
		composeFindCmd(path)))
	if err != nil {
		return -1, err
	}
	value, err = strconv.Atoi(strings.TrimSpace(stdout))
	return value, err
}

// ListFiles uses the client pod to list the paths matching the given `path`
func ListFiles(storeEnv *Env, path string) (string, error) {
	var stdout string
	stdout, _, err := run.Unchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		storeEnv.Namespace,
		storeEnv.Client.Name,
		composeListFiles(path)))
	if err != nil {
		return "", err
	}
	return strings.Trim(stdout, "\n"), nil
}

// globToRegexp converts a path glob, where `*` matches any sequence of
// characters including `/`, into an anchored extended regular expression
func globToRegexp(pattern string) string {
	fragments := strings.Split(pattern, "*")
	for i, fragment := range fragments {
		fragments[i] = regexp.QuoteMeta(fragment)
	}
	return "^" + strings.Join(fragments, ".*") + "$"
}

// listFilesScript builds a shell script printing every object in the store
// as a `bucket/key` line, filtered by the glob expressed in the given path.
// A failure enumerating the buckets aborts the script, so an unreachable
// endpoint is not mistaken for an empty store, and the per-bucket listings
// run in parallel (eight at a time) to keep the cost of a poll from growing
// linearly with the number of buckets. The script ends with the pipeline
// emitting the matches, so callers can append further stages (e.g. `| wc -l`)
func listFilesScript(path string) string {
	return fmt.Sprintf(
		`buckets=$(aws s3api list-buckets --query "Buckets[].Name" --output text) || exit 1; `+
			`dir=$(mktemp -d) || exit 1; trap "rm -rf $dir" EXIT; n=0; `+
			`for b in $buckets; do `+
			`aws s3api list-objects-v2 --bucket "$b" --query "Contents[].Key" --output text | `+
			`tr "\t" "\n" | sed "s|^|$b/|" > "$dir/$b" & `+
			`n=$((n+1)); [ $((n %% 8)) -eq 0 ] && wait; `+
			`done; wait; `+
			`cat /dev/null "$dir"/* 2>/dev/null | { grep -E "%v" || true; }`,
		globToRegexp(path))
}

// composeListFiles builds the command to list the filenames matching a given path
func composeListFiles(path string) string {
	return fmt.Sprintf("sh -c '%v'", listFilesScript(path))
}

// composeCleanFiles builds the command removing every object under the given
// `bucket[/prefix]` path
func composeCleanFiles(path string) string {
	return fmt.Sprintf(`sh -c 'aws s3 rm --recursive "s3://%v"'`, path)
}

// composeFindCmd builds the command counting the objects matching a given path
func composeFindCmd(path string) string {
	return fmt.Sprintf("sh -c '%v | wc -l'", listFilesScript(path))
}

// GetFileTags will use the client pod to retrieve the tags in a specified path
func GetFileTags(storeEnv *Env, path string) (TagSet, error) {
	var output TagSet
	// Make sure we have a registered backup to access
	out, _, err := run.UncheckedRetry(fmt.Sprintf(
		"kubectl exec -n %v %v -- sh -c '%v | head -n1'",
		storeEnv.Namespace,
		storeEnv.Client.Name,
		listFilesScript(path)))
	if err != nil {
		return output, err
	}

	walFile := strings.TrimSpace(out)
	bucket, key, found := strings.Cut(walFile, "/")
	if !found {
		return output, fmt.Errorf("no file matching %q found in the object store", path)
	}

	stdout, _, err := run.UncheckedRetry(fmt.Sprintf(
		"kubectl exec -n %v %v -- aws s3api get-object-tagging --bucket %v --key %v",
		storeEnv.Namespace,
		storeEnv.Client.Name,
		bucket,
		key))
	if err != nil {
		return output, err
	}

	var tagging struct {
		TagSet []struct {
			Key   string `json:"Key"`
			Value string `json:"Value"`
		} `json:"TagSet"`
	}
	if err := json.Unmarshal([]byte(stdout), &tagging); err != nil {
		return output, err
	}
	output.Tags = make(map[string]string, len(tagging.TagSet))
	for _, tag := range tagging.TagSet {
		output.Tags[tag.Key] = tag.Value
	}
	return output, nil
}

// TestBarmanConnectivity validates the barman connectivity to the object storage endpoint
func TestBarmanConnectivity(
	namespace,
	clusterName,
	primaryPodName,
	id,
	key string,
	svcName string,
) (bool, error) {
	env := fmt.Sprintf("export AWS_CA_BUNDLE=%s;export AWS_ACCESS_KEY_ID=%s;export AWS_SECRET_ACCESS_KEY=%s;",
		postgres.BarmanBackupEndpointCACertificateLocation, id, key)

	endpointURL := fmt.Sprintf("https://%s:9000", svcName)
	destinationPath := fmt.Sprintf("s3://%s/", "not-evaluated")
	cmd := fmt.Sprintf("barman-cloud-check-wal-archive --cloud-provider aws-s3 --endpoint-url %s %s %s --test",
		endpointURL, destinationPath, clusterName)

	stdout, stderr, err := run.Unchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -c postgres -- /bin/bash -c \"%s %s\"",
		namespace,
		primaryPodName,
		env,
		cmd,
	))
	if err != nil {
		return false, fmt.Errorf("barman connectivity test failed: %w (stdout: %s, stderr: %s)", err, stdout, stderr)
	}
	return true, nil
}

// CleanFiles removes every object under the given `bucket[/prefix]` path
func CleanFiles(storeEnv *Env, path string) (string, error) {
	var stdout string
	stdout, _, err := run.Unchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		storeEnv.Namespace,
		storeEnv.Client.Name,
		composeCleanFiles(path)))
	if err != nil {
		return "", err
	}
	return strings.Trim(stdout, "\n"), nil
}

// GetFilePath gets the glob matching WAL/backup objects in a configured bucket
func GetFilePath(serverName, fileName string) string {
	// the * globs enable matching these typical paths:
	// 	bucketName/serverName/base/20220618T140300/data.tar
	// 	bucketName/serverName/wals/0000000100000000/000000010000000000000002.gz
	//  bucketName/serverName/wals/00000002.history.gz
	return filepath.Join("*", serverName, "*", fileName)
}
