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
	"io/ioutil"
	"strings"

	"github.com/spf13/cobra"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

type runFlags struct {
	commonFlags

	Name      string
	Namespace string
	Image     string

	MinAvailable  int
	Replicas      int
	Requests      string
	Limits        string
	SchedulerName string
	FileName      string
}

var launchJobFlags = &runFlags{}

// InitRunFlags init the run flags.
func InitRunFlags(cmd *cobra.Command) {
	initFlags(cmd, &launchJobFlags.commonFlags)

	cmd.Flags().StringVarP(&launchJobFlags.Image, "image", "i", "busybox", "the container image of job")
	cmd.Flags().StringVarP(&launchJobFlags.Namespace, "namespace", "n", "default", "the namespace of job")
	cmd.Flags().StringVarP(&launchJobFlags.Name, "name", "N", "", "the name of job")
	cmd.Flags().IntVarP(&launchJobFlags.MinAvailable, "min", "m", 1, "the minimal available tasks of job")
	cmd.Flags().IntVarP(&launchJobFlags.Replicas, "replicas", "r", 1, "the total tasks of job")
	cmd.Flags().StringVarP(&launchJobFlags.Requests, "requests", "R", "cpu=1000m,memory=100Mi", "the resource request of the task")
	cmd.Flags().StringVarP(&launchJobFlags.Limits, "limits", "L", "cpu=1000m,memory=100Mi", "the resource limit of the task")
	cmd.Flags().StringVarP(&launchJobFlags.SchedulerName, "scheduler", "S", "volcano", "the scheduler for this job")
	cmd.Flags().StringVarP(&launchJobFlags.FileName, "filename", "f", "", "the yaml file of job")
}

var jobName = "job.volcano.sh"

// RunJob creates the job.
func RunJob() error {
	config, err := util.BuildConfig(launchJobFlags.Master, launchJobFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if launchJobFlags.Name == "" && launchJobFlags.FileName == "" {
		err = fmt.Errorf("job name cannot be left blank")
		return err
	}

	req, err := populateResourceListV1(launchJobFlags.Requests)
	if err != nil {
		return err
	}

	limit, err := populateResourceListV1(launchJobFlags.Limits)
	if err != nil {
		return err
	}

	job, err := readFile(launchJobFlags.FileName)
	if err != nil {
		return err
	}

	if job == nil {
		job = constructLaunchJobFlagsJob(launchJobFlags, req, limit)
	}

	jobClient := versioned.NewForConfigOrDie(config)
	newJob, err := jobClient.BatchV1alpha1().Jobs(launchJobFlags.Namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	if newJob.Spec.Queue == "" {
		newJob.Spec.Queue = "default"
	}

	fmt.Printf("run job %v successfully\n", newJob.Name)

	return nil
}

func readFile(filename string) (*vcbatch.Job, error) {
	if filename == "" {
		return nil, nil
	}

	if !strings.Contains(filename, ".yaml") && !strings.Contains(filename, ".yml") {
		return nil, fmt.Errorf("only support yaml file")
	}

	file, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file, err: %v", err)
	}

	var job vcbatch.Job
	if err := yaml.Unmarshal(file, &job); err != nil {
		return nil, fmt.Errorf("Failed to unmarshal file, err:  %v", err)
	}

	return &job, nil
}

func constructLaunchJobFlagsJob(launchJobFlags *runFlags, req, limit v1.ResourceList) *vcbatch.Job {
	return &vcbatch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      launchJobFlags.Name,
			Namespace: launchJobFlags.Namespace,
		},
		Spec: vcbatch.JobSpec{
			MinAvailable:  int32(launchJobFlags.MinAvailable),
			SchedulerName: launchJobFlags.SchedulerName,
			Tasks: []vcbatch.TaskSpec{
				{
					Replicas: int32(launchJobFlags.Replicas),

					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Name:   launchJobFlags.Name,
							Labels: map[string]string{jobName: launchJobFlags.Name},
						},
						Spec: v1.PodSpec{
							RestartPolicy: v1.RestartPolicyNever,
							Containers: []v1.Container{
								{
									Image:           launchJobFlags.Image,
									Name:            launchJobFlags.Name,
									ImagePullPolicy: v1.PullIfNotPresent,
									Resources: v1.ResourceRequirements{
										Limits:   limit,
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
}
