/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package log contains the logging subsystem of PGK
package log

import (
	"context"
	"fmt"
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

	// DefaultLevelString is the string representation of the default level
	DefaultLevelString = InfoLevelString
	// DefaultLevel is the default logging level
	DefaultLevel = InfoLevel
)

type uuidKey struct{}

// Logger wraps a logr.Logger, hiding parts of its APIs
type logger struct {
	logr.Logger
}

// Log is the logger that will be used in this package
var log = &logger{ctrl.Log}

// GetLogger returns the default logger
func GetLogger() Logger {
	return log
}

// Logger is a reduced version of logr.Logger
type Logger interface {
	Enabled() bool

	Error(err error, msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Debug(msg string, keysAndValues ...interface{})
	Trace(msg string, keysAndValues ...interface{})

	WithCaller() Logger
	WithValues(keysAndValues ...interface{}) Logger
	WithName(name string) Logger
	getLogger() logr.Logger
}

// SetLogger will set the backing logr implementation for instance manager.
func SetLogger(logr logr.Logger) {
	log.Logger = logr
}

func (l *logger) getLogger() logr.Logger {
	return l.Logger
}

func (l *logger) Enabled() bool {
	return l.Logger.Enabled()
}

func (l *logger) Debug(msg string, keysAndValues ...interface{}) {
	l.Logger.V(int(-DebugLevel)).Info(msg, keysAndValues...)
}

func (l *logger) Trace(msg string, keysAndValues ...interface{}) {
	l.Logger.V(int(-TraceLevel)).Info(msg, keysAndValues...)
}

func (l *logger) WithValues(keysAndValues ...interface{}) Logger {
	return &logger{l.Logger.WithValues(keysAndValues...)}
}

func (l *logger) WithName(name string) Logger {
	return &logger{l.Logger.WithName(name)}
}

func (l *logger) WithCaller() Logger {
	_, fileName, fileLine, ok := runtime.Caller(2)
	if ok {
		return l.WithValues("caller", fmt.Sprintf("%s:%d", fileName, fileLine))
	}
	return l
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

// FromContext builds a logger with some additional information stored in the context
func FromContext(ctx context.Context) Logger {
	var l Logger = &logger{ctrlLog.FromContext(ctx)}
	uuid, ok := ctx.Value(uuidKey{}).(uuid.UUID)
	if ok {
		l = l.WithValues("uuid", uuid)
	}
	return l.WithCaller()
}

// IntoContext injects a logger into a context
func IntoContext(ctx context.Context, log Logger) context.Context {
	return ctrlLog.IntoContext(ctx, log.getLogger())
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
	return FromContext(ctx), IntoContext(ctx, &logger{logr.FromContext(ctx)})
}
