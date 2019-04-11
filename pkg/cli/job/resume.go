/*
Copyright 2018 The Kubernetes Authors.

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
	"github.com/spf13/cobra"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/batch/v1alpha1"
)

type resumeFlags struct {
	commonFlags

	Namespace string
	JobName   string
}

var resumeJobFlags = &resumeFlags{}

// InitResumeFlags is used to init all resume flags
func InitResumeFlags(cmd *cobra.Command) {
	initFlags(cmd, &resumeJobFlags.commonFlags)

	cmd.Flags().StringVarP(&resumeJobFlags.Namespace, "namespace", "", "default", "the namespace of job")
	cmd.Flags().StringVarP(&resumeJobFlags.JobName, "name", "n", "", "the name of job")
}

// ResumeJob creates commands to resume job
func ResumeJob() error {
	config, err := buildConfig(resumeJobFlags.Master, resumeJobFlags.Kubeconfig)
	if err != nil {
		return err
	}

	return createJobCommand(config,
		resumeJobFlags.Namespace, resumeJobFlags.JobName,
		v1alpha1.ResumeJobAction)
}
