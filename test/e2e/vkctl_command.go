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

package e2e

import (
	"fmt"
	"os/exec"
	"strings"

	. "github.com/onsi/gomega"
)

func ListJobs(namespace string) string {
	command := []string{"job", "list"}
	if namespace != "" {
		command = append(command, "--namespace", namespace)
	}
	return RunCliCommand(command)
}

func RunCliCommand(command []string) string {
	if masterURL() != "" {
		command = append(command, "--master", masterURL())
	}
	command = append(command, "--kubeconfig", kubeconfigPath(homeDir()))
	output, err := exec.Command(VolcanoCliBinary(), command...).Output()
	Expect(err).NotTo(HaveOccurred(),
		fmt.Sprintf("Command %s failed to execute: %s", strings.Join(command, ""), err))
	return string(output)
}
