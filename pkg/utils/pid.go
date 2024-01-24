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

package utils

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/cloudnative-pg/cloudnative-pg/pkg/fileutils"
)

// Process store the information of a given Pid
type Process struct {
	Pid  int
	Name string
	Tgid int
	Ngid int
	Ppid int
}

// GetProcessByPid use the PID to find the process and gather information
// looking for the stat directory
func GetProcessByPid(pid int) (*Process, error) {
	if pid < 1 {
		return nil, nil
	}
	process := &Process{Pid: pid}
	pidDir := fmt.Sprintf("/proc/%d", pid)
	_, err := os.Stat(pidDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	statPath := fmt.Sprintf("%s/status", pidDir)
	if exists, err := fileutils.FileExists(statPath); !exists || err != nil {
		if !exists {
			return nil, fmt.Errorf("status file doesn't exists for the process")
		}
		return nil, err
	}

	lines, err := fileutils.ReadFileLines(statPath)
	if err != nil {
		return nil, err
	}

	// Per documentation https://www.kernel.org/doc/Documentation/filesystems/proc.txt
	// section Table 1-2 the order of the lines doesn't change and has a fixed value
	// since Kernel 4.19
	process.Name = strings.Trim(strings.Split(lines[0], ":")[1], " \t\r")
	process.Tgid, _ = strconv.Atoi(strings.Trim(strings.Split(lines[3], ":")[1], " "))
	process.Ngid, _ = strconv.Atoi(strings.Trim(strings.Split(lines[4], ":")[1], " "))
	process.Ppid, _ = strconv.Atoi(strings.Trim(strings.Split(lines[6], ":")[1], " "))

	return process, nil
}

// GetAllProcesses search in all the pids available in /proc looking for a process
// with the given name in the executable
func GetAllProcesses() ([]Process, error) {
	var err error
	dir, err := os.Open("/proc")
	if err != nil {
		return nil, err
	}
	defer func() {
		if errClose := dir.Close(); errClose != nil {
			err = errClose
		}
	}()

	dirs, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}

	processes := make([]Process, 0, 50)

	for _, dirName := range dirs {
		pid, err := strconv.Atoi(dirName.Name())
		if err != nil {
			continue
		}
		process, err := GetProcessByPid(pid)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		processes = append(processes, *process)
	}

	return processes, nil
}
