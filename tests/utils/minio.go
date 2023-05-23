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
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/postgres"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

const (
	minioImage       = "minio/minio:RELEASE.2022-06-20T23-13-45Z"
	minioClientImage = "minio/mc:RELEASE.2022-06-11T21-10-36Z"
)

// MinioSetup contains the resources needed for a working minio server deployment:
// a PersistentVolumeClaim, a Deployment and a Service
type MinioSetup struct {
	PersistentVolumeClaim corev1.PersistentVolumeClaim
	Deployment            appsv1.Deployment
	Service               corev1.Service
}

// TagSet will contain the `tagset` section of the minio output command
type TagSet struct {
	Tags map[string]string `json:"tagset"`
}

// InstallMinio installs minio in a given namespace
func InstallMinio(
	env *TestingEnvironment,
	minioSetup MinioSetup,
	timeoutSeconds uint,
) error {
	if err := env.Client.Create(env.Ctx, &minioSetup.PersistentVolumeClaim); err != nil {
		return err
	}
	if err := env.Client.Create(env.Ctx, &minioSetup.Deployment); err != nil {
		return err
	}
	err := retry.Do(
		func() error {
			deployment := &appsv1.Deployment{}
			if err := env.Client.Get(
				env.Ctx,
				client.ObjectKey{Namespace: minioSetup.Deployment.Namespace, Name: minioSetup.Deployment.Name},
				deployment,
			); err != nil {
				return err
			}
			if deployment.Status.ReadyReplicas != *minioSetup.Deployment.Spec.Replicas {
				return fmt.Errorf("not all replicas are ready. Expected %v, found %v",
					*minioSetup.Deployment.Spec.Replicas,
					deployment.Status.ReadyReplicas,
				)
			}
			return nil
		},
		retry.Attempts(timeoutSeconds),
		retry.Delay(time.Second),
		retry.DelayType(retry.FixedDelay),
	)
	if err != nil {
		return err
	}
	err = env.Client.Create(env.Ctx, &minioSetup.Service)
	return err
}

// MinioDefaultSetup returns the definition for the default minio setup
func MinioDefaultSetup(namespace string) (MinioSetup, error) {
	pvc, err := MinioDefaultPVC(namespace)
	if err != nil {
		return MinioSetup{}, err
	}
	deployment := MinioDefaultDeployment(namespace, pvc)
	service := MinioDefaultSVC(namespace)
	setup := MinioSetup{
		PersistentVolumeClaim: pvc,
		Deployment:            deployment,
		Service:               service,
	}
	return setup, nil
}

// MinioDefaultDeployment returns a default Deployment for minio
func MinioDefaultDeployment(namespace string, minioPVC corev1.PersistentVolumeClaim) appsv1.Deployment {
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}
	if !utils.HaveSeccompSupport() {
		seccompProfile = nil
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
								AllowPrivilegeEscalation: pointer.Bool(false),
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

// MinioDefaultSVC returns a default Service for minio
func MinioDefaultSVC(namespace string) corev1.Service {
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

// MinioDefaultPVC returns a default PVC for minio
func MinioDefaultPVC(namespace string) (corev1.PersistentVolumeClaim, error) {
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
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					"storage": resource.MustParse("4Gi"),
				},
			},
			StorageClassName: &storageClass,
		},
	}
	return minioPVC, nil
}

// MinioSSLSetup returns the definition for a minio setup using SSL
func MinioSSLSetup(namespace string) (MinioSetup, error) {
	setup, err := MinioDefaultSetup(namespace)
	if err != nil {
		return MinioSetup{}, err
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

// MinioDefaultClient returns the default Pod definition for a minio client
func MinioDefaultClient(namespace string) corev1.Pod {
	seccompProfile := &corev1.SeccompProfile{
		Type: corev1.SeccompProfileTypeRuntimeDefault,
	}
	if !utils.HaveSeccompSupport() {
		seccompProfile = nil
	}

	minioClient := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      "mc",
			Labels:    map[string]string{"run": "mc"},
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "mc",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name:  "mc",
					Image: minioClientImage,
					Env: []corev1.EnvVar{
						{
							Name:  "MC_HOST_minio",
							Value: "http://minio:minio123@minio-service:9000",
						},
						{
							Name:  "MC_URL",
							Value: "https://minio-service:9000",
						},
						{
							Name:  "HOME",
							Value: "/mc",
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "mc",
							MountPath: "/mc",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: pointer.Bool(false),
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
	return minioClient
}

// MinioSSLClient returns the Pod definition for a minio client using SSL
func MinioSSLClient(namespace string) corev1.Pod {
	const (
		configVolumeMountPath = "/mc/.mc"
		configVolumeName      = "mc-config"
		minioServerCASecret   = "minio-server-ca-secret" // #nosec
		tlsVolumeName         = "secret-volume"
		tlsVolumeMountPath    = configVolumeMountPath + "/certs/CAs"
	)
	var secretMode int32 = 0o600

	minioClient := MinioDefaultClient(namespace)
	minioClient.Spec.Volumes = append(minioClient.Spec.Volumes,
		corev1.Volume{
			Name: configVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		corev1.Volume{
			Name: tlsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  minioServerCASecret,
					DefaultMode: &secretMode,
				},
			},
		},
	)
	minioClient.Spec.Containers[0].VolumeMounts = append(
		minioClient.Spec.Containers[0].VolumeMounts,
		corev1.VolumeMount{
			Name:      configVolumeName,
			MountPath: configVolumeMountPath,
		},
		corev1.VolumeMount{
			Name:      tlsVolumeName,
			MountPath: tlsVolumeMountPath,
		},
	)
	minioClient.Spec.Containers[0].Env[0].Value = "https://minio:minio123@minio-service:9000"

	return minioClient
}

// CountFilesOnMinio uses the minioClient in the given `namespace` to count  the
// amount of files matching the given `path`
func CountFilesOnMinio(namespace string, minioClientName string, path string) (value int, err error) {
	var stdout string
	stdout, _, err = RunUnchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -- %v",
		namespace,
		minioClientName,
		composeFindMinioCmd(path, "minio")))
	if err != nil {
		return -1, err
	}
	value, err = strconv.Atoi(strings.Trim(stdout, "\n"))
	return value, err
}

// composeFindMinioCmd builds the Minio find command
func composeFindMinioCmd(path string, serviceName string) string {
	return fmt.Sprintf("sh -c 'mc find %v --path %v | wc -l'", serviceName, path)
}

// GetFileTagsOnMinio will use the minioClient to retrieve the tags in a specified path
func GetFileTagsOnMinio(namespace, minioClientName, path string) (TagSet, error) {
	var output TagSet
	// Make sure we have a registered backup to access
	out, _, err := RunUncheckedRetry(fmt.Sprintf(
		"kubectl exec -n %v %v -- sh -c 'mc find minio --name %v | head -n1'",
		namespace,
		minioClientName,
		path))
	if err != nil {
		return output, err
	}

	walFile := strings.Trim(out, "\n")

	stdout, _, err := RunUncheckedRetry(fmt.Sprintf(
		"kubectl exec -n %v %v -- sh -c 'mc --json tag list %v'",
		namespace,
		minioClientName,
		walFile))
	if err != nil {
		return output, err
	}

	err = json.Unmarshal([]byte(stdout), &output)
	if err != nil {
		return output, err
	}
	return output, nil
}

// MinioTestConnectivityUsingBarmanCloudWalArchive returns true if test connection is successful else false
func MinioTestConnectivityUsingBarmanCloudWalArchive(namespace, clusterName, podName, id, key string) (bool, error) {
	minioSvc := MinioDefaultSVC(namespace)
	minioSvcName := minioSvc.GetName()
	// test connectivity should work with valid sample "000000010000000000000000" wal file
	// using barman-cloud-wal-archive script
	cmd := fmt.Sprintf("export AWS_CA_BUNDLE=%s;export AWS_ACCESS_KEY_ID=%s;export AWS_SECRET_ACCESS_KEY=%s;"+
		"barman-cloud-wal-archive --cloud-provider aws-s3 --endpoint-url https://%s:9000 s3://cluster-backups/ %s "+
		"000000010000000000000000 --test", postgres.BarmanBackupEndpointCACertificateLocation, id, key,
		minioSvcName, clusterName)
	_, _, err := RunUnchecked(fmt.Sprintf(
		"kubectl exec -n %v %v -c postgres -- /bin/bash -c \"%v\"",
		namespace,
		podName,
		cmd))
	if err != nil {
		return false, err
	}
	return true, nil
}
