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

// Package minio contains all the require functions to setup a MinIO deployment and
// query this MinIO deployment using the MinIO API
package minio

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/avast/retry-go/v5"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/certs"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/clusterutils"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/environment"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/exec"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/forwardconnection"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/namespaces"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/objects"
	postgres2 "github.com/cloudnative-pg/cloudnative-pg/tests/utils/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/run"
	"github.com/cloudnative-pg/cloudnative-pg/tests/utils/secrets"
)

const (
	// minioImage is the image used to run a MinIO server
	minioImage          = "docker.io/minio/minio:RELEASE.2025-09-07T16-13-09Z"
	minioDeploymentName = "minio"
	minioNamespace      = "minio"
	// minioAccessKey is the access key of the shared MinIO deployment
	minioAccessKey = "minio"
	// minioSecretKey is the secret key of the shared MinIO deployment
	minioSecretKey = "minio123" // #nosec G101
	// ObjectStorageCredentialsSecretName is the name of the secret that holds
	// the MinIO credentials, referenced by the Cluster fixtures via
	// spec.backup.barmanObjectStore.s3Credentials.
	ObjectStorageCredentialsSecretName = "backup-storage-creds" // #nosec G101
)

// Instance carries everything an e2e spec needs to talk to the shared MinIO
// deployment: the in-cluster coordinates of the service and its TLS material,
// plus a port-forwarded MinIO API client.
type Instance struct {
	ServiceName      string
	Namespace        string
	CaSecretName     string
	TLSSecret        string
	ctx              context.Context
	CrudClient       client.Client
	Interface        kubernetes.Interface
	RestClientConfig *rest.Config
	MinioClient      *minio.Client
	Forwarder        *forwardconnection.ForwardConnection
}

// Setup contains the resources needed for a working minio server deployment:
// a PersistentVolumeClaim, a Deployment and a Service
type Setup struct {
	PersistentVolumeClaim corev1.PersistentVolumeClaim
	Deployment            appsv1.Deployment
	Service               corev1.Service
}

// TagSet will contain the `tagset` section of the minio output command
type TagSet struct {
	Tags map[string]string `json:"tagset"`
}

// installMinio installs minio in a given namespace.
func installMinio(
	env *environment.TestingEnvironment,
	minioSetup Setup,
	timeoutSeconds uint,
) error {
	if err := env.Client.Create(
		env.Ctx, &minioSetup.PersistentVolumeClaim); err != nil &&
		!apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := env.Client.Create(env.Ctx, &minioSetup.Deployment); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	if err := waitForDeploymentReady(
		env, minioSetup.Deployment.Namespace, minioSetup.Deployment.Name, timeoutSeconds,
	); err != nil {
		return err
	}
	if err := env.Client.Create(env.Ctx, &minioSetup.Service); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func waitForDeploymentReady(
	env *environment.TestingEnvironment,
	namespace,
	deploymentName string,
	timeoutSeconds uint,
) error {
	if timeoutSeconds == 0 {
		timeoutSeconds = 240
	}

	return retry.New(
		retry.Attempts(timeoutSeconds),
		retry.Delay(time.Second),
		retry.DelayType(retry.FixedDelay)).
		Do(
			func() error {
				deployment := &appsv1.Deployment{}
				if err := env.Client.Get(
					env.Ctx,
					client.ObjectKey{Namespace: namespace, Name: deploymentName},
					deployment,
				); err != nil {
					return err
				}

				expectedReplicas := int32(1)
				if deployment.Spec.Replicas != nil {
					expectedReplicas = *deployment.Spec.Replicas
				}
				if deployment.Status.ReadyReplicas != expectedReplicas {
					return fmt.Errorf("not all replicas are ready. Expected %v, found %v",
						expectedReplicas,
						deployment.Status.ReadyReplicas,
					)
				}
				return nil
			},
		)
}

// defaultSetup returns the definition for the default minio setup
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

// defaultDeployment returns a default Deployment for minio
func defaultDeployment(namespace string, minioPVC corev1.PersistentVolumeClaim) appsv1.Deployment {
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}

	minioDeployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minio",
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "minio"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "minio"},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "data",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: minioPVC.Name,
								},
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name: "minio",
							// Latest Apache License release
							Image:   minioImage,
							Command: nil,
							Args:    []string{"server", "data"},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 9000,
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "MINIO_ACCESS_KEY",
									Value: "minio",
								},
								{
									Name:  "MINIO_SECRET_KEY",
									Value: "minio123",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/data",
								},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/minio/health/live",
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
										Path: "/minio/health/ready",
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
					},
				},
			},
		},
	}
	return minioDeployment
}

// defaultSVC returns a default Service for minio
func defaultSVC(namespace string) corev1.Service {
	minioService := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "minio-service",
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
			Selector: map[string]string{"app": "minio"},
		},
	}
	return minioService
}

// defaultPVC returns a default PVC for minio
func defaultPVC(namespace string) (corev1.PersistentVolumeClaim, error) {
	const claimName = "minio-pv-claim"
	storageClass, ok := os.LookupEnv("E2E_DEFAULT_STORAGE_CLASS")
	if !ok {
		return corev1.PersistentVolumeClaim{}, fmt.Errorf("storage class not defined")
	}

	minioPVC := corev1.PersistentVolumeClaim{
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
	return minioPVC, nil
}

// sslSetup returns the definition for a minio setup using SSL
func sslSetup(namespace string) (Setup, error) {
	setup, err := defaultSetup(namespace)
	if err != nil {
		return Setup{}, err
	}
	const tlsVolumeName = "secret-volume"
	const tlsVolumeMountPath = "/etc/secrets/certs"
	var secretMode int32 = 0o600
	setup.Deployment.Spec.Template.Spec.Containers[0].Args = []string{
		"--certs-dir", tlsVolumeMountPath, "server", "/data",
	}
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
									Name: "minio-server-tls-secret",
								},
								Items: []corev1.KeyToPath{
									{
										Key:  "tls.crt",
										Path: "public.crt",
									},
									{
										Key:  "tls.key",
										Path: "private.key",
									},
								},
							},
						},
						{
							Secret: &corev1.SecretProjection{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "minio-server-ca-secret",
								},
								Items: []corev1.KeyToPath{
									{
										Key:  "ca.crt",
										Path: "CAs/public.crt",
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

// CleanupSharedNamespace deletes the shared MinIO namespace if it exists.
// It should be called from a defer at the end of the suite, never from a test
func CleanupSharedNamespace(ctx context.Context, crudClient client.Client) error {
	var ns corev1.Namespace
	switch err := objects.Get(ctx, crudClient, client.ObjectKey{Name: minioNamespace}, &ns); {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}
	return namespaces.DeleteNamespaceAndWait(ctx, crudClient, minioNamespace, 600)
}

// RequestInstance ensures a shared MinIO deployment exists and returns a MinIO API client for it.
func RequestInstance(env *environment.TestingEnvironment, namespace string) (*Instance, error) {
	inst := &Instance{
		Namespace:        minioNamespace,
		ServiceName:      "minio-service." + minioNamespace,
		CaSecretName:     "minio-server-ca-secret",
		TLSSecret:        "minio-server-tls-secret",
		ctx:              env.Ctx,
		CrudClient:       env.Client,
		Interface:        env.Interface,
		RestClientConfig: env.RestClientConfig,
	}

	// The minio namespace is shared across every spec that requests MinIO and
	// kept alive for the whole suite — see CleanupSharedNamespace for the
	// matching teardown. If a previous suite run was interrupted and left the
	// namespace Terminating, wait for it to fully disappear before recreating.
	if err := retry.New(retry.Attempts(120), retry.Delay(time.Second)).Do(func() error {
		var ns corev1.Namespace
		err := objects.Get(env.Ctx, env.Client, client.ObjectKey{Name: inst.Namespace}, &ns)
		switch {
		case apierrors.IsNotFound(err):
			return namespaces.CreateNamespace(env.Ctx, env.Client, inst.Namespace)
		case err != nil:
			return err
		case ns.Status.Phase == corev1.NamespaceTerminating:
			return fmt.Errorf("namespace %q is terminating", inst.Namespace)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var minioDeployment appsv1.Deployment
	err := objects.Get(
		env.Ctx,
		env.Client,
		client.ObjectKey{Namespace: inst.Namespace, Name: minioDeploymentName},
		&minioDeployment,
	)
	if apierrors.IsNotFound(err) {
		if err := deployServer(inst, env); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	if err := waitForDeploymentReady(env, inst.Namespace, minioDeploymentName, 0); err != nil {
		return nil, err
	}

	podList := &corev1.PodList{}
	if err := objects.List(
		env.Ctx,
		env.Client,
		podList,
		client.InNamespace(inst.Namespace),
		client.MatchingLabels{"app": "minio"},
	); err != nil {
		return nil, err
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no MinIO pods found in namespace %s", inst.Namespace)
	}

	dialer, err := forwardconnection.NewDialer(
		env.Interface,
		env.RestClientConfig,
		inst.Namespace,
		podList.Items[0].Name,
	)
	if err != nil {
		return nil, err
	}

	inst.Forwarder, err = forwardconnection.NewForwardConnection(
		dialer,
		[]string{"0:9000"},
		io.Discard,
		io.Discard,
	)
	if err != nil {
		return nil, err
	}

	if err := inst.Forwarder.StartAndWait(env.Ctx); err != nil {
		return nil, err
	}
	// Release the port-forward goroutine when the requesting spec finishes;
	// otherwise every test that calls RequestInstance leaks one.
	ginkgo.DeferCleanup(inst.Forwarder.Close)

	port, err := inst.Forwarder.GetLocalPort()
	if err != nil {
		return nil, err
	}

	inst.MinioClient, err = minio.New(fmt.Sprintf("localhost:%s", port), &minio.Options{
		Creds:  credentials.NewStaticV4(minioAccessKey, minioSecretKey, ""),
		Secure: true,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, //nolint:gosec
			},
		},
	})
	if err != nil {
		return nil, err
	}

	if err := inst.createCaSecret(env, namespace); err != nil {
		return nil, err
	}

	if _, err := secrets.CreateObjectStorageSecret(
		env.Ctx,
		env.Client,
		namespace,
		ObjectStorageCredentialsSecretName,
		minioAccessKey,
		minioSecretKey,
	); err != nil {
		return nil, err
	}

	return inst, nil
}

func deployServer(inst *Instance, env *environment.TestingEnvironment) error {
	caPair, err := certs.CreateRootCA(inst.Namespace, "minio")
	if err != nil {
		return err
	}

	caSecret := caPair.GenerateCASecret(inst.Namespace, inst.CaSecretName)
	if _, err = objects.Create(env.Ctx, env.Client, caSecret); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	// sign and create secret using CA certificate and key
	serverPair, err := caPair.CreateAndSignPair("minio-service", certs.CertTypeServer,
		[]string{"minio.useless.domain.not.verified", "minio-service.minio"},
	)
	if err != nil {
		return err
	}

	serverSecret := serverPair.GenerateCertificateSecret(inst.Namespace, inst.TLSSecret)
	if err = env.Client.Create(env.Ctx, serverSecret); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}

	setup, err := sslSetup(inst.Namespace)
	if err != nil {
		return err
	}
	return installMinio(env, setup, 0)
}

func (m *Instance) getCaSecret(env *environment.TestingEnvironment, namespace string) (*corev1.Secret, error) {
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

// createCaSecret creates the certificates required to authenticate against the MinIO service
func (m *Instance) createCaSecret(env *environment.TestingEnvironment, namespace string) error {
	caSecret, err := m.getCaSecret(env, namespace)
	if err != nil {
		return err
	}
	_, err = objects.Create(env.Ctx, env.Client, caSecret)
	return err
}

// CountFiles counts objects matching the given MinIO path glob using the MinIO SDK.
func (m *Instance) CountFiles(path string) (int, error) {
	objects, err := m.listMatchingObjects(path)
	if err != nil {
		return -1, err
	}
	return len(objects), nil
}

// ListFiles lists object paths matching the given MinIO path glob using the MinIO SDK.
func (m *Instance) ListFiles(path string) (string, error) {
	objects, err := m.listMatchingObjects(path)
	if err != nil {
		return "", err
	}
	paths := make([]string, 0, len(objects))
	for _, object := range objects {
		paths = append(paths, minioObjectPath(object.bucket, object.key))
	}
	return strings.Join(paths, "\n"), nil
}

// GetFileTags retrieves object tags for the first object matching a specified path using the MinIO SDK.
func (m *Instance) GetFileTags(path string) (TagSet, error) {
	var output TagSet
	objects, err := m.listMatchingObjects(path)
	if err != nil {
		return output, err
	}
	if len(objects) == 0 {
		return output, fmt.Errorf("no MinIO object found matching %q", path)
	}

	tags, err := m.MinioClient.GetObjectTagging(
		m.ctx,
		objects[0].bucket,
		objects[0].key,
		minio.GetObjectTaggingOptions{},
	)
	if err != nil {
		return output, err
	}
	output.Tags = tags.ToMap()
	return output, nil
}

// CleanFiles deletes objects under the given MinIO path using the MinIO SDK.
func (m *Instance) CleanFiles(path string) (string, error) {
	bucket, prefix, err := splitMinioPath(path)
	if err != nil {
		return "", err
	}

	exists, err := m.MinioClient.BucketExists(m.ctx, bucket)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", nil
	}

	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}

	var deletedPaths []string
	objects := m.MinioClient.ListObjects(m.ctx, bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})
	for object := range objects {
		if object.Err != nil {
			return "", object.Err
		}
		if err := m.MinioClient.RemoveObject(
			m.ctx,
			bucket,
			object.Key,
			minio.RemoveObjectOptions{},
		); err != nil {
			return "", err
		}
		deletedPaths = append(deletedPaths, minioObjectPath(bucket, object.Key))
	}

	return strings.Join(deletedPaths, "\n"), nil
}

// ForgeArchiveWal copies an existing archived WAL object to a new WAL object name using the MinIO SDK.
func (m *Instance) ForgeArchiveWal(clusterName, existingWALName, newWALName string) error {
	const walSegmentPath = "wals/0000000100000000"

	sourceObject := filepath.Join(clusterName, walSegmentPath, existingWALName+".gz")
	destinationObject := filepath.Join(clusterName, walSegmentPath, newWALName)
	_, err := m.MinioClient.CopyObject(
		m.ctx,
		minio.CopyDestOptions{
			Bucket: clusterName,
			Object: destinationObject,
		},
		minio.CopySrcOptions{
			Bucket: clusterName,
			Object: sourceObject,
		},
	)
	return err
}

// matchedObject is the result row returned by listMatchingObjects.
type matchedObject struct {
	bucket string
	key    string
}

// listingHint summarises the literal portion of a glob path so that we can
// scope the MinIO listing instead of scanning every bucket end-to-end.
type listingHint struct {
	// bucket is non-empty when the leading path segment is a literal — we can
	// then list only that single bucket and skip the BucketList call.
	bucket string
	// keyPrefix is the longest contiguous literal prefix of the key portion
	// (everything after the bucket segment up to the first glob). It is fed
	// to ListObjectsOptions.Prefix as a server-side filter; the matcher still
	// runs to enforce the full pattern.
	keyPrefix string
}

// extractLiteralPathHint walks the glob path and pulls out as much literal
// prefix as is safe to feed to MinIO as a listing scope. It tolerates an
// optional leading "minio/" alias.
func extractLiteralPathHint(path string) listingHint {
	segments := strings.Split(strings.TrimPrefix(path, "minio/"), "/")

	isGlob := func(s string) bool { return strings.ContainsAny(s, "*?") }

	var hint listingHint
	if len(segments) == 0 || segments[0] == "" {
		return hint
	}
	if !isGlob(segments[0]) {
		hint.bucket = segments[0]
	}

	var keyParts []string
	for _, s := range segments[1:] {
		if isGlob(s) {
			break
		}
		keyParts = append(keyParts, s)
	}
	hint.keyPrefix = strings.Join(keyParts, "/")
	return hint
}

func (m *Instance) listMatchingObjects(path string) ([]matchedObject, error) {
	matches, err := newMinioPathMatcher(path)
	if err != nil {
		return nil, err
	}

	hint := extractLiteralPathHint(path)

	var bucketNames []string
	if hint.bucket != "" {
		exists, err := m.MinioClient.BucketExists(m.ctx, hint.bucket)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, nil
		}
		bucketNames = []string{hint.bucket}
	} else {
		buckets, err := m.MinioClient.ListBuckets(m.ctx)
		if err != nil {
			return nil, err
		}
		bucketNames = make([]string, len(buckets))
		for i, b := range buckets {
			bucketNames[i] = b.Name
		}
	}

	var objects []matchedObject
	for _, bucket := range bucketNames {
		objectCh := m.MinioClient.ListObjects(m.ctx, bucket, minio.ListObjectsOptions{
			Prefix:    hint.keyPrefix,
			Recursive: true,
		})
		for object := range objectCh {
			if object.Err != nil {
				return nil, object.Err
			}
			if matches(bucket, object.Key) {
				objects = append(objects, matchedObject{bucket: bucket, key: object.Key})
			}
		}
	}

	sort.Slice(objects, func(i, j int) bool {
		return minioObjectPath(objects[i].bucket, objects[i].key) <
			minioObjectPath(objects[j].bucket, objects[j].key)
	})
	return objects, nil
}

func newMinioPathMatcher(path string) (func(bucket, key string) bool, error) {
	matcher, err := regexp.Compile(globToRegexp(path))
	if err != nil {
		return nil, err
	}

	return func(bucket, key string) bool {
		return matcher.MatchString(minioObjectPath(bucket, key)) ||
			matcher.MatchString(minioObjectPathWithoutAlias(bucket, key)) ||
			matcher.MatchString(key)
	}, nil
}

func minioObjectPath(bucket, key string) string {
	return "minio/" + minioObjectPathWithoutAlias(bucket, key)
}

func minioObjectPathWithoutAlias(bucket, key string) string {
	return bucket + "/" + key
}

func globToRegexp(pattern string) string {
	var builder strings.Builder
	builder.WriteString("^")
	for _, char := range pattern {
		switch char {
		case '*':
			builder.WriteString(".*")
		case '?':
			builder.WriteByte('.')
		default:
			builder.WriteString(regexp.QuoteMeta(string(char)))
		}
	}
	builder.WriteString("$")
	return builder.String()
}

// splitMinioPath parses a "minio/<bucket>/<prefix>" path into its bucket and
// key-prefix parts. CleanFiles needs a concrete bucket name to drive the
// MinIO API, so glob patterns on the bucket position are rejected explicitly
// — leaving them through would silently no-op against a non-existent bucket
// named "*".
func splitMinioPath(minioPath string) (string, string, error) {
	path := strings.Trim(strings.TrimPrefix(minioPath, "minio/"), "/")
	if path == "" {
		return "", "", fmt.Errorf("empty MinIO path")
	}

	bucket, prefix, _ := strings.Cut(path, "/")
	if strings.ContainsAny(bucket, "*?") {
		return "", "", fmt.Errorf("MinIO bucket name cannot contain glob characters: %q", minioPath)
	}
	return bucket, prefix, nil
}

// TestBarmanConnectivity validates the barman connectivity to the minio endpoint
func TestBarmanConnectivity(
	namespace,
	clusterName,
	primaryPodName,
	minioID,
	minioKey string,
	minioSvcName string,
) (bool, error) {
	env := fmt.Sprintf("export AWS_CA_BUNDLE=%s;export AWS_ACCESS_KEY_ID=%s;export AWS_SECRET_ACCESS_KEY=%s;",
		postgres.BarmanBackupEndpointCACertificateLocation, minioID, minioKey)

	endpointURL := fmt.Sprintf("https://%s:9000", minioSvcName)
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

// GetFilePath gets the MinIO file string for WAL/backup objects in a configured bucket
func GetFilePath(serverName, fileName string) string {
	// the * regexes enable matching these typical paths:
	// 	minio/backups/serverName/base/20220618T140300/data.tar
	// 	minio/backups/serverName/wals/0000000100000000/000000010000000000000002.gz
	//  minio/backups/serverName/wals/00000002.history.gz
	return filepath.Join("*", serverName, "*", fileName)
}

// AssertArchiveWalOnMinio archives a WAL on the primary and verifies it lands
// in this MinIO bucket within the given timeout (seconds).
func (m *Instance) AssertArchiveWalOnMinio(namespace, clusterName, serverName string, timeout int) {
	var latestWALPath string
	// Create a WAL on the primary and check if it arrives at minio, within a short time
	ginkgo.By("archiving WALs and verifying they exist", func() {
		pod, err := clusterutils.GetPrimary(m.ctx, m.CrudClient, namespace, clusterName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		primary := pod.GetName()

		latestWAL := SwitchWalAndGetLatestArchive(
			m.ctx, m.CrudClient, m.Interface, m.RestClientConfig, namespace, primary,
		)
		latestWALPath = GetFilePath(serverName, latestWAL+".gz")
	})

	ginkgo.By(fmt.Sprintf("verify the existence of WAL %v in minio", latestWALPath), func() {
		gomega.Eventually(func() (int, error) {
			// WALs are compressed with gzip in the fixture
			return m.CountFiles(latestWALPath)
		}, timeout).Should(gomega.BeEquivalentTo(1))
	})
}

// SwitchWalAndGetLatestArchive trigger a new wal and get the name of latest wal file
func SwitchWalAndGetLatestArchive(
	ctx context.Context,
	crudClient client.Client,
	kubeInterface kubernetes.Interface,
	restConfig *rest.Config,
	namespace, podName string,
) string {
	_, _, err := exec.QueryInInstancePodWithTimeout(
		ctx, crudClient, kubeInterface, restConfig,
		exec.PodLocator{
			Namespace: namespace,
			PodName:   podName,
		},
		postgres2.PostgresDBName,
		"CHECKPOINT",
		300*time.Second,
	)
	gomega.Expect(err).ToNot(gomega.HaveOccurred(),
		"failed to trigger a new wal while executing 'switchWalAndGetLatestArchive'")

	out, _, err := exec.QueryInInstancePod(
		ctx, crudClient, kubeInterface, restConfig,
		exec.PodLocator{
			Namespace: namespace,
			PodName:   podName,
		},
		postgres2.PostgresDBName,
		"SELECT pg_catalog.pg_walfile_name(pg_switch_wal())",
	)
	gomega.Expect(err).ToNot(
		gomega.HaveOccurred(),
		"failed to get latest wal file name while executing 'switchWalAndGetLatestArchive")

	return strings.TrimSpace(out)
}
