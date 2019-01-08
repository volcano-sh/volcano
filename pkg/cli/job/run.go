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
	"github.com/spf13/cobra"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vuclanapi "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
	"hpw.cloud/volcano/pkg/client/clientset/versioned"
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

var jobName = "job.volcanproj.org"

func RunJob() error {
	config, err := buildConfig(launchJobFlags.Master, launchJobFlags.Kubeconfig)
	if err != nil {
		return err
	}

	req, err := populateResourceListV1(launchJobFlags.Requests)
	if err != nil {
		return err
	}

	job := &vuclanapi.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      launchJobFlags.Name,
			Namespace: launchJobFlags.Namespace,
		},
		Spec: vuclanapi.JobSpec{
			MinAvailable: int32(launchJobFlags.MinAvailable),
			TaskSpecs: []vuclanapi.TaskSpec{
				{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							jobName: launchJobFlags.Name,
						},
					},
					Replicas: int32(launchJobFlags.Replicas),

					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name:   launchJobFlags.Name,
							Labels: map[string]string{jobName: launchJobFlags.Name},
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

	jobClient := versioned.NewForConfigOrDie(config)
	if _, err := jobClient.Batch().Jobs(launchJobFlags.Namespace).Create(job); err != nil {
		return err
	}

	return nil
}
