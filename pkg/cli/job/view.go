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
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

type viewFlags struct {
	commonFlags

	Namespace string
	JobName   string
}

var viewJobFlags = &viewFlags{}

// InitViewFlags  init the view command flags
func InitViewFlags(cmd *cobra.Command) {
	initFlags(cmd, &viewJobFlags.commonFlags)

	cmd.Flags().StringVarP(&viewJobFlags.Namespace, "namespace", "N", "default", "the namespace of job")
	cmd.Flags().StringVarP(&viewJobFlags.JobName, "name", "n", "", "the name of job")
}

// ViewJob gives full details of the  job
func ViewJob() error {
	config, err := buildConfig(viewJobFlags.Master, viewJobFlags.Kubeconfig)
	if err != nil {
		return err
	}
	if viewJobFlags.JobName == "" {
		err := fmt.Errorf("job name (specified by --name or -n) is mandatory to view a particular job")
		return err
	}

	jobClient := versioned.NewForConfigOrDie(config)
	job, err := jobClient.BatchV1alpha1().Jobs(viewJobFlags.Namespace).Get(viewJobFlags.JobName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if job == nil {
		fmt.Printf("No resources found\n")
		return nil
	}
	PrintJob(job, os.Stdout)

	return nil
}

// PrintJob  prints the job details
func PrintJob(job *v1alpha1.Job, writer io.Writer) {
	replicas := int32(0)
	for _, ts := range job.Spec.Tasks {
		replicas += ts.Replicas
	}
	lines := []string{
		fmt.Sprintf("%s:\t\t%s", Name, job.Name),
		fmt.Sprintf("%s:\t%s", Creation, job.CreationTimestamp.Format("2006-01-02 15:04:05")),
		fmt.Sprintf("%s:\t%d", Replicas, replicas),
		fmt.Sprintf("%s:\t\t%d", Min, job.Status.MinAvailable),
		fmt.Sprintf("%s:\t%s", Scheduler, job.Spec.SchedulerName),
		"Status",
		fmt.Sprintf("  %s:\t%s", Phase, job.Status.State.Phase),
		fmt.Sprintf("  %s:\t%d", Version, job.Status.Version),
		fmt.Sprintf("  %s:\t%d", RetryCount, job.Status.RetryCount),
		fmt.Sprintf("  %s:\t%d", Pending, job.Status.Pending),
		fmt.Sprintf("  %s:\t%d", Running, job.Status.Running),
		fmt.Sprintf("  %s:\t%d", Succeeded, job.Status.Succeeded),
		fmt.Sprintf("  %s:\t%d", Failed, job.Status.Failed),
		fmt.Sprintf("  %s:\t%d", Terminating, job.Status.Terminating),
	}
	_, err := fmt.Fprint(writer, strings.Join(lines, "\n"), "\n")
	if err != nil {
		fmt.Printf("Failed to print view command result: %s.\n", err)
	}
}
