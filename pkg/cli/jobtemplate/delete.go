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

	// Name is name of job template
	Name string
	// Namespace is namespace of job template
	Namespace string
	// FilePath is the file path of job template.
	FilePath string
}

var deleteJobTemplateFlags = &deleteFlags{}

// InitDeleteFlags is used to init all flags during job template deleting.
func InitDeleteFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &deleteJobTemplateFlags.CommonFlags)
	cmd.Flags().StringVarP(&deleteJobTemplateFlags.Name, "name", "N", "", "the name of job template")
	cmd.Flags().StringVarP(&deleteJobTemplateFlags.Namespace, "namespace", "n", "default", "the namespace of job template")
	cmd.Flags().StringVarP(&deleteJobTemplateFlags.FilePath, "file", "f", "", "the path to the YAML file containing the job template")
}

// DeleteJobTemplate is used to delete a job template.
func DeleteJobTemplate(ctx context.Context) error {
	config, err := util.BuildConfig(deleteJobTemplateFlags.Master, deleteJobTemplateFlags.Kubeconfig)
	if err != nil {
		return err
	}

	jobTemplateClient := versioned.NewForConfigOrDie(config)
	if err != nil {
		return err
	}

	if deleteJobTemplateFlags.FilePath != "" {
		yamlData, err := os.ReadFile(deleteJobTemplateFlags.FilePath)
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

			jobTemplate := &flowv1alpha1.JobTemplate{}
			if err := yaml.Unmarshal([]byte(doc), jobTemplate); err != nil {
				return err
			}

			if jobTemplate.Namespace == "" {
				jobTemplate.Namespace = "default"
			}

			err := jobTemplateClient.FlowV1alpha1().JobTemplates(jobTemplate.Namespace).Delete(ctx, jobTemplate.Name, metav1.DeleteOptions{})
			if err == nil {
				fmt.Printf("Deleted JobTemplate: %s/%s\n", jobTemplate.Namespace, jobTemplate.Name)
				deletedCount++
			} else {
				fmt.Printf("Failed to delete JobTemplate: %v\n", err)
			}
		}
		return nil
	}

	if deleteJobTemplateFlags.Name == "" {
		return fmt.Errorf("job template name must be specified")
	}

	jobTemplate, err := jobTemplateClient.FlowV1alpha1().JobTemplates(deleteJobTemplateFlags.Namespace).Get(ctx, deleteJobTemplateFlags.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	err = jobTemplateClient.FlowV1alpha1().JobTemplates(jobTemplate.Namespace).Delete(ctx, jobTemplate.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	fmt.Printf("Deleted JobTemplate: %s/%s\n", jobTemplate.Namespace, jobTemplate.Name)

	return nil
}
