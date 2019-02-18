/*
Copyright 2018 The Vulcan Authors.

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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"hpw.cloud/volcano/pkg/client/clientset/versioned"
)

type listFlags struct {
	commonFlags

	Namespace string
}

const(
	Name string = "Name"
	Creation string = "Creation"
	Phase string = "Phase"
	Replicas string = "Replicas"
	Min string = "Min"
	Pending string = "Pending"
	Running string = "Running"
	Succeeded string = "Succeeded"
	Failed string = "Failed"
)
//NOTE: Need to update the print command result codes as well if columns are add/delete/update
var ListColumns = []string{Name, Creation, Phase, Replicas, Min, Pending, Running, Succeeded, Failed}

var listJobFlags = &listFlags{}

func InitListFlags(cmd *cobra.Command) {
	initFlags(cmd, &listJobFlags.commonFlags)

	cmd.Flags().StringVarP(&listJobFlags.Namespace, "namespace", "", "default", "the namespace of job")
}

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

	Headers := make([]interface{}, len(ListColumns))
	for i,h := range ListColumns{
		Headers[i] = h
	}
	fmt.Printf("%-25s%-25s%-12s%-12s%-6s%-10s%-10s%-12s%-10s\n",Headers...)
	for _, job := range jobs.Items {
		replicas := int32(0)
		for _, ts := range job.Spec.Tasks {
			replicas += ts.Replicas
		}
		//Print job attributes according to the sequence of header's
		fmt.Printf("%-25s%-25s%-12s%-12d%-6d%-10d%-10d%-12d%-10d\n",
			job.Name, job.CreationTimestamp.Format("2006-01-02 15:04:05"), job.Status.State.Phase, replicas,
			job.Status.MinAvailable, job.Status.Pending, job.Status.Running, job.Status.Succeeded, job.Status.Failed)
	}

	return nil
}
