/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package log contains the logging subsystem of PGK
package log

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// ErrorLevelString is the string representation of the error level
	ErrorLevelString = "error"
	// ErrorLevel is the error level priority
	ErrorLevel = zapcore.ErrorLevel

	// InfoLevelString is the string representation of the info level
	InfoLevelString = "info"
	// InfoLevel is the info level priority
	InfoLevel = zapcore.InfoLevel

	// DebugLevelString is the string representation of the debug level
	DebugLevelString = "debug"
	// DebugLevel is the debug level priority
	DebugLevel zapcore.Level = -2

	// TraceLevelString is the string representation of the trace level
	TraceLevelString = "trace"
	// TraceLevel is the trace level priority
	TraceLevel zapcore.Level = -4

	// WarningLevelString is the string representation of the warning level
	WarningLevelString = "warning"
	// WarningLevel is the warning level priority
	WarningLevel = zapcore.WarnLevel

	// DefaultLevelString is the string representation of the default level
	DefaultLevelString = InfoLevelString
	// DefaultLevel is the default logging level
	DefaultLevel = InfoLevel
)

type uuidKey struct{}

// Logger wraps a logr.Logger, hiding parts of its APIs
type logger struct {
	logr.Logger

	printCaller bool
}

// Log is the logger that will be used in this package
var log = &logger{Logger: ctrl.Log}

// GetLogger returns the default logger
func GetLogger() Logger {
	return log
}

// Logger is a reduced version of logr.Logger
type Logger interface {
	Enabled() bool

	Error(err error, msg string, keysAndValues ...interface{})
	Warning(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
	Trace(msg string, keysAndValues ...interface{})

	WithCaller() Logger
	WithValues(keysAndValues ...interface{}) Logger
	WithName(name string) Logger

	GetLogger() logr.Logger
}

// SetLogger will set the backing logr implementation for instance manager.
func SetLogger(logr logr.Logger) {
	log.Logger = logr
}

func (l *logger) enrich() logr.Logger {
	cl := l.GetLogger()

	if l.printCaller {
		_, fileName, fileLine, ok := runtime.Caller(2)
		if ok {
			cl = l.WithValues("caller", fmt.Sprintf("%s:%d", fileName, fileLine)).GetLogger()
		}
	}

	if podName := os.Getenv("POD_NAME"); podName != "" {
		cl = cl.WithValues("logging_pod", podName)
	}

	return cl
}

func (l *logger) GetLogger() logr.Logger {
	return l.Logger
}

func (l *logger) Enabled() bool {
	return l.Logger.Enabled()
}

func (l *logger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.enrich().V(int(-ErrorLevel)).Error(err, msg, keysAndValues...)
}

func (l *logger) Info(msg string, keysAndValues ...interface{}) {
	l.enrich().V(int(-InfoLevel)).Info(msg, keysAndValues...)
}

func (l *logger) Warning(msg string, keysAndValues ...interface{}) {
	l.enrich().V(int(-WarningLevel)).Info(msg, keysAndValues...)
}

func (l *logger) Debug(msg string, keysAndValues ...interface{}) {
	l.enrich().V(int(-DebugLevel)).Info(msg, keysAndValues...)
}

func (l *logger) Trace(msg string, keysAndValues ...interface{}) {
	l.enrich().V(int(-TraceLevel)).Info(msg, keysAndValues...)
}

func (l *logger) WithValues(keysAndValues ...interface{}) Logger {
	return &logger{Logger: l.Logger.WithValues(keysAndValues...), printCaller: l.printCaller}
}

func (l *logger) WithName(name string) Logger {
	return &logger{Logger: l.Logger.WithName(name), printCaller: l.printCaller}
}

func (l logger) WithCaller() Logger {
	return &logger{Logger: l.Logger, printCaller: true}
}

// Enabled exposes the same method from the logr.Logger interface using the default logger
func Enabled() bool {
	return log.Enabled()
}

// Error exposes the same method from the logr.Logger interface using the default logger
func Error(err error, msg string, keysAndValues ...interface{}) {
	log.Error(err, msg, keysAndValues...)
}

// Info exposes the same method from the logr.Logger interface using the default logger
func Info(msg string, keysAndValues ...interface{}) {
	log.Info(msg, keysAndValues...)
}

// Warning exposes the same method from the logr.Logger interface using the default logger
func Warning(msg string, keysAndValues ...interface{}) {
	log.Warning(msg, keysAndValues...)
}

// Debug exposes the same method from the logr.Logger interface using the default logger
func Debug(msg string, keysAndValues ...interface{}) {
	log.Debug(msg, keysAndValues...)
}

// Trace exposes the same method from the logr.Logger interface using the default logger
func Trace(msg string, keysAndValues ...interface{}) {
	log.Trace(msg, keysAndValues...)
}

// WithValues exposes the same method from the logr.Logger interface using the default logger
func WithValues(keysAndValues ...interface{}) Logger {
	return log.WithValues(keysAndValues...)
}

// WithName exposes the same method from the logr.Logger interface using the default logger
func WithName(name string) Logger {
	return log.WithName(name)
}

// WithCaller exposes the same method from logr.Logger interface using the default logger
func WithCaller() Logger {
	return log.WithCaller()
}

// FromContext builds a logger with some additional information stored in the context
func FromContext(ctx context.Context) Logger {
	l, ok := ctx.Value(logger{}).(Logger)
	if !ok {
		l = &logger{Logger: ctrlLog.FromContext(ctx)}
	}

	uuid, ok := ctx.Value(uuidKey{}).(uuid.UUID)
	if ok {
		l = l.WithValues("uuid", uuid)
	}

	return l
}

// IntoContext injects a logger into a context
func IntoContext(ctx context.Context, log Logger) context.Context {
	return ctrlLog.IntoContext(ctx, log.GetLogger())
}

// AddUUID wraps a given context to inject a new uuid
func AddUUID(ctx context.Context) (context.Context, error) {
	id, err := uuid.NewUUID()
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, uuidKey{}, id), nil
}

// SetupLogger sets up the logger from a given context, wrapping it with a new uuid, and any given name
func SetupLogger(ctx context.Context) (Logger, context.Context) {
	if newCtx, err := AddUUID(ctx); err == nil {
		ctx = newCtx
	}

	// The only error that we can have calling FromContext() is a not found
	// in which case we will have an empty not nil value for newLogger which
	// still useful when setting up the logger
	newLogger, _ := logr.FromContext(ctx)

	return FromContext(ctx), IntoContext(ctx, &logger{Logger: newLogger})
}
