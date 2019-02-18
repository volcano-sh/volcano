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
	ctlJob "hpw.cloud/volcano/pkg/cli/job"
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
	command = append(command, "--master", masterURL())
	output, err := exec.Command(VolcanoCliBinary(), command...).Output()
	Expect(err).NotTo(HaveOccurred(),
		fmt.Sprintf("Command %s failed to execute: %s", strings.Join(command, ""), err))
	return string(output)
}

func ParseListOutput(content string, jobName string) []map[string]string {
	var parsedJobs []map[string]string
	results := strings.Split(content, "/n")
	Expect(len(results)).Should(BeNumerically(">=", 1),
		"List Command Expected to have one result at least.")
	for _, job := range results[1:] {
		jobAttrs := strings.Split(job, " ")
		jobResult := make(map[string]string)
		for i, t := range ctlJob.ListColumns {
			jobResult[t] = jobAttrs[i]
		}
		if jobName != "" && jobName != jobResult[ctlJob.Name] {
			continue
		}
		parsedJobs = append(parsedJobs, jobResult)
	}
	Expect(len(parsedJobs)).Should(BeNumerically(">=", 1),
		fmt.Sprintf("Can't find Job within name: %s", jobName))
	return parsedJobs
}
