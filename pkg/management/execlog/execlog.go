/*
This file is part of Cloud Native PostgreSQL.

Copyright (C) 2019-2021 EnterpriseDB Corporation.
*/

// Package execlog handles stdout and stderr pipes of started commands
// and logs them in JSON using the provided logger
package execlog

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"

	"github.com/EnterpriseDB/cloud-native-postgresql/pkg/management/log"
)

const (
	// PipeKey is the key for the pipe the log refers to
	PipeKey = "pipe"
	// StdOut is the PipeKey value for stdout
	StdOut = "stdout"
	// StdErr is the PipeKey value for stderr
	StdErr = "stderr"
)

// RunStreaming executes the command redirecting its stdout and stderr to the logger.
// This function waits for command to terminate end reports non-zero exit codes.
func RunStreaming(cmd *exec.Cmd, cmdName string) (err error) {
	if err := RunStreamingNoWait(cmd, cmdName); err != nil {
		return err
	}

	return cmd.Wait()
}

// RunStreamingNoWait executes the command redirecting its stdout and stderr to the logger.
// This function does not wait for command to terminate.
func RunStreamingNoWait(cmd *exec.Cmd, cmdName string) (err error) {
	logger := log.WithName(cmdName)

	stdoutWriter := &LogWriter{
		Logger: logger.WithValues(PipeKey, StdOut),
	}
	stderrWriter := &LogWriter{
		Logger: logger.WithValues(PipeKey, StdErr),
	}

	return RunStreamingNoWaitWithWriter(cmd, cmdName, stdoutWriter, stderrWriter)
}

// copyPipe is an internal function used to copy the content of a io.Reader
// into a io.Writer one line at a time.
func copyPipe(dst io.Writer, src io.ReadCloser, logger log.Logger) {
	defer func() {
		err := src.Close()
		if err != nil {
			logger.Error(err, "error closing src pipe")
		}
	}()

	scanner := bufio.NewScanner(src)

	for scanner.Scan() {
		line := scanner.Bytes()
		_, err := dst.Write(line)
		if err != nil {
			logger.Error(err, "can't write to dst writer", "line", line)
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Error(err, "can't scan from src pipe")
	}
}

// RunBuffering creates two dedicated pipes for stdout and stderr, run the command and logs its output after
// the command exited
func RunBuffering(cmd *exec.Cmd, cmdName string) (err error) {
	logger := log.WithName(cmdName)

	var stdoutBuffer, stderrBuffer bytes.Buffer

	cmd.Stdout = &stdoutBuffer
	cmd.Stderr = &stderrBuffer
	err = cmd.Run()

	// Log stdout/stderr regardless of error status
	if s := stdoutBuffer.String(); len(s) > 0 {
		logger.WithValues(PipeKey, StdOut).Info(s)
	}

	if s := stderrBuffer.String(); len(s) > 0 {
		logger.WithValues(PipeKey, StdErr).Info(s)
	}

	return err
}

// RunStreamingNoWaitWithWriter executes the command redirecting its stdout and stderr to the corresponding writers.
// This function does not wait for command to terminate.
func RunStreamingNoWaitWithWriter(
	cmd *exec.Cmd,
	cmdName string,
	stdoutWriter io.Writer,
	stderrWriter io.Writer,
) (err error) {
	logger := log.WithName(cmdName)

	stdoutPipeRead, stdoutPipeWrite, err := os.Pipe()
	if err != nil {
		return err
	}

	stderrPipeRead, stderrPipeWrite, err := os.Pipe()
	if err != nil {
		return err
	}

	cmd.Stdout = stdoutPipeWrite
	cmd.Stderr = stderrPipeWrite
	err = cmd.Start()
	if err != nil {
		return err
	}

	err = stdoutPipeWrite.Close()
	if err != nil {
		return err
	}

	err = stderrPipeWrite.Close()
	if err != nil {
		return err
	}

	go copyPipe(stdoutWriter, stdoutPipeRead, logger)

	go copyPipe(stderrWriter, stderrPipeRead, logger)

	return nil
}
