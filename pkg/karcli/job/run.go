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

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1alpha1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset"
)

type runFlags struct {
	commonFlags

	Name      string
	Namespace string
	Image     string

	MinAvailable int
	Replicas     int
	Requests     string
}

var launchJobFlags = &runFlags{}

func InitRunFlags(cmd *cobra.Command) {
	initFlags(cmd, &launchJobFlags.commonFlags)

	cmd.Flags().StringVarP(&launchJobFlags.Image, "image", "", "busybox", "the container image of job")
	cmd.Flags().StringVarP(&launchJobFlags.Namespace, "namespace", "", "default", "the namespace of job")
	cmd.Flags().StringVarP(&launchJobFlags.Name, "name", "", "test", "the name of job")
	cmd.Flags().IntVarP(&launchJobFlags.MinAvailable, "min", "", 1, "the minimal available tasks of job")
	cmd.Flags().IntVarP(&launchJobFlags.Replicas, "replicas", "", 1, "the total tasks of job")
	cmd.Flags().StringVarP(&launchJobFlags.Requests, "requests", "", "cpu=1000m,memory=100Mi", "the resource request of the task")
}

var queueJobName = "queuejob.arbitrator.k8s.io"

func RunJob() error {
	config, err := buildConfig(launchJobFlags.Master, launchJobFlags.Kubeconfig)
	if err != nil {
		return err
	}

	queueClient := clientset.NewForConfigOrDie(config)

	req, err := populateResourceListV1(launchJobFlags.Requests)
	if err != nil {
		return err
	}

	qj := &arbv1.QueueJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      launchJobFlags.Name,
			Namespace: launchJobFlags.Namespace,
		},
		Spec: arbv1.QueueJobSpec{
			SchedSpec: arbv1.SchedulingSpecTemplate{
				MinAvailable: launchJobFlags.MinAvailable,
			},
			TaskSpecs: []arbv1.TaskSpec{
				{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							queueJobName: launchJobFlags.Name,
						},
					},
					Replicas: int32(launchJobFlags.Replicas),

					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name:   launchJobFlags.Name,
							Labels: map[string]string{queueJobName: launchJobFlags.Name},
						},
						Spec: v1.PodSpec{
							SchedulerName: launchJobFlags.SchedulerName,
							RestartPolicy: v1.RestartPolicyNever,
							Containers: []v1.Container{
								{
									Image:           launchJobFlags.Image,
									Name:            launchJobFlags.Name,
									ImagePullPolicy: v1.PullIfNotPresent,
									Resources: v1.ResourceRequirements{
										Requests: req,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if _, err := queueClient.ArbV1().QueueJobs(launchJobFlags.Namespace).Create(qj); err != nil {
		return err
	}

	return nil
}
