/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/
package main

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1alpha1 "github.com/2ndquadrant/cloud-native-postgresql/api/v1alpha1"

	"github.com/2ndquadrant/cloud-native-postgresql/pkg/log"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/postgres/webserver"
)

var (
	postgresCommand *exec.Cmd
	k8s             client.Client
)

func runSubCommand() {
	var err error

	ctx := context.Background()
	err = verifyClusterStatus(ctx)
	if err != nil {
		log.Log.Error(err, "Error while checking Kubernetes cluster status")
		return
	}

	startWebServer()
	registerSignalHandler()

	postgresCommand, err = instance.Run()
	if err != nil {
		log.Log.Error(err, "Unable to start PostgreSQL up")
	}

	if err = postgresCommand.Wait(); err != nil {
		log.Log.Error(err, "PostgreSQL exited with errors")
	}
}

// clusterObjectKey construct the object key leading to the cluster object
// in k8s
func clusterObjectKey() client.ObjectKey {
	return client.ObjectKey{
		Name:      instance.ClusterName,
		Namespace: instance.Namespace,
	}
}

// verifyClusterStatus check if this cluster exist in k8s and panic if this
// pod belongs to a master but the cluster status is not coherent with that
func verifyClusterStatus(ctx context.Context) error {
	var err error
	var cluster apiv1alpha1.Cluster

	k8s, err = createKubernetesClient()
	if err != nil {
		return err
	}

	err = k8s.Get(ctx, clusterObjectKey(), &cluster)
	if err != nil {
		return err
	}

	isPrimary, err := instance.IsPrimary()
	if err != nil {
		return err
	}

	if isPrimary {
		isCurrentPrimary := instance.PodName == cluster.Status.CurrentPrimary
		isTargetPrimary := instance.PodName == cluster.Status.TargetPrimary

		if !isCurrentPrimary && !isTargetPrimary {
			log.Log.Info("Safety measure failed. This PGDATA belongs to "+
				"a primary instance, but this instance is neither primary "+
				"nor target primary",
				"currentPrimary", cluster.Status.CurrentPrimary,
				"targetPrimary", cluster.Status.TargetPrimary,
				"podName", instance.PodName)
			return errors.Errorf("This PGDATA belongs to a primary but " +
				"this instance is neither the current primary nor the target primary. " +
				"Aborting")
		}

		if cluster.Status.CurrentPrimary == "" {
			// We have a current primary! That's me!
			cluster.Status.CurrentPrimary = instance.PodName
			if err := k8s.Status().Update(ctx, &cluster); err != nil {
				return err
			}
		}
	}

	return nil
}

// startWebServer start the web server for handling probes given
// a certain PostgreSQL instance
func startWebServer() {
	go func() {
		err := webserver.ListenAndServe(&instance)
		if err != nil {
			log.Log.Error(err, "Error while starting the web server")
		}
	}()
}

// registerSignalHandler handles signals from k8s, notifying postgres as
// needed
func registerSignalHandler() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-signals
		log.Log.Info("Received termination signal", "signal", sig)

		log.Log.Info("Shutting down web server")
		err := webserver.Shutdown()
		if err != nil {
			log.Log.Error(err, "Error while shutting down the web server")
		} else {
			log.Log.Info("Web server shut down")
		}

		if postgresCommand != nil {
			log.Log.Info("Shutting down PostgreSQL instance")
			err := postgresCommand.Process.Signal(syscall.SIGTERM)
			if err != nil {
				log.Log.Error(err, "Unable to send SIGTERM to PostgreSQL instance")
			}
		}
	}()
}
