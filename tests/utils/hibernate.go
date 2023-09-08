package utils

import (
	"context"
	"fmt"

	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/reconciler/hibernation"
	"github.com/cloudnative-pg/cloudnative-pg/pkg/utils"
)

// HibernationMethod will be one of the supported ways to trigger an instance fencing
type HibernationMethod string

const (
	// HibernateDeclaratively it is a keyword to use while fencing on/off the instances using annotation method
	HibernateDeclaratively HibernationMethod = "annotation"
	// HibernateImperatively it is a keyword to use while fencing on/off the instances using plugin method
	HibernateImperatively HibernationMethod = "plugin"
)

// HibernateOn hibernate on a cluster
func HibernateOn(
	env *TestingEnvironment,
	namespace,
	clusterName string,
	method HibernationMethod,
) error {
	switch method {
	case HibernateImperatively:
		_, _, err := Run(fmt.Sprintf("kubectl cnpg hibernate on %v -n %v",
			clusterName, namespace))
		if err != nil {
			return err
		}
		return nil
	case HibernateDeclaratively:
		cluster, err := env.GetCluster(namespace, clusterName)
		if err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		originCluster := cluster.DeepCopy()
		cluster.Annotations[utils.HibernationAnnotationName] = hibernation.HibernationOn

		err = env.Client.Patch(context.Background(), cluster, ctrlclient.MergeFrom(originCluster))
		return err
	default:
		return fmt.Errorf("unknown method: %v", method)
	}
}

// HibernateOff hibernate off a cluster
func HibernateOff(
	env *TestingEnvironment,
	namespace,
	clusterName string,
	method HibernationMethod,
) error {
	switch method {
	case HibernateImperatively:
		_, _, err := Run(fmt.Sprintf("kubectl cnpg hibernate off %v -n %v",
			clusterName, namespace))
		if err != nil {
			return err
		}
		return nil
	case HibernateDeclaratively:
		cluster, err := env.GetCluster(namespace, clusterName)
		if err != nil {
			return err
		}
		if cluster.Annotations == nil {
			cluster.Annotations = make(map[string]string)
		}
		originCluster := cluster.DeepCopy()
		cluster.Annotations[utils.HibernationAnnotationName] = hibernation.HibernationOff

		err = env.Client.Patch(context.Background(), cluster, ctrlclient.MergeFrom(originCluster))
		return err
	default:
		return fmt.Errorf("unknown method: %v", method)
	}
}
