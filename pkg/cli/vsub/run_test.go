/*
Copyright 2026 The Volcano Authors.

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
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestConstructLaunchJobFlagsJob(t *testing.T) {
	flags := &runFlags{
		Name:          "my-job",
		Namespace:     "default",
		MinAvailable:  2,
		Replicas:      3,
		SchedulerName: "volcano",
		Command:       "echo hello world",
	}
	req := v1.ResourceList{v1.ResourceCPU: resource.MustParse("1")}
	limit := v1.ResourceList{v1.ResourceCPU: resource.MustParse("2")}

	job, err := constructLaunchJobFlagsJob(flags, req, limit)
	if err != nil {
		t.Fatalf("constructLaunchJobFlagsJob() unexpected error: %v", err)
	}

	if job.Name != "my-job" || job.Namespace != "default" {
		t.Errorf("constructLaunchJobFlagsJob() name/namespace = %s/%s, want my-job/default", job.Name, job.Namespace)
	}
	if job.Spec.MinAvailable != 2 {
		t.Errorf("constructLaunchJobFlagsJob() MinAvailable = %d, want 2", job.Spec.MinAvailable)
	}
	if len(job.Spec.Tasks) != 1 || job.Spec.Tasks[0].Replicas != 3 {
		t.Fatalf("constructLaunchJobFlagsJob() unexpected tasks: %+v", job.Spec.Tasks)
	}

	container := job.Spec.Tasks[0].Template.Spec.Containers[0]
	wantCommand := []string{"echo", "hello", "world"}
	if len(container.Command) != len(wantCommand) {
		t.Fatalf("constructLaunchJobFlagsJob() command = %v, want %v", container.Command, wantCommand)
	}
	for i, c := range wantCommand {
		if container.Command[i] != c {
			t.Errorf("constructLaunchJobFlagsJob() command[%d] = %s, want %s", i, container.Command[i], c)
		}
	}
	if container.Resources.Requests.Cpu().String() != "1" || container.Resources.Limits.Cpu().String() != "2" {
		t.Errorf("constructLaunchJobFlagsJob() resources = requests %v limits %v, want 1/2",
			container.Resources.Requests.Cpu(), container.Resources.Limits.Cpu())
	}
}

func TestConstructLaunchJobFlagsJobNoCommand(t *testing.T) {
	flags := &runFlags{Name: "no-cmd-job", Namespace: "default"}

	job, err := constructLaunchJobFlagsJob(flags, v1.ResourceList{}, v1.ResourceList{})
	if err != nil {
		t.Fatalf("constructLaunchJobFlagsJob() unexpected error: %v", err)
	}
	if len(job.Spec.Tasks[0].Template.Spec.Containers[0].Command) != 0 {
		t.Errorf("constructLaunchJobFlagsJob() expected no command, got %v", job.Spec.Tasks[0].Template.Spec.Containers[0].Command)
	}
}

func TestConstructLaunchJobFlagsJobInvalidCommand(t *testing.T) {
	flags := &runFlags{Name: "bad-cmd-job", Namespace: "default", Command: "echo 'unterminated"}

	_, err := constructLaunchJobFlagsJob(flags, v1.ResourceList{}, v1.ResourceList{})
	if err == nil {
		t.Fatal("constructLaunchJobFlagsJob() expected an error for an unterminated quoted command, got nil")
	}
}

func TestSetDefaultArgs(t *testing.T) {
	for _, key := range []string{SchedulerNameEnv, DefaultImageEnv, DefaultJobNamespaceEnv} {
		t.Setenv(key, "")
	}

	t.Run("falls back to hardcoded defaults", func(t *testing.T) {
		launchJobFlags = &runFlags{}
		setDefaultArgs()
		if launchJobFlags.SchedulerName != defaultSchedulerName {
			t.Errorf("SchedulerName = %s, want %s", launchJobFlags.SchedulerName, defaultSchedulerName)
		}
		if launchJobFlags.Image != defaultImage {
			t.Errorf("Image = %s, want %s", launchJobFlags.Image, defaultImage)
		}
		if launchJobFlags.Namespace != defaultJobNamespace {
			t.Errorf("Namespace = %s, want %s", launchJobFlags.Namespace, defaultJobNamespace)
		}
	})

	t.Run("env vars override defaults", func(t *testing.T) {
		t.Setenv(SchedulerNameEnv, "custom-scheduler")
		t.Setenv(DefaultImageEnv, "custom-image")
		t.Setenv(DefaultJobNamespaceEnv, "custom-namespace")

		launchJobFlags = &runFlags{}
		setDefaultArgs()
		if launchJobFlags.SchedulerName != "custom-scheduler" {
			t.Errorf("SchedulerName = %s, want custom-scheduler", launchJobFlags.SchedulerName)
		}
		if launchJobFlags.Image != "custom-image" {
			t.Errorf("Image = %s, want custom-image", launchJobFlags.Image)
		}
		if launchJobFlags.Namespace != "custom-namespace" {
			t.Errorf("Namespace = %s, want custom-namespace", launchJobFlags.Namespace)
		}
	})

	t.Run("explicit flags are not overridden", func(t *testing.T) {
		t.Setenv(SchedulerNameEnv, "env-scheduler")

		launchJobFlags = &runFlags{SchedulerName: "flag-scheduler"}
		setDefaultArgs()
		if launchJobFlags.SchedulerName != "flag-scheduler" {
			t.Errorf("SchedulerName = %s, want flag-scheduler (should not be overridden)", launchJobFlags.SchedulerName)
		}
	})
}
