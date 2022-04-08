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

// Package execlog handles stdout and stderr pipes of started commands
// and logs them in JSON using the provided logger
package execlog

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

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

// StreamingCmd contains the context of a running streaming command
type StreamingCmd struct {
	process   *os.Process
	copyPipes *sync.WaitGroup
	waitDone  chan struct{}
}

// StreamingCmdFromProcess creates a StreamingCmd starting from an
// existing os.Process. This is useful when we want to adopt
// a running PostgreSQL instance.
func StreamingCmdFromProcess(process *os.Process) *StreamingCmd {
	return &StreamingCmd{
		process:   process,
		copyPipes: &sync.WaitGroup{},
	}
}

// Wait waits for the command to exit and waits for any copying
// from stdout or stderr to complete.
//
// The returned error is nil if the command runs, has no problems,
// and exits with a zero exit status.
//
// If the command fails to run or doesn't complete successfully, the
// error is of type *exec.ExitError. Other error types may be
// returned for I/O problems.
//
// This implements the same interface of Wait method of exec.Cmd struct
func (se *StreamingCmd) Wait() error {
	if se == nil {
		return errors.New("nil StreamingCmd")
	}

	// Wait for the process to terminate
	state, err := se.process.Wait()

	// Cleanup any remaining goroutine
	if se.waitDone != nil && se.copyPipes != nil {
		close(se.waitDone)
		se.copyPipes.Wait()
	}

	// Implements the same interface of Wait method of exec.Cmd struct
	if err != nil {
		return err
	} else if !state.Success() {
		return &exec.ExitError{ProcessState: state}
	}

	return nil
}

// RunStreaming executes the command redirecting its stdout and stderr to the logger.
// This function waits for command to terminate end reports non-zero exit codes.
func RunStreaming(cmd *exec.Cmd, cmdName string) (err error) {
	streamingCmd, err := RunStreamingNoWait(cmd, cmdName)
	if err != nil {
		return err
	}

	return streamingCmd.Wait()
}

// RunStreamingNoWait executes the command redirecting its stdout and stderr to the logger.
// This function does not wait for command to terminate.
func RunStreamingNoWait(cmd *exec.Cmd, cmdName string) (streamingCmd *StreamingCmd, err error) {
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

	// Ignore os.ErrDeadlineExceeded here because it is expected
	// when the invoker forcibly terminate the copyPipe cycle.
	if err := scanner.Err(); err != nil && !errors.Is(err, os.ErrDeadlineExceeded) {
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
// This function does not wait for command to terminate. You can invoke StreamingCmd.Wait() to wait for the command
// termination and retrieve the exit status.
func RunStreamingNoWaitWithWriter(
	cmd *exec.Cmd,
	cmdName string,
	stdoutWriter io.Writer,
	stderrWriter io.Writer,
) (streamingCmd *StreamingCmd, err error) {
	logger := log.WithName(cmdName)

	stdoutPipeRead, stdoutPipeWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	stderrPipeRead, stderrPipeWrite, err := os.Pipe()
	if err != nil {
		return nil, err
	}

	cmd.Stdout = stdoutPipeWrite
	cmd.Stderr = stderrPipeWrite
	err = cmd.Start()
	if err != nil {
		return nil, err
	}

	err = stdoutPipeWrite.Close()
	if err != nil {
		return nil, err
	}

	err = stderrPipeWrite.Close()
	if err != nil {
		return nil, err
	}

	var (
		copyPipedStarted sync.WaitGroup
		copyPipes        sync.WaitGroup
	)

	copyPipedStarted.Add(2)
	copyPipes.Add(2)
	go func() {
		copyPipedStarted.Done()
		defer copyPipes.Done()
		copyPipe(stdoutWriter, stdoutPipeRead, logger)
	}()
	go func() {
		copyPipedStarted.Done()
		defer copyPipes.Done()
		copyPipe(stderrWriter, stderrPipeRead, logger)
	}()
	copyPipedStarted.Wait()

	// Set a ReadDeadline on the pipes when the waitDone is closed,
	// allowing the copyPipe functions to terminate even when
	// the child has attached the other end of the pipe
	// to a long-living subprocesses. (i.e. pg_ctl starting postgres)
	waitDone := make(chan struct{})
	go func() {
		<-waitDone
		// Give to copyPipe processes 100 milliseconds to
		// pick all the logs
		deadline := time.Now().Add(100 * time.Millisecond)
		_ = stdoutPipeRead.SetReadDeadline(deadline)
		_ = stderrPipeRead.SetReadDeadline(deadline)
	}()

	return &StreamingCmd{process: cmd.Process, copyPipes: &copyPipes, waitDone: waitDone}, nil
}
