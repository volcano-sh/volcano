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
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/cli/util"
)

type deleteFlags struct {
	util.CommonFlags

	// Name is name of jobflow
	Name string
	// Namespace is namespace of jobflow
	Namespace string
	// FilePath is the file path of jobflow
	FilePath string
}

var deleteJobFlowFlags = &deleteFlags{}

// InitDeleteFlags is used to init all flags during jobflow deleting.
func InitDeleteFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &deleteJobFlowFlags.CommonFlags)
	cmd.Flags().StringVarP(&deleteJobFlowFlags.Name, "name", "N", "", "the name of jobflow")
	cmd.Flags().StringVarP(&deleteJobFlowFlags.Namespace, "namespace", "n", "default", "the namespace of jobflow")
	cmd.Flags().StringVarP(&deleteJobFlowFlags.FilePath, "file", "f", "", "the path to the YAML file containing the jobflow")
}

// DeleteJobFlow is used to delete a jobflow.
func DeleteJobFlow(ctx context.Context) error {
	config, err := util.BuildConfig(deleteJobFlowFlags.Master, deleteJobFlowFlags.Kubeconfig)
	if err != nil {
		return err
	}

	jobFlowClient := versioned.NewForConfigOrDie(config)
	if err != nil {
		return err
	}

	if deleteJobFlowFlags.FilePath != "" {
		yamlData, err := os.ReadFile(deleteJobFlowFlags.FilePath)
		if err != nil {
			return err
		}

		yamlDocs := strings.Split(string(yamlData), "---")

		deletedCount := 0
		for _, doc := range yamlDocs {
			doc = strings.TrimSpace(doc)
			if doc == "" {
				continue
			}

			jobFlow := &flowv1alpha1.JobFlow{}
			if err := yaml.Unmarshal([]byte(doc), jobFlow); err != nil {
				return err
			}

			if jobFlow.Namespace == "" {
				jobFlow.Namespace = "default"
			}

			err := jobFlowClient.FlowV1alpha1().JobFlows(jobFlow.Namespace).Delete(ctx, jobFlow.Name, metav1.DeleteOptions{})
			if err == nil {
				fmt.Printf("Deleted JobFlow: %s/%s\n", jobFlow.Namespace, jobFlow.Name)
				deletedCount++
			} else {
				fmt.Printf("Failed to delete JobFlow: %v\n", err)
			}
		}
		return nil
	}

	if deleteJobFlowFlags.Name == "" {
		return fmt.Errorf("jobflow name must be specified")
	}

	jobFlow, err := jobFlowClient.FlowV1alpha1().JobFlows(deleteJobFlowFlags.Namespace).Get(ctx, deleteJobFlowFlags.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	err = jobFlowClient.FlowV1alpha1().JobFlows(jobFlow.Namespace).Delete(ctx, jobFlow.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	fmt.Printf("Deleted JobFlow: %s/%s\n", jobFlow.Namespace, jobFlow.Name)

	return nil
}
