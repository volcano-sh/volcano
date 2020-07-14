/*
Copyright 2019 The Volcano Authors.

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

package job

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"
)

//ResumeJob resumes the job in the given namespace.
func ResumeJob(name string, namespace string) string {
	command := []string{"job", "resume"}
	Expect(name).NotTo(Equal(""), "Job name should not be empty in Resume job command")
	command = append(command, "--name", name)
	if namespace != "" {
		command = append(command, "--namespace", namespace)
	}
	return RunCliCommand(command)
}

// SuspendJob suspends the job in the given namepsace.
func SuspendJob(name string, namespace string) string {
	command := []string{"job", "suspend"}
	Expect(name).NotTo(Equal(""), "Job name should not be empty in Suspend job command")
	command = append(command, "--name", name)
	if namespace != "" {
		command = append(command, "--namespace", namespace)
	}
	return RunCliCommand(command)
}

// ListJobs list all the jobs in the given namespace.
func ListJobs(namespace string) string {
	command := []string{"job", "list"}
	if namespace != "" {
		command = append(command, "--namespace", namespace)
	}
	return RunCliCommand(command)
}

// DeleteJob delete the job in the given namespace.
func DeleteJob(name string, namespace string) string {
	command := []string{"job", "delete"}
	Expect(name).NotTo(Equal(""), "Job name should not be empty in delete job command")
	command = append(command, "--name", name)
	if namespace != "" {
		command = append(command, "--namespace", namespace)
	}
	return RunCliCommand(command)
}

// RunCliCommand runs the volcano command.
func RunCliCommand(command []string) string {
	if masterURL() != "" {
		command = append(command, "--master", masterURL())
	}
	command = append(command, "--kubeconfig", kubeconfigPath(homeDir()))
	vcctl := VolcanoCliBinary()
	Expect(fileExist(vcctl)).To(BeTrue(), fmt.Sprintf(
		"vcctl binary: %s is required for E2E tests, please update VC_BIN environment", vcctl))
	output, err := exec.Command(vcctl, command...).Output()
	Expect(err).NotTo(HaveOccurred(),
		fmt.Sprintf("Command %s failed to execute: %s", strings.Join(command, ""), err))
	return string(output)
}

// RunCliCommandWithoutKubeConfig runs the volcano command.
func RunCliCommandWithoutKubeConfig(command []string) string {
	if masterURL() != "" {
		command = append(command, "--master", masterURL())
	}
	vcctl := VolcanoCliBinary()
	Expect(fileExist(vcctl)).To(BeTrue(), fmt.Sprintf(
		"vcctl binary: %s is required for E2E tests, please update VC_BIN environment", vcctl))
	output, err := exec.Command(vcctl, command...).Output()
	Expect(err).NotTo(HaveOccurred(),
		fmt.Sprintf("Command %s failed to execute: %s", strings.Join(command, ""), err))
	return string(output)
}
