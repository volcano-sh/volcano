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
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	"volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/cli/util"
)

type describeFlags struct {
	util.CommonFlags

	// Name is name of job template
	Name string
	// Namespace is namespace of job template
	Namespace string
	// Format print format: yaml or json format
	Format string
}

var describeJobTemplateFlags = &describeFlags{}

// InitDescribeFlags is used to init all flags.
func InitDescribeFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &describeJobTemplateFlags.CommonFlags)
	cmd.Flags().StringVarP(&describeJobTemplateFlags.Name, "name", "N", "", "the name of job template")
	cmd.Flags().StringVarP(&describeJobTemplateFlags.Namespace, "namespace", "n", "default", "the namespace of job template")
	cmd.Flags().StringVarP(&describeJobTemplateFlags.Format, "format", "o", "yaml", "the format of output")
}

// DescribeJobTemplate is used to get the particular job template details.
func DescribeJobTemplate(ctx context.Context) error {
	if err := checkFormat(describeJobTemplateFlags.Format); err != nil {
		return err
	}

	config, err := util.BuildConfig(describeJobTemplateFlags.Master, describeJobTemplateFlags.Kubeconfig)
	if err != nil {
		return err
	}
	jobTemplateClient := versioned.NewForConfigOrDie(config)

	// Get job template list detail
	if describeJobTemplateFlags.Name == "" {
		jobTemplates, err := jobTemplateClient.FlowV1alpha1().JobTemplates(describeJobTemplateFlags.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i, jobTemplate := range jobTemplates.Items {
			// Remove managedFields
			jobTemplate.ManagedFields = nil
			if err := PrintJobTemplateDetail(&jobTemplate, describeJobTemplateFlags.Format); err != nil {
				return err
			}
			// Print space if it's not the last element
			if len(jobTemplates.Items) != 1 && i < len(jobTemplates.Items)-1 {
				fmt.Printf("\n\n")
			}
		}
		// Get job template detail
	} else {
		jobTemplate, err := jobTemplateClient.FlowV1alpha1().JobTemplates(describeJobTemplateFlags.Namespace).Get(ctx, describeJobTemplateFlags.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		// Remove managedFields
		jobTemplate.ManagedFields = nil
		// Set APIVersion and Kind if not set
		if jobTemplate.APIVersion == "" || jobTemplate.Kind == "" {
			jobTemplate.APIVersion = v1alpha1.SchemeGroupVersion.String()
			jobTemplate.Kind = "JobTemplate"
		}
		return PrintJobTemplateDetail(jobTemplate, describeJobTemplateFlags.Format)
	}

	return nil
}

// PrintJobTemplateDetail print job template details
func PrintJobTemplateDetail(jobTemplate *v1alpha1.JobTemplate, format string) error {
	switch format {
	case "json":
		return printJSON(jobTemplate)
	case "yaml":
		return printYAML(jobTemplate)
	default:
		return checkFormat(format)
	}
}

// checkFormat checks that the output format is one the printer supports.
func checkFormat(format string) error {
	if format != "json" && format != "yaml" {
		return fmt.Errorf("format %s invalid, valid formats are json and yaml", format)
	}

	return nil
}

func printJSON(jobTemplate *v1alpha1.JobTemplate) error {
	b, err := json.MarshalIndent(jobTemplate, "", "  ")
	if err != nil {
		return err
	}
	os.Stdout.Write(b)
	fmt.Println("")

	return nil
}

func printYAML(jobTemplate *v1alpha1.JobTemplate) error {
	b, err := yaml.Marshal(jobTemplate)
	if err != nil {
		return err
	}
	os.Stdout.Write(b)

	return nil
}
