/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package app

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"

	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/fileutils"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/management/log"
	"gitlab.2ndquadrant.com/k8s/cloud-native-postgresql/pkg/management/postgres"
)

var (
	instance postgres.Instance
)

// InstanceManagerCommand is the command handling the management of a
// certain instance and it's meant to be executed inside the PostgreSQL
// image
func InstanceManagerCommand(args []string) {
	var pwFile string
	var appDBName string
	var appUser string
	var appPwFile string
	var pgData string
	var parentNode string
	var podName string
	var clusterName string
	var backupName string
	var namespace string

	initCommand := flag.NewFlagSet("init", flag.ExitOnError)
	initCommand.StringVar(&pwFile, "pw-file", "",
		"The file containing the PostgreSQL superuser password to use during the init phase")
	initCommand.StringVar(&appDBName, "app-db-name", "app",
		"The name of the application containing the database")
	initCommand.StringVar(&appUser, "app-user", "app",
		"The name of the application user")
	initCommand.StringVar(&appPwFile, "app-pw-file", "",
		"The file where the password for the application user is stored")
	initCommand.StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	initCommand.StringVar(&parentNode, "parent-node", "", "The origin node")
	initCommand.StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	initCommand.StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")

	joinCommand := flag.NewFlagSet("join", flag.ExitOnError)
	joinCommand.StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	joinCommand.StringVar(&parentNode, "parent-node", "", "The origin node")
	joinCommand.StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster state")
	joinCommand.StringVar(&pwFile, "pw-file", "",
		"The file containing the PostgreSQL superuser password to use to connect to PostgreSQL")

	runCommand := flag.NewFlagSet("run", flag.ExitOnError)
	runCommand.StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be started up")
	runCommand.StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster state")
	runCommand.StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	runCommand.StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")
	runCommand.StringVar(&pwFile, "pw-file", "",
		"The file containing the PostgreSQL superuser password to use to connect to PostgreSQL")

	restoreCommand := flag.NewFlagSet("restore", flag.ExitOnError)
	restoreCommand.StringVar(&pwFile, "pw-file", "",
		"The file containing the PostgreSQL superuser password to use during the init phase")
	restoreCommand.StringVar(&parentNode, "parent-node", "", "The origin node")
	restoreCommand.StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	restoreCommand.StringVar(&backupName, "backup-name", "", "The name of the backup that should be restored")
	restoreCommand.StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	restoreCommand.StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and the Pod in k8s")

	statusCommand := flag.NewFlagSet("status", flag.ExitOnError)

	if len(args) == 0 {
		fmt.Println("usage: manager instance <command> <args>")
		fmt.Println("Available commands:")
		fmt.Println("  init      Bootstrap the first instance of a PostgreSQL cluster")
		fmt.Println("  join      Bootstrap a new node by joining an existing node")
		fmt.Println("  run       Run the PostgreSQL instance")
		fmt.Println("  status    Print the instance status")
		fmt.Println("  restore   Create a new PGData given a backup")
		return
	}

	switch args[0] {
	case "init":
		// Ignore errors; initCommand is set for ExitOnError.
		_ = initCommand.Parse(args[1:])
		info := postgres.InitInfo{
			PgData:                  pgData,
			PasswordFile:            pwFile,
			ApplicationDatabase:     appDBName,
			ApplicationUser:         appUser,
			ApplicationPasswordFile: appPwFile,
			ParentNode:              parentNode,
			ClusterName:             clusterName,
			Namespace:               namespace,
		}

		initSubCommand(info)
	case "join":
		// Ignore errors; joinCommand is set for ExitOnError.
		_ = joinCommand.Parse(args[1:])
		info := postgres.JoinInfo{
			PgData:     pgData,
			ParentNode: parentNode,
			PodName:    podName,
		}

		// Here we need to create a pgpass file
		// given that we'll use it to clone a pgdata from an
		// existing server
		if err := postgres.CreatePgPass(pwFile); err != nil {
			log.Log.Error(err, "Error creating pgpass file")
			os.Exit(1)
		}
		joinSubCommand(info)
	case "run":
		// Ignore errors; runCommand is set for ExitOnError.
		_ = runCommand.Parse(args[1:])
		instance.PgData = pgData
		instance.ApplicationDatabase = appDBName
		instance.Namespace = namespace
		instance.PodName = podName
		instance.ClusterName = clusterName

		// Here we need to create a pgpass file because
		// we'll use it ot handle replication between different PG Pods
		if err := postgres.CreatePgPass(pwFile); err != nil {
			log.Log.Error(err, "Error creating pgpass file")
			os.Exit(1)
		}
		runSubCommand()
	case "status":
		// Ignore errors; statusCommand is set for ExitOnError
		_ = statusCommand.Parse(args[1:])
		statusSubCommand()
	case "restore":
		// Ignore errors; restoreCommand is set for ExitOnError.
		_ = restoreCommand.Parse(args[1:])
		info := postgres.InitInfo{
			PgData:       pgData,
			PasswordFile: pwFile,
			ClusterName:  clusterName,
			Namespace:    namespace,
			BackupName:   backupName,
			ParentNode:   parentNode,
		}

		restoreSubCommand(info)
	default:
		fmt.Printf("%v is not a valid command\n", args[0])
		os.Exit(1)
	}
}

func initSubCommand(info postgres.InitInfo) {
	status, err := fileutils.FileExists(info.PgData)
	if err != nil {
		log.Log.Error(err, "Error while checking for an existent PGData")
		os.Exit(1)
	}
	if status {
		log.Log.Info("PGData already exists, no need to init")
		return
	}

	err = info.VerifyConfiguration()
	if err != nil {
		log.Log.Error(err, "Configuration not valid",
			"info", info)
		os.Exit(1)
	}

	err = info.Bootstrap()
	if err != nil {
		log.Log.Error(err, "Error while bootstrapping data directory")
		os.Exit(1)
	}
}

func restoreSubCommand(info postgres.InitInfo) {
	status, err := fileutils.FileExists(info.PgData)
	if err != nil {
		log.Log.Error(err, "Error while checking for an existent PGData")
		os.Exit(1)
	}
	if status {
		log.Log.Info("PGData already exists, can't restore over an existing folder")
		return
	}

	err = info.VerifyConfiguration()
	if err != nil {
		log.Log.Error(err, "Configuration not valid",
			"info", info)
		os.Exit(1)
	}

	err = info.Restore()
	if err != nil {
		log.Log.Error(err, "Error while restoring a backup")
		os.Exit(1)
	}
}

func joinSubCommand(info postgres.JoinInfo) {
	status, err := fileutils.FileExists(info.PgData)
	if err != nil {
		log.Log.Error(err, "Error while checking for an existent PGData")
		os.Exit(1)
	}
	if status {
		log.Log.Info("PGData already exists, no need to init")
		return
	}

	err = info.Join()
	if err != nil {
		log.Log.Error(err, "Error joining node")
		os.Exit(1)
	}
}

func statusSubCommand() {
	resp, err := http.Get("http://localhost:8000/pg/status")
	if err != nil {
		log.Log.Error(err, "Error while requesting instance status")
		os.Exit(1)
	}

	if resp.StatusCode != 200 {
		bytes, _ := ioutil.ReadAll(resp.Body)
		log.Log.Info(
			"Error while extracting status",
			"statusCode", resp.StatusCode, "body", string(bytes))
		_ = resp.Body.Close()
		os.Exit(1)
	}

	_, err = io.Copy(os.Stdout, resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		log.Log.Error(err, "Error while showing status info")
		os.Exit(1)
	}
}
