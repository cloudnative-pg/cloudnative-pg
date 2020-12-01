/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2020 2ndQuadrant Italia SRL. Exclusively licensed to 2ndQuadrant Limited.
*/

// Package log contains the logging subsystem of PGK
package log

import (
	"os"
	"strconv"

	"github.com/go-logr/logr"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	crzap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// Log is the logger that will be used in this package
var Log logr.Logger

// V4 prints Log.V(4) logs
var V4 = zapcore.Level(-4)

func init() {
	level := zap.NewAtomicLevelAt(zap.DebugLevel)

	// To enable the debugging logging level you
	// have to just set the "DEBUG" environment variable
	// to "1" or something ParseBool-compatible
	debugActive, err := strconv.ParseBool(os.Getenv("DEBUG"))
	if debugActive && err != nil {
		level = zap.NewAtomicLevelAt(V4)
	}

	Log = crzap.New(crzap.UseDevMode(true), crzap.Level(&level))
}
