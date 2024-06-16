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

type getFlags struct {
	util.CommonFlags
	// Name of the job template.
	Name string
	// Namespace of the job template.
	Namespace string
}

var getJobTemplateFlags = &getFlags{}

// InitGetFlags is used to init all flags.
func InitGetFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &getJobTemplateFlags.CommonFlags)
	cmd.Flags().StringVarP(&getJobTemplateFlags.Name, "name", "N", "", "the name of job template")
	cmd.Flags().StringVarP(&getJobTemplateFlags.Namespace, "namespace", "n", "default", "the namespace of job template")
}

// GetJobTemplate gets a job template.
func GetJobTemplate(ctx context.Context) error {
	config, err := util.BuildConfig(getJobTemplateFlags.Master, getJobTemplateFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if getJobTemplateFlags.Name == "" {
		err := fmt.Errorf("name is mandatory to get the particular job template details")
		return err
	}

	jobTemplateClient := versioned.NewForConfigOrDie(config)
	jobTemplate, err := jobTemplateClient.FlowV1alpha1().JobTemplates(getJobTemplateFlags.Namespace).Get(ctx, getJobTemplateFlags.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	PrintJobTemplate(jobTemplate, os.Stdout)

	return nil
}

// PrintJobTemplate prints the job template details.
func PrintJobTemplate(jobTemplate *v1alpha1.JobTemplate, writer io.Writer) {
	maxNameLen := len(Name)
	maxNamespaceLen := len(Namespace)
	if len(jobTemplate.Name) > maxNameLen {
		maxNameLen = len(jobTemplate.Name)
	}
	if len(jobTemplate.Namespace) > maxNamespaceLen {
		maxNamespaceLen = len(jobTemplate.Namespace)
	}

	columnSpacing := 4
	maxNameLen += columnSpacing
	maxNamespaceLen += columnSpacing
	// Find the max length of the name, namespace.
	formatStr := fmt.Sprintf("%%-%ds%%-%ds\n", maxNameLen, maxNamespaceLen)

	// Print the header.
	_, err := fmt.Fprintf(writer, formatStr, Name, Namespace)
	if err != nil {
		fmt.Printf("Failed to print JobTemplate command result: %s.\n", err)
	}
	// Print the separator.
	_, err = fmt.Fprintf(writer, formatStr, jobTemplate.Name, jobTemplate.Namespace)
	if err != nil {
		fmt.Printf("Failed to print JobTemplate command result: %s.\n", err)
	}
}
