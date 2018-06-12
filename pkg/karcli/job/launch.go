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

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset"
)

type launchFlags struct {
	Name          string
	Namespace     string
	Image         string
	Master        string
	MinAvailable  int
	Replicas      int
	Requests      string
	SchedulerName string
}

var launchJobFlags = &launchFlags{}

func InitLaunchFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&launchJobFlags.Image, "image", "i", "busybox", "the container image of job")
	cmd.Flags().StringVarP(&launchJobFlags.Namespace, "namespace", "", "default", "the namespace of job")
	cmd.Flags().StringVarP(&launchJobFlags.Name, "name", "n", "test", "the name of job")
	cmd.Flags().IntVarP(&launchJobFlags.MinAvailable, "min", "m", 1, "the minimal available tasks of job")
	cmd.Flags().IntVarP(&launchJobFlags.Replicas, "replicas", "r", 1, "the total tasks of job")
	cmd.Flags().StringVarP(&launchJobFlags.Requests, "requests", "", "cpu=1000m,memory=100Mi", "the resource request of the task")
	cmd.Flags().StringVarP(&launchJobFlags.SchedulerName, "scheduler", "", "kar-scheduler", "the scheduler for this job")
	cmd.Flags().StringVarP(&launchJobFlags.Master, "master", "s", "localhost:8080", "the address of api server")
}

func LaunchJob() {
	config, err := buildConfig(launchJobFlags.Master, "")
	if err != nil {
		panic(err)
	}

	queueClient := clientset.NewForConfigOrDie(config)

	req, err := populateResourceListV1(launchJobFlags.Requests)
	if err != nil {
		panic(err)
	}

	qj := &arbv1.QueueJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      launchJobFlags.Name,
			Namespace: launchJobFlags.Namespace,
		},
		Spec: arbv1.QueueJobSpec{
			Replicas: int32(launchJobFlags.Replicas),
			SchedSpec: arbv1.SchedulingSpecTemplate{
				MinAvailable: launchJobFlags.MinAvailable,
			},
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					SchedulerName: launchJobFlags.SchedulerName,
					Containers: []v1.Container{
						{
							Image: launchJobFlags.Image,
							Name:  launchJobFlags.Name,
							Resources: v1.ResourceRequirements{
								Requests: req,
							},
						},
					},
				},
			},
		},
	}

	if _, err := queueClient.ArbV1().QueueJobs(launchJobFlags.Namespace).Create(qj); err != nil {
		panic(err)
	}
}
