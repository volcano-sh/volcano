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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset"
	"github.com/spf13/cobra"
)

type listFlags struct {
	commonFlags

	Namespace string
}

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

	queueClient := clientset.NewForConfigOrDie(config)

	queueJobs, err := queueClient.ArbV1().QueueJobs(listJobFlags.Namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	if len(queueJobs.Items) == 0 {
		fmt.Printf("No resources found\n")
		return nil
	}

	fmt.Printf("%-30s%-25s%-12s%-8s%-12s%-12s%-12s%-12s\n",
		"Name", "Creation", "Replicas", "Min", "Pending", "Running", "Succeeded", "Failed")
	for _, qj := range queueJobs.Items {
		replicas := int32(0)
		for _, ts := range qj.Spec.TaskSpecs {
			replicas += ts.Replicas
		}

		fmt.Printf("%-30s%-25s%-12d%-8d%-12d%-12d%-12d%-12d\n",
			qj.Name, qj.CreationTimestamp.Format("2006-01-02 15:04:05"), replicas,
			qj.Status.MinAvailable, qj.Status.Pending, qj.Status.Running, qj.Status.Succeeded, qj.Status.Failed)
	}

	return nil
}
