/*
Copyright 2024 The Volcano Authors.

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

package jobtemplate

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/cli/util"
)

const (
	// Name job template name
	Name string = "Name"
	// Namespace job template namespace
	Namespace string = "Namespace"
)

type listFlags struct {
	util.CommonFlags
	// Namespace job template namespace
	Namespace string
}

var listJobTemplateFlags = &listFlags{}

// InitListFlags inits all flags.
func InitListFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &listJobTemplateFlags.CommonFlags)
	cmd.Flags().StringVarP(&listJobTemplateFlags.Namespace, "namespace", "n", "default", "the namespace of job template")
}

// ListJobTemplate lists all job templates.
func ListJobTemplate(ctx context.Context) error {
	config, err := util.BuildConfig(listJobTemplateFlags.Master, listJobTemplateFlags.Kubeconfig)
	if err != nil {
		return err
	}

	jobClient := versioned.NewForConfigOrDie(config)
	jobTemplates, err := jobClient.FlowV1alpha1().JobTemplates(listJobTemplateFlags.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(jobTemplates.Items) == 0 {
		fmt.Printf("No resources found\n")
		return nil
	}
	PrintJobTemplates(jobTemplates, os.Stdout)

	return nil
}

// PrintJobTemplates prints all the job templates.
func PrintJobTemplates(jobTemplates *v1alpha1.JobTemplateList, writer io.Writer) {
	// Calculate the max length of the name, namespace on list.
	maxNameLen, maxNamespaceLen := calculateMaxInfoLength(jobTemplates)
	columnSpacing := 4
	maxNameLen += columnSpacing
	maxNamespaceLen += columnSpacing
	formatStr := fmt.Sprintf("%%-%ds%%-%ds\n", maxNameLen, maxNamespaceLen)
	// Print the header.
	_, err := fmt.Fprintf(writer, formatStr, Name, Namespace)
	if err != nil {
		fmt.Printf("Failed to print JobTemplate command result: %s.\n", err)
	}
	// Print the job templates.
	for _, jobTemplate := range jobTemplates.Items {
		_, err := fmt.Fprintf(writer, formatStr, jobTemplate.Name, jobTemplate.Namespace)
		if err != nil {
			fmt.Printf("Failed to print JobTemplate command result: %s.\n", err)
		}
	}
}

// calculateMaxInfoLength calculates the maximum length of the Name, Namespace fields.
func calculateMaxInfoLength(jobTemplates *v1alpha1.JobTemplateList) (int, int) {
	maxNameLen := len(Name)
	maxNamespaceLen := len(Namespace)

	for _, jobTemplate := range jobTemplates.Items {
		if len(jobTemplate.Name) > maxNameLen {
			maxNameLen = len(jobTemplate.Name)
		}
		if len(jobTemplate.Namespace) > maxNamespaceLen {
			maxNamespaceLen = len(jobTemplate.Namespace)
		}
	}
	return maxNameLen, maxNamespaceLen
}
