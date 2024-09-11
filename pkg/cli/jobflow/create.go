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

type createFlags struct {
	util.CommonFlags
	// FilePath is the file path of jobflow
	FilePath string
}

var createJobFlowFlags = &createFlags{}

// InitCreateFlags is used to init all flags during queue creating.
func InitCreateFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &createJobFlowFlags.CommonFlags)
	cmd.Flags().StringVarP(&createJobFlowFlags.FilePath, "file", "f", "", "the path to the YAML file containing the jobflow")
}

// CreateJobFlow create a jobflow.
func CreateJobFlow(ctx context.Context) error {
	config, err := util.BuildConfig(createJobFlowFlags.Master, createJobFlowFlags.Kubeconfig)
	if err != nil {
		return err
	}

	// Read YAML data from a file.
	yamlData, err := os.ReadFile(createJobFlowFlags.FilePath)
	if err != nil {
		return err
	}
	// Split YAML data into individual documents.
	yamlDocs := strings.Split(string(yamlData), "---")

	jobFlowClient := versioned.NewForConfigOrDie(config)
	createdCount := 0
	for _, doc := range yamlDocs {
		// Skip empty documents or documents with only whitespace.
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		// Parse each YAML document into a JobFlow object.
		obj := &flowv1alpha1.JobFlow{}
		if err = yaml.Unmarshal([]byte(doc), obj); err != nil {
			return err
		}
		// Set the namespace if it's not specified.
		if obj.Namespace == "" {
			obj.Namespace = "default"
		}

		_, err = jobFlowClient.FlowV1alpha1().JobFlows(obj.Namespace).Create(ctx, obj, metav1.CreateOptions{})
		if err == nil {
			fmt.Printf("Created JobFlow: %s/%s\n", obj.Namespace, obj.Name)
			createdCount++
		} else {
			fmt.Printf("Failed to create JobFlow: %v\n", err)
		}
	}
	return nil
}
