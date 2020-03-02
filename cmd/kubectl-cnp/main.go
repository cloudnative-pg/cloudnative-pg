/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package main

import (
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"

	apiv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/log"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/utils"
)

var (
	configFlags *genericclioptions.ConfigFlags

	rootCmd = &cobra.Command{
		Use:               "kubectl cnp",
		Short:             "An interface to manage your Cloud Native PostgreSQL clusters",
		PersistentPreRunE: createKubernetesClient,
	}

	promoteCmd = &cobra.Command{
		Use:   "promote [cluster] [server]",
		Short: "Promote a certain server as a master",
		Args:  cobra.ExactArgs(2),
		Run:   promote,
	}

	statusCmd = &cobra.Command{
		Use:   "status [cluster]",
		Short: "Get the status of a PostgreSQL cluster",
		Args:  cobra.ExactArgs(1),
		Run:   status,
	}

	// Namespace to operate in
	namespace string

	// Kubernetes dynamic client
	kubeclient dynamic.Interface
)

func main() {
	configFlags = genericclioptions.NewConfigFlags(true)
	configFlags.AddFlags(rootCmd.PersistentFlags())
	rootCmd.AddCommand(promoteCmd, statusCmd)

	_ = rootCmd.Execute()
}

func createKubernetesClient(cmd *cobra.Command, args []string) error {
	kubeconfig := configFlags.ToRawKubeConfigLoader()

	config, err := kubeconfig.ClientConfig()
	if err != nil {
		return err
	}

	kubeclient, err = dynamic.NewForConfig(config)
	if err != nil {
		return err
	}

	namespace, _, err = kubeconfig.Namespace()
	if err != nil {
		return err
	}

	return nil
}

func promote(cmd *cobra.Command, args []string) {
	clusterName := args[0]
	serverName := args[1]

	// Check cluster status
	object, err := kubeclient.Resource(apiv1alpha1.ClusterGVK).
		Namespace(namespace).
		Get(clusterName, metav1.GetOptions{})
	if err != nil {
		log.Log.Error(err, "Cannot find PostgreSQL cluster",
			"namespace", namespace,
			"name", clusterName)
		return
	}

	// Check if the Pod exist
	_, err = kubeclient.Resource(schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "pods",
	}).Namespace(namespace).Get(serverName, metav1.GetOptions{})
	if err != nil {
		log.Log.Error(err, "Cannot find PostgreSQL server",
			"namespace", namespace,
			"name", serverName)
		return
	}

	// The Pod exists, let's do it!
	err = utils.SetTargetPrimary(object, serverName)
	if err != nil {
		log.Log.Error(err, "Cannot find status field of cluster",
			"object", object)
		return
	}

	// Update, considering possible conflicts
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err = kubeclient.
			Resource(apiv1alpha1.ClusterGVK).
			Namespace(namespace).
			UpdateStatus(object, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		log.Log.Error(err, "Cannot update PostgreSQL cluster status",
			"namespace", namespace,
			"name", serverName,
			"object", object)
		return
	}
}

func status(cmd *cobra.Command, args []string) {
	panic("TODO")
}
