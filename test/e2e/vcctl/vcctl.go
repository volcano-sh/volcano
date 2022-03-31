/*
Copyright 2021 The Volcano Authors.

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

package vcctl

import (
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test Help option of vcctl cli", func() {

	It("Command: vcctl --help", func() {
		var output = `
Usage:
  vcctl [command]

Available Commands:
  help        Help about any command
  job         vcctl command line operation job
  queue       Queue Operations
  version     Print the version information

Flags:
  -h, --help                           help for vcctl
      --log-flush-frequency duration   Maximum number of seconds between log flushes (default 5s)

Use "vcctl [command] --help" for more information about a command.

`
		command := []string{"--help"}
		cmdOutput := RunCliCommandWithoutKubeConfig(command)
		exist := strings.Contains(output, cmdOutput)
		Expect(exist).Should(Equal(true))
	})

	It("Command: vcctl job --help", func() {
		var output = `
vcctl command line operation job

Usage:
  vcctl job [command]

Available Commands:
  delete      delete a job
  list        list job information
  resume      resume a job
  run         run job by parameters from the command line
  suspend     abort a job
  view        show job information

Flags:
  -h, --help   help for job

Global Flags:
      --log-flush-frequency duration   Maximum number of seconds between log flushes (default 5s)

Use "vcctl job [command] --help" for more information about a command.

`
		command := []string{"job", "--help"}
		cmdOutput := RunCliCommandWithoutKubeConfig(command)
		exist := strings.Contains(output, cmdOutput)
		Expect(exist).Should(Equal(true))
	})

	It("Command: vcctl job list --help", func() {
		kubeConfig := os.Getenv("KUBECONFIG")
		var output = `
list job information

Usage:
  vcctl job list [flags]

Flags:
      --all-namespaces      list jobs in all namespaces
  -h, --help                help for list
  -k, --kubeconfig string   (optional) absolute path to the kubeconfig file (default "` + kubeConfig + `")
  -s, --master string       the address of apiserver
  -n, --namespace string    the namespace of job (default "default")
  -S, --scheduler string    list job with specified scheduler name
      --selector string     fuzzy matching jobName

Global Flags:
      --log-flush-frequency duration   Maximum number of seconds between log flushes (default 5s)

`
		command := []string{"job", "list", "--help"}
		cmdOutput := RunCliCommandWithoutKubeConfig(command)
		exist := strings.Contains(output, cmdOutput)
		Expect(exist).Should(Equal(true))
	})

	It("Command: vcctl job suspend -n {$JobName} --help", func() {
		kubeConfig := os.Getenv("KUBECONFIG")
		var output = `
abort a job

Usage:
  vcctl job suspend [flags]

Flags:
  -h, --help                help for suspend
  -k, --kubeconfig string   (optional) absolute path to the kubeconfig file (default "` + kubeConfig + `")
  -s, --master string       the address of apiserver
  -N, --name string         the name of job
  -n, --namespace string    the namespace of job (default "default")

Global Flags:
      --log-flush-frequency duration   Maximum number of seconds between log flushes (default 5s)

`
		command := []string{"job", "suspend", "-n", "job1", "--help"}
		cmdOutput := RunCliCommandWithoutKubeConfig(command)
		exist := strings.Contains(output, cmdOutput)
		Expect(exist).Should(Equal(true))
	})

	It("vcctl job resume -n {$JobName} --help", func() {
		kubeConfig := os.Getenv("KUBECONFIG")
		var output = `
resume a job

Usage:
  vcctl job resume [flags]

Flags:
  -h, --help                help for resume
  -k, --kubeconfig string   (optional) absolute path to the kubeconfig file (default "` + kubeConfig + `")
  -s, --master string       the address of apiserver
  -N, --name string         the name of job
  -n, --namespace string    the namespace of job (default "default")

Global Flags:
      --log-flush-frequency duration   Maximum number of seconds between log flushes (default 5s)

`
		command := []string{"job", "resume", "-n", "job1", "--help"}
		cmdOutput := RunCliCommandWithoutKubeConfig(command)
		exist := strings.Contains(output, cmdOutput)
		Expect(exist).Should(Equal(true))
	})

	It("vcctl job run --help", func() {
		kubeConfig := os.Getenv("KUBECONFIG")
		var output = `
run job by parameters from the command line

Usage:
  vcctl job run [flags]

Flags:
  -f, --filename string     the yaml file of job
  -h, --help                help for run
  -i, --image string        the container image of job (default "busybox")
  -k, --kubeconfig string   (optional) absolute path to the kubeconfig file (default "` + kubeConfig + `")
  -L, --limits string       the resource limit of the task (default "cpu=1000m,memory=100Mi")
  -s, --master string       the address of apiserver
  -m, --min int             the minimal available tasks of job (default 1)
  -N, --name string         the name of job
  -n, --namespace string    the namespace of job (default "default")
  -r, --replicas int        the total tasks of job (default 1)
  -R, --requests string     the resource request of the task (default "cpu=1000m,memory=100Mi")
  -S, --scheduler string    the scheduler for this job (default "volcano")

Global Flags:
      --log-flush-frequency duration   Maximum number of seconds between log flushes (default 5s)

`
		command := []string{"job", "run", "--help"}
		cmdOutput := RunCliCommandWithoutKubeConfig(command)
		exist := strings.Contains(output, cmdOutput)
		Expect(exist).Should(Equal(true))
	})

})
