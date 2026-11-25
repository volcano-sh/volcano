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
	"bytes"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/cli/util"
)

type createFlags struct {
	util.CommonFlags
	// FilePath is the file path of job template.
	FilePath string
}

var createJobTemplateFlags = &createFlags{}

// InitCreateFlags is used to init all flags during queue creating.
func InitCreateFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &createJobTemplateFlags.CommonFlags)
	cmd.Flags().StringVarP(&createJobTemplateFlags.FilePath, "file", "f", "", "the path to the YAML file containing the job template")
}

// CreateJobTemplate create a job template.
func CreateJobTemplate(ctx context.Context) error {
	config, err := util.BuildConfig(createJobTemplateFlags.Master, createJobTemplateFlags.Kubeconfig)
	if err != nil {
		return err
	}

	// Read YAML data from a file.
	yamlData, err := os.ReadFile(createJobTemplateFlags.FilePath)
	if err != nil {
		return err
	}
	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(yamlData), 4096)

	jobTemplateClient := versioned.NewForConfigOrDie(config)
	var errs []error
	for {
		var obj *flowv1alpha1.JobTemplate
		if err := decoder.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if obj == nil {
			continue
		}
		// Set the namespace if it's not specified.
		if obj.Namespace == "" {
			obj.Namespace = "default"
		}

		_, err = jobTemplateClient.FlowV1alpha1().JobTemplates(obj.Namespace).Create(ctx, obj, metav1.CreateOptions{})
		if err != nil {
			errs = append(errs, fmt.Errorf("create JobTemplate %s/%s: %v", obj.Namespace, obj.Name, err))
			continue
		}
		fmt.Printf("Created JobTemplate: %s/%s\n", obj.Namespace, obj.Name)
	}

	return utilerrors.NewAggregate(errs)
}
