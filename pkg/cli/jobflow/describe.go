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

package jobflow

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

	// Name is name of jobflow
	Name string
	// Namespace is namespace of jobflow
	Namespace string
	// Format print format: yaml or json format
	Format string
}

var describeJobFlowFlags = &describeFlags{}

// InitDescribeFlags is used to init all flags.
func InitDescribeFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &describeJobFlowFlags.CommonFlags)
	cmd.Flags().StringVarP(&describeJobFlowFlags.Name, "name", "N", "", "the name of jobflow")
	cmd.Flags().StringVarP(&describeJobFlowFlags.Namespace, "namespace", "n", "default", "the namespace of jobflow")
	cmd.Flags().StringVarP(&describeJobFlowFlags.Format, "format", "o", "yaml", "the format of output")
}

// DescribeJobFlow is used to get the particular jobflow details.
func DescribeJobFlow(ctx context.Context) error {
	config, err := util.BuildConfig(describeJobFlowFlags.Master, describeJobFlowFlags.Kubeconfig)
	if err != nil {
		return err
	}
	jobFlowClient := versioned.NewForConfigOrDie(config)

	// Get jobflow list detail
	if describeJobFlowFlags.Name == "" {
		jobFlows, err := jobFlowClient.FlowV1alpha1().JobFlows(describeJobFlowFlags.Namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for i, jobFlow := range jobFlows.Items {
			// Remove managedFields
			jobFlow.ManagedFields = nil
			PrintJobFlowDetail(&jobFlow, describeJobFlowFlags.Format)
			// Print a separator if it's not the last element
			if len(jobFlows.Items) != 1 && i < len(jobFlows.Items)-1 {
				fmt.Println("---------------------------------")
			}
		}
		// Get jobflow detail
	} else {
		jobFlow, err := jobFlowClient.FlowV1alpha1().JobFlows(describeJobFlowFlags.Namespace).Get(ctx, describeJobFlowFlags.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		// Remove managedFields
		jobFlow.ManagedFields = nil
		// Set APIVersion and Kind if not set
		if jobFlow.APIVersion == "" || jobFlow.Kind == "" {
			jobFlow.APIVersion = v1alpha1.SchemeGroupVersion.String()
			jobFlow.Kind = "JobFlow"
		}
		PrintJobFlowDetail(jobFlow, describeJobFlowFlags.Format)
	}

	return nil
}

// PrintJobFlowDetail print jobflow details
func PrintJobFlowDetail(jobFlow *v1alpha1.JobFlow, format string) {
	switch format {
	case "json":
		printJSON(jobFlow)
	case "yaml":
		printYAML(jobFlow)
	default:
		fmt.Printf("Unsupported format: %s", format)
	}
}

func printJSON(jobFlow *v1alpha1.JobFlow) {
	b, err := json.MarshalIndent(jobFlow, "", "  ")
	if err != nil {
		fmt.Printf("Error marshaling JSON: %v\n", err)
		return
	}
	os.Stdout.Write(b)
	fmt.Println("")
}

func printYAML(jobFlow *v1alpha1.JobFlow) {
	b, err := yaml.Marshal(jobFlow)
	if err != nil {
		fmt.Printf("Error marshaling YAML: %v\n", err)
		return
	}
	os.Stdout.Write(b)
}
