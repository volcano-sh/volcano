/*
Copyright 2018 The Volcano Authors.

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

	"github.com/spf13/cobra"

	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
)

type suspendFlags struct {
	commonFlags

	Namespace string
	JobName   string
}

var suspendJobFlags = &suspendFlags{}

// InitSuspendFlags init suspend related flags.
func InitSuspendFlags(cmd *cobra.Command) {
	initFlags(cmd, &suspendJobFlags.commonFlags)

	cmd.Flags().StringVarP(&suspendJobFlags.Namespace, "namespace", "n", "default", "the namespace of job")
	cmd.Flags().StringVarP(&suspendJobFlags.JobName, "name", "N", "", "the name of job")
}

// SuspendJob suspends the job.
func SuspendJob() error {
	config, err := util.BuildConfig(suspendJobFlags.Master, suspendJobFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if suspendJobFlags.JobName == "" {
		err := fmt.Errorf("job name is mandatory to suspend a particular job")
		return err
	}

	return createJobCommand(config,
		suspendJobFlags.Namespace, suspendJobFlags.JobName,
		v1alpha1.AbortJobAction)
}
