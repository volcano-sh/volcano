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
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

type listFlags struct {
	commonFlags

	Namespace     string
	SchedulerName string
	allNamespace  bool
	selector      string
}

const (

	// Name  name etc below key words are used in job print format
	Name string = "Name"
	// Creation create
	Creation string = "Creation"
	// Phase phase
	Phase string = "Phase"
	// Replicas  replicas
	Replicas string = "Replicas"
	// Min  minimum
	Min string = "Min"
	// Scheduler scheduler
	Scheduler string = "Scheduler"
	// Pending  pending
	Pending string = "Pending"
	// Running running
	Running string = "Running"
	// Succeeded success
	Succeeded string = "Succeeded"
	// Terminating terminating
	Terminating string = "Terminating"
	// Version version
	Version string = "Version"
	// Failed  failed
	Failed string = "Failed"
	// Unknown pod
	Unknown string = "Unknown"
	// RetryCount retry count
	RetryCount string = "RetryCount"
	// JobType  job type
	JobType string = "JobType"
	// Namespace job namespace
	Namespace string = "Namespace"
)

var listJobFlags = &listFlags{}

// InitListFlags init list command flags.
func InitListFlags(cmd *cobra.Command) {
	initFlags(cmd, &listJobFlags.commonFlags)

	cmd.Flags().StringVarP(&listJobFlags.Namespace, "namespace", "n", "default", "the namespace of job")
	cmd.Flags().StringVarP(&listJobFlags.SchedulerName, "scheduler", "S", "", "list job with specified scheduler name")
	cmd.Flags().BoolVarP(&listJobFlags.allNamespace, "all-namespaces", "", false, "list jobs in all namespaces")
	cmd.Flags().StringVarP(&listJobFlags.selector, "selector", "", "", "fuzzy matching jobName")
}

// ListJobs lists all jobs details.
func ListJobs() error {
	config, err := util.BuildConfig(listJobFlags.Master, listJobFlags.Kubeconfig)
	if err != nil {
		return err
	}
	if listJobFlags.allNamespace {
		listJobFlags.Namespace = ""
	}
	jobClient := versioned.NewForConfigOrDie(config)
	jobs, err := jobClient.BatchV1alpha1().Jobs(listJobFlags.Namespace).List(context.TODO(), metav1.ListOptions{})
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

// PrintJobs prints all jobs details.
func PrintJobs(jobs *v1alpha1.JobList, writer io.Writer) {
	maxLenInfo := getMaxLen(jobs)

	titleFormat := "%%-%ds%%-15s%%-12s%%-12s%%-12s%%-6s%%-10s%%-10s%%-12s%%-10s%%-12s%%-10s\n"
	contentFormat := "%%-%ds%%-15s%%-12s%%-12s%%-12d%%-6d%%-10d%%-10d%%-12d%%-10d%%-12d%%-10d\n"

	var err error
	if listJobFlags.allNamespace {
		_, err = fmt.Fprintf(writer, fmt.Sprintf("%%-%ds"+titleFormat, maxLenInfo[1], maxLenInfo[0]),
			Namespace, Name, Creation, Phase, JobType, Replicas, Min, Pending, Running, Succeeded, Failed, Unknown, RetryCount)
	} else {
		_, err = fmt.Fprintf(writer, fmt.Sprintf(titleFormat, maxLenInfo[0]),
			Name, Creation, Phase, JobType, Replicas, Min, Pending, Running, Succeeded, Failed, Unknown, RetryCount)
	}
	if err != nil {
		fmt.Printf("Failed to print list command result: %s.\n", err)
	}

	for _, job := range jobs.Items {
		if listJobFlags.SchedulerName != "" && listJobFlags.SchedulerName != job.Spec.SchedulerName {
			continue
		}
		if !strings.Contains(job.Name, listJobFlags.selector) {
			continue
		}
		replicas := int32(0)
		for _, ts := range job.Spec.Tasks {
			replicas += ts.Replicas
		}
		jobType := job.ObjectMeta.Labels[v1alpha1.JobTypeKey]
		if jobType == "" {
			jobType = "Batch"
		}

		if listJobFlags.allNamespace {
			_, err = fmt.Fprintf(writer, fmt.Sprintf("%%-%ds"+contentFormat, maxLenInfo[1], maxLenInfo[0]),
				job.Namespace, job.Name, job.CreationTimestamp.Format("2006-01-02"), job.Status.State.Phase, jobType, replicas,
				job.Status.MinAvailable, job.Status.Pending, job.Status.Running, job.Status.Succeeded, job.Status.Failed, job.Status.Unknown, job.Status.RetryCount)
		} else {
			_, err = fmt.Fprintf(writer, fmt.Sprintf(contentFormat, maxLenInfo[0]),
				job.Name, job.CreationTimestamp.Format("2006-01-02"), job.Status.State.Phase, jobType, replicas,
				job.Status.MinAvailable, job.Status.Pending, job.Status.Running, job.Status.Succeeded, job.Status.Failed, job.Status.Unknown, job.Status.RetryCount)
		}
		if err != nil {
			fmt.Printf("Failed to print list command result: %s.\n", err)
		}
	}
}

func getMaxLen(jobs *v1alpha1.JobList) []int {
	maxNameLen := len(Name)
	maxNamespaceLen := len(Namespace)
	for _, job := range jobs.Items {
		if len(job.Name) > maxNameLen {
			maxNameLen = len(job.Name)
		}
		if len(job.Namespace) > maxNamespaceLen {
			maxNamespaceLen = len(job.Namespace)
		}
	}

	return []int{maxNameLen + 3, maxNamespaceLen + 3}
}
