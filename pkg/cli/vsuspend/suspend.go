/*
Copyright 2019 The Vulcan Authors.

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

package vsuspend

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"volcano.sh/volcano/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
)

type suspendFlags struct {
	util.CommonFlags

	Namespace string
	JobName   string
}

var suspendJobFlags = &suspendFlags{}

const (
	// DefaultJobNamespaceEnv is the env name of default namespace of the job
	DefaultJobNamespaceEnv = "VOLCANO_DEFAULT_JOB_NAMESPACE"

	defaultJobNamespace = "default"
)

// InitSuspendFlags  init suspend related flags
func InitSuspendFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &suspendJobFlags.CommonFlags)

	cmd.Flags().StringVarP(&suspendJobFlags.Namespace, "namespace", "N", "",
		fmt.Sprintf("the namespace of job, overwrite the value of '%s' (default \"%s\")", DefaultJobNamespaceEnv, defaultJobNamespace))
	cmd.Flags().StringVarP(&suspendJobFlags.JobName, "name", "n", "", "the name of job")

	setDefaultArgs()
}

func setDefaultArgs() {

	if suspendJobFlags.Namespace == "" {
		namespace := os.Getenv(DefaultJobNamespaceEnv)

		if namespace != "" {
			suspendJobFlags.Namespace = namespace
		} else {
			suspendJobFlags.Namespace = defaultJobNamespace
		}
	}

}

// SuspendJob  suspends the job
func SuspendJob() error {
	config, err := util.BuildConfig(suspendJobFlags.Master, suspendJobFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if suspendJobFlags.JobName == "" {
		err := fmt.Errorf("job name is mandatory to suspend a particular job")
		return err
	}

	return util.CreateJobCommand(config,
		suspendJobFlags.Namespace, suspendJobFlags.JobName,
		v1alpha1.AbortJobAction)
}
