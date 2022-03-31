/*
Copyright 2019 The Volcano Authors.

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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	coreV1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/cli/util"
)

type viewFlags struct {
	commonFlags

	Namespace string
	JobName   string
}

// level of print indent.
const (
	Level0 = iota
	Level1
	Level2
)

var viewJobFlags = &viewFlags{}

// InitViewFlags init the view command flags.
func InitViewFlags(cmd *cobra.Command) {
	initFlags(cmd, &viewJobFlags.commonFlags)

	cmd.Flags().StringVarP(&viewJobFlags.Namespace, "namespace", "n", "default", "the namespace of job")
	cmd.Flags().StringVarP(&viewJobFlags.JobName, "name", "N", "", "the name of job")
}

// ViewJob gives full details of the job.
func ViewJob() error {
	config, err := util.BuildConfig(viewJobFlags.Master, viewJobFlags.Kubeconfig)
	if err != nil {
		return err
	}
	if viewJobFlags.JobName == "" {
		err := fmt.Errorf("job name (specified by --name or -N) is mandatory to view a particular job")
		return err
	}

	jobClient := versioned.NewForConfigOrDie(config)
	job, err := jobClient.BatchV1alpha1().Jobs(viewJobFlags.Namespace).Get(context.TODO(), viewJobFlags.JobName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if job == nil {
		fmt.Printf("No resources found\n")
		return nil
	}
	PrintJobInfo(job, os.Stdout)
	PrintEvents(GetEvents(config, job), os.Stdout)
	return nil
}

// PrintJobInfo print the job detailed info into writer.
func PrintJobInfo(job *v1alpha1.Job, writer io.Writer) {
	WriteLine(writer, Level0, "Name:       \t%s\n", job.Name)
	WriteLine(writer, Level0, "Namespace:  \t%s\n", job.Namespace)
	if len(job.Labels) > 0 {
		label, _ := json.Marshal(job.Labels)
		WriteLine(writer, Level0, "Labels:     \t%s\n", string(label))
	} else {
		WriteLine(writer, Level0, "Labels:     \t<none>\n")
	}
	if len(job.Annotations) > 0 {
		annotation, _ := json.Marshal(job.Annotations)
		WriteLine(writer, Level0, "Annotations:\t%s\n", string(annotation))
	} else {
		WriteLine(writer, Level0, "Annotations:\t<none>\n")
	}
	WriteLine(writer, Level0, "API Version:\t%s\n", job.APIVersion)
	WriteLine(writer, Level0, "Kind:       \t%s\n", job.Kind)

	WriteLine(writer, Level0, "Metadata:\n")
	WriteLine(writer, Level1, "Creation Timestamp:\t%s\n", job.CreationTimestamp)
	WriteLine(writer, Level1, "Generate Name:     \t%s\n", job.GenerateName)
	WriteLine(writer, Level1, "Generation:        \t%d\n", job.Generation)
	WriteLine(writer, Level1, "Resource Version:  \t%s\n", job.ResourceVersion)
	WriteLine(writer, Level1, "Self Link:         \t%s\n", job.SelfLink)
	WriteLine(writer, Level1, "UID:               \t%s\n", job.UID)

	WriteLine(writer, Level0, "Spec:\n")
	WriteLine(writer, Level1, "Min Available:     \t%d\n", job.Spec.MinAvailable)
	WriteLine(writer, Level1, "Plugins:\n")
	WriteLine(writer, Level2, "Env:\t%v\n", job.Spec.Plugins["env"])
	WriteLine(writer, Level2, "Ssh:\t%v\n", job.Spec.Plugins["ssh"])
	WriteLine(writer, Level1, "Scheduler Name:    \t%s\n", job.Spec.SchedulerName)
	WriteLine(writer, Level1, "Tasks:\n")
	for i := 0; i < len(job.Spec.Tasks); i++ {
		WriteLine(writer, Level2, "Name:\t%s\n", job.Spec.Tasks[i].Name)
		WriteLine(writer, Level2, "Replicas:\t%d\n", job.Spec.Tasks[i].Replicas)
		WriteLine(writer, Level2, "Template:\n")
		WriteLine(writer, Level2+1, "Metadata:\n")
		WriteLine(writer, Level2+2, "Annotations:\n")
		WriteLine(writer, Level2+3, "Cri . Cci . Io / Container - Type:          \t%s\n", job.Spec.Tasks[i].Template.ObjectMeta.Annotations["cri.cci.io/container-type"])
		WriteLine(writer, Level2+3, "Kubernetes . Io / Availablezone:            \t%s\n", job.Spec.Tasks[i].Template.ObjectMeta.Annotations["kubernetes.io/availablezone"])
		WriteLine(writer, Level2+3, "Network . Alpha . Kubernetes . Io / Network:\t%s\n", job.Spec.Tasks[i].Template.ObjectMeta.Annotations["network.alpha.kubernetes.io/network"])
		WriteLine(writer, Level2+2, "Creation Timestamp:\t%s\n", job.Spec.Tasks[i].Template.ObjectMeta.CreationTimestamp)

		WriteLine(writer, Level2+1, "Spec:\n")
		WriteLine(writer, Level2+2, "Containers:\n")
		for j := 0; j < len(job.Spec.Tasks[i].Template.Spec.Containers); j++ {
			WriteLine(writer, Level2+3, "Command:\n")
			for k := 0; k < len(job.Spec.Tasks[i].Template.Spec.Containers[j].Command); k++ {
				WriteLine(writer, Level2+4, "%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Command[k])
			}
			WriteLine(writer, Level2+3, "Image:\t%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Image)
			WriteLine(writer, Level2+3, "Name: \t%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Name)
			WriteLine(writer, Level2+3, "Ports:\n")
			for k := 0; k < len(job.Spec.Tasks[i].Template.Spec.Containers[j].Ports); k++ {
				WriteLine(writer, Level2+4, "Container Port:\t%d\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Ports[k].ContainerPort)
				WriteLine(writer, Level2+4, "Name:          \t%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Ports[k].Name)
			}
			WriteLine(writer, Level2+3, "Resources:\n")
			WriteLine(writer, Level2+4, "Limits:\n")
			WriteLine(writer, Level2+5, "Cpu:   \t%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Resources.Limits.Cpu())
			WriteLine(writer, Level2+5, "Memory:\t%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Resources.Limits.Memory())
			WriteLine(writer, Level2+4, "Requests:\n")
			WriteLine(writer, Level2+5, "Cpu:   \t%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Resources.Requests.Cpu())
			WriteLine(writer, Level2+5, "Memory:\t%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].Resources.Requests.Memory())
			WriteLine(writer, Level2+4, "Working Dir:\t%s\n", job.Spec.Tasks[i].Template.Spec.Containers[j].WorkingDir)
		}
		WriteLine(writer, Level2+2, "Image Pull Secrets:\n")
		for j := 0; j < len(job.Spec.Tasks[i].Template.Spec.ImagePullSecrets); j++ {
			WriteLine(writer, Level2+3, "Name:     \t%s\n", job.Spec.Tasks[i].Template.Spec.ImagePullSecrets[j].Name)
		}
		WriteLine(writer, Level2+2, "Restart Policy:   \t%s\n", job.Spec.Tasks[i].Template.Spec.RestartPolicy)
	}

	WriteLine(writer, Level0, "Status:\n")
	if job.Status.Succeeded > 0 {
		WriteLine(writer, Level1, "Succeeded:    \t%d\n", job.Status.Succeeded)
	}
	if job.Status.Pending > 0 {
		WriteLine(writer, Level1, "Pending:      \t%d\n", job.Status.Pending)
	}
	if job.Status.Running > 0 {
		WriteLine(writer, Level1, "Running:      \t%d\n", job.Status.Running)
	}
	if job.Status.Failed > 0 {
		WriteLine(writer, Level1, "Failed:       \t%d\n", job.Status.Failed)
	}
	if job.Status.Terminating > 0 {
		WriteLine(writer, Level1, "Terminating:  \t%d\n", job.Status.Terminating)
	}
	if job.Status.Unknown > 0 {
		WriteLine(writer, Level1, "Unknown:      \t%d\n", job.Status.Unknown)
	}
	if job.Status.RetryCount > 0 {
		WriteLine(writer, Level1, "RetryCount:   \t%d\n", job.Status.RetryCount)
	}
	if job.Status.MinAvailable > 0 {
		WriteLine(writer, Level1, "Min Available:\t%d\n", job.Status.MinAvailable)
	}
	if job.Status.Version > 0 {
		WriteLine(writer, Level1, "Version:      \t%d\n", job.Status.Version)
	}

	WriteLine(writer, Level1, "State:\n")
	WriteLine(writer, Level2, "Phase:\t%s\n", job.Status.State.Phase)
	if len(job.Status.ControlledResources) > 0 {
		WriteLine(writer, Level1, "Controlled Resources:\n")
		for key, value := range job.Status.ControlledResources {
			WriteLine(writer, Level2, "%s: \t%s\n", key, value)
		}
	}
	if len(job.Status.Conditions) > 0 {
		WriteLine(writer, Level1, "Conditions:\n    Status\tTransitionTime\n")
		for _, c := range job.Status.Conditions {
			WriteLine(writer, Level2, "%v \t%v \n",
				c.Status,
				c.LastTransitionTime)
		}
	}
}

// PrintEvents print event info to writer.
func PrintEvents(events []coreV1.Event, writer io.Writer) {
	if len(events) > 0 {
		WriteLine(writer, Level0, "%s:\n%-15s\t%-40s\t%-30s\t%-40s\t%s\n", "Events", "Type", "Reason", "Age", "Form", "Message")
		WriteLine(writer, Level0, "%-15s\t%-40s\t%-30s\t%-40s\t%s\n", "-------", "-------", "-------", "-------", "-------")
		for _, e := range events {
			var interval string
			if e.Count > 1 {
				interval = fmt.Sprintf("%s (x%d over %s)", translateTimestampSince(e.LastTimestamp), e.Count, translateTimestampSince(e.FirstTimestamp))
			} else {
				interval = translateTimestampSince(e.FirstTimestamp)
			}
			EventSourceString := []string{e.Source.Component}
			if len(e.Source.Host) > 0 {
				EventSourceString = append(EventSourceString, e.Source.Host)
			}
			WriteLine(writer, Level0, "%-15v\t%-40v\t%-30s\t%-40s\t%v\n",
				e.Type,
				e.Reason,
				interval,
				strings.Join(EventSourceString, ", "),
				strings.TrimSpace(e.Message),
			)
		}
	} else {
		WriteLine(writer, Level0, "Events: \t<none>\n")
	}
}

// GetEvents get the job event by config.
func GetEvents(config *rest.Config, job *v1alpha1.Job) []coreV1.Event {
	kubernetes, err := kubernetes.NewForConfig(config)
	if err != nil {
		fmt.Printf("%v\n", err)
		return nil
	}
	events, _ := kubernetes.CoreV1().Events(viewJobFlags.Namespace).List(context.TODO(), metav1.ListOptions{})
	var jobEvents []coreV1.Event
	for _, v := range events.Items {
		if strings.HasPrefix(v.ObjectMeta.Name, job.Name+".") {
			jobEvents = append(jobEvents, v)
		}
	}
	return jobEvents
}

// WriteLine write lines with specified indent.
func WriteLine(writer io.Writer, spaces int, content string, params ...interface{}) {
	prefix := ""
	for i := 0; i < spaces; i++ {
		prefix += "  "
	}
	fmt.Fprintf(writer, prefix+content, params...)
}
