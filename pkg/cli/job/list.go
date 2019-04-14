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
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubernetes-sigs/volcano/pkg/apis/batch/v1alpha1"
	"github.com/kubernetes-sigs/volcano/pkg/client/clientset/versioned"
)

type listFlags struct {
	commonFlags

	Namespace string
}

const (
	// Name const
	Name string = "Name"
	// Creation const
	Creation string = "Creation"
	// Phase const
	Phase string = "Phase"
	// Replicas const
	Replicas string = "Replicas"
	// Min const
	Min string = "Min"
	// Pending const
	Pending string = "Pending"
	// Running const
	Running string = "Running"
	// Succeeded const
	Succeeded string = "Succeeded"
	// Failed const
	Failed string = "Failed"
)

var listJobFlags = &listFlags{}

// InitListFlags inits all flags
func InitListFlags(cmd *cobra.Command) {
	initFlags(cmd, &listJobFlags.commonFlags)

	cmd.Flags().StringVarP(&listJobFlags.Namespace, "namespace", "", "default", "the namespace of job")
}

// ListJobs lists all the jobs
func ListJobs() error {
	config, err := buildConfig(listJobFlags.Master, listJobFlags.Kubeconfig)
	if err != nil {
		return err
	}

	jobClient := versioned.NewForConfigOrDie(config)
	jobs, err := jobClient.BatchV1alpha1().Jobs(listJobFlags.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(jobs.Items) == 0 {
		fmt.Printf("No resources found\n")
		return nil
	}
	PrintJobs(jobs, os.Stdout)

	return nil
}

// PrintJobs prints all the jobs
func PrintJobs(jobs *v1alpha1.JobList, writer io.Writer) {
	_, err := fmt.Fprintf(writer, "%-25s%-25s%-12s%-12s%-6s%-10s%-10s%-12s%-10s\n",
		Name, Creation, Phase, Replicas, Min, Pending, Running, Succeeded, Failed)
	if err != nil {
		fmt.Printf("Failed to print list command result: %s.\n", err)
	}
	for _, job := range jobs.Items {
		replicas := int32(0)
		for _, ts := range job.Spec.Tasks {
			replicas += ts.Replicas
		}
		_, err = fmt.Fprintf(writer, "%-25s%-25s%-12s%-12d%-6d%-10d%-10d%-12d%-10d\n",
			job.Name, job.CreationTimestamp.Format("2006-01-02 15:04:05"), job.Status.State.Phase, replicas,
			job.Status.MinAvailable, job.Status.Pending, job.Status.Running, job.Status.Succeeded, job.Status.Failed)
		if err != nil {
			fmt.Printf("Failed to print list command result: %s.\n", err)
		}
	}
}
