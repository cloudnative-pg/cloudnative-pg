/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/2ndquadrant/cloud-native-postgresql/pkg/fileutils"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/log"
	"github.com/2ndquadrant/cloud-native-postgresql/pkg/management/postgres"
)

var (
	instance postgres.Instance
)

func main() {
	var pwFile string
	var appDbName string
	var appUser string
	var appPwFile string
	var postgresHBARules string
	var postgresConfig string
	var pgData string
	var parentNode string
	var podName string
	var clusterName string
	var namespace string

	initCommand := flag.NewFlagSet("init", flag.ExitOnError)
	initCommand.StringVar(&pwFile, "pw-file", "/etc/secret/postgresPassword",
		"The file containing the PostgreSQL superuser password to use during the init phase")
	initCommand.StringVar(&appDbName, "app-db-name", "app",
		"The name of the application containing the database")
	initCommand.StringVar(&appUser, "app-user", "app",
		"The name of the application user")
	initCommand.StringVar(&appPwFile, "app-pw-file", "/etc/secret/ownerPassword",
		"The file where the password for the application user is stored")
	initCommand.StringVar(&postgresHBARules, "hba-rules-file", "",
		"The file containing the HBA rules to apply to PostgreSQL")
	initCommand.StringVar(&postgresConfig, "postgresql-config-file", "",
		"The file containing the PostgreSQL configuration to add")
	initCommand.StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")

	joinCommand := flag.NewFlagSet("join", flag.ExitOnError)
	joinCommand.StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	joinCommand.StringVar(&parentNode, "parent-node", "", "The origin node")
	joinCommand.StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster stater")

	runCommand := flag.NewFlagSet("run", flag.ExitOnError)
	runCommand.StringVar(&pgData, "pg-data", os.Getenv("PGDATA"), "The PGDATA to be created")
	runCommand.StringVar(&appDbName, "app-db-name", "app",
		"The name of the application containing the database")
	runCommand.StringVar(&podName, "pod-name", os.Getenv("POD_NAME"), "The name of this pod, to "+
		"be checked against the cluster stater")
	runCommand.StringVar(&clusterName, "cluster-name", os.Getenv("CLUSTER_NAME"), "The name of the "+
		"current cluster in k8s, used to coordinate switchover and failover")
	runCommand.StringVar(&namespace, "namespace", os.Getenv("NAMESPACE"), "The namespace of "+
		"the cluster and of the Pod in k8s")

	statusCommand := flag.NewFlagSet("status", flag.ExitOnError)

	if len(os.Args) == 1 {
		fmt.Println("usage: pgk <command> <args>")
		fmt.Println("Available commands:")
		fmt.Println("  init    Bootstrap the first instance of a PostgreSQL cluster")
		fmt.Println("  join    Bootstrap a new node by joining an existing node")
		fmt.Println("  run     Run the PostgreSQL instance")
		fmt.Println("  status  Print the instance status")
		return
	}

	switch os.Args[1] {
	case "init":
		// Ignore errors; initCommand is set for ExitOnError.
		_ = initCommand.Parse(os.Args[2:])
		info := postgres.InitInfo{
			PgData:                  pgData,
			PasswordFile:            pwFile,
			ApplicationDatabase:     appDbName,
			ApplicationUser:         appUser,
			ApplicationPasswordFile: appPwFile,
			HBARulesFile:            postgresHBARules,
			PostgreSQLConfigFile:    postgresConfig,
		}
		initSubCommand(info)
	case "join":
		// Ignore errors; joinCommand is set for ExitOnError.
		_ = joinCommand.Parse(os.Args[2:])
		info := postgres.JoinInfo{
			PgData:     pgData,
			ParentNode: parentNode,
			PodName:    podName,
		}
		joinSubCommand(info)
	case "run":
		// Ignore errors; runCommand is set for ExitOnError.
		_ = runCommand.Parse(os.Args[2:])
		instance.PgData = pgData
		instance.ApplicationDatabase = appDbName
		instance.Port = 5432
		instance.Namespace = namespace
		instance.PodName = podName
		instance.ClusterName = clusterName
		runSubCommand()
	case "status":
		// Ignore errors; statusCommand is set for ExitOnError
		_ = statusCommand.Parse(os.Args[2:])
		statusSubCommand()
	default:
		fmt.Printf("%v is not a valid command\n", os.Args[1])
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
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Log.Info("Error while extracting status", "statusCode", resp.StatusCode, "body", resp.Body)
		os.Exit(1)
	}

	_, err = io.Copy(os.Stdout, resp.Body)
	if err != nil {
		log.Log.Error(err, "Error while showing status info")
		os.Exit(1)
	}
}
