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

package vsub

import (
	"context"
	"fmt"
	"os"

	"github.com/google/shlex"
	"github.com/spf13/cobra"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vcbatch "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/cli/util"
	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

type runFlags struct {
	util.CommonFlags

	Name      string
	Namespace string
	Image     string

	MinAvailable  int
	Replicas      int
	Requests      string
	Limits        string
	SchedulerName string
	FileName      string
	Command       string
}

var launchJobFlags = &runFlags{}

const (
	// SchedulerNameEnv is the env name of default scheduler name.
	SchedulerNameEnv = "VOLCANO_SCHEDULER_NAME"

	// DefaultImageEnv is the env name of default image.
	DefaultImageEnv = "VOLCANO_DEFAULT_IMAGE"

	// DefaultJobNamespaceEnv is the env name of default namespace of the job
	DefaultJobNamespaceEnv = "VOLCANO_DEFAULT_JOB_NAMESPACE"

	defaultImage         = "busybox"
	defaultSchedulerName = "volcano"
	defaultJobNamespace  = "default"
)

// InitRunFlags init the run flags.
func InitRunFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &launchJobFlags.CommonFlags)

	cmd.Flags().StringVarP(&launchJobFlags.Image, "image", "i", "",
		fmt.Sprintf("the container image of job, overwrite the value of '%s' (default \"%s\")",
			DefaultImageEnv, defaultImage))
	cmd.Flags().StringVarP(&launchJobFlags.Namespace, "namespace", "N", "",
		fmt.Sprintf("the namespace of job, overwrite the value of '%s' (default \"%s\")", DefaultJobNamespaceEnv, defaultJobNamespace))
	cmd.Flags().StringVarP(&launchJobFlags.Name, "name", "n", "", "the name of job")
	cmd.Flags().IntVarP(&launchJobFlags.MinAvailable, "min", "m", 1, "the minimal available tasks of job")
	cmd.Flags().IntVarP(&launchJobFlags.Replicas, "replicas", "r", 1, "the total tasks of job")
	cmd.Flags().StringVarP(&launchJobFlags.Requests, "requests", "R", "cpu=1000m,memory=100Mi", "the resource request of the task")
	cmd.Flags().StringVarP(&launchJobFlags.Limits, "limits", "L", "cpu=1000m,memory=100Mi", "the resource limit of the task")
	cmd.Flags().StringVarP(&launchJobFlags.SchedulerName, "scheduler", "S", "",
		fmt.Sprintf("the scheduler for this job, overwrite the value of '%s' (default \"%s\")",
			SchedulerNameEnv, defaultSchedulerName))
	cmd.Flags().StringVarP(&launchJobFlags.Command, "command", "c", "", "the command of of job")

	setDefaultArgs()
}

func setDefaultArgs() {
	if launchJobFlags.SchedulerName == "" {
		schedulerName := os.Getenv(SchedulerNameEnv)

		if schedulerName != "" {
			launchJobFlags.SchedulerName = schedulerName
		} else {
			launchJobFlags.SchedulerName = defaultSchedulerName
		}
	}

	if launchJobFlags.Image == "" {
		image := os.Getenv(DefaultImageEnv)

		if image != "" {
			launchJobFlags.Image = image
		} else {
			launchJobFlags.Image = defaultImage
		}
	}

	if launchJobFlags.Namespace == "" {
		namespace := os.Getenv(DefaultJobNamespaceEnv)

		if namespace != "" {
			launchJobFlags.Namespace = namespace
		} else {
			launchJobFlags.Namespace = defaultJobNamespace
		}
	}

}

var jobName = "job.volcano.sh"

// RunJob creates the job.
func RunJob() error {
	config, err := util.BuildConfig(launchJobFlags.Master, launchJobFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if launchJobFlags.Name == "" {
		err = fmt.Errorf("job name cannot be left blank")
		return err
	}

	req, err := util.PopulateResourceListV1(launchJobFlags.Requests)
	if err != nil {
		return err
	}

	limit, err := util.PopulateResourceListV1(launchJobFlags.Limits)
	if err != nil {
		return err
	}

	job, err := constructLaunchJobFlagsJob(launchJobFlags, req, limit)
	if err != nil {
		return err
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

func constructLaunchJobFlagsJob(launchJobFlags *runFlags, req, limit v1.ResourceList) (*vcbatch.Job, error) {

	var commands []string

	if launchJobFlags.Command != "" {
		var err error
		if commands, err = shlex.Split(launchJobFlags.Command); err != nil {
			return nil, err
		}
	}

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
									Command:         commands,
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
	}, nil
}
