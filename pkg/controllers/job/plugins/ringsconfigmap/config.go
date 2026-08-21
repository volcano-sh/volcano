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

package ringsconfigmap

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"strings"
	"text/template"

	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

// Config holds the configuration for the ringsconfigmap plugin
type Config struct {
	// ConfigMapNameTemplate is the template for ConfigMap name
	// Default: "rings-config-{{.Name}}"
	ConfigMapNameTemplate string

	// VolumeName is the name of the volume to be added to pods
	// Default: "ascend-910-config"
	VolumeName string

	// MountPath is the path where the ConfigMap will be mounted
	// Default: "/user/serverid/devindex/config"
	MountPath string

	// ConfigMapLabels are the labels to be added to the ConfigMap
	// Default: {"ring-controller.atlas": "ascend-910b"}
	ConfigMapLabels map[string]string

	// ConfigMapData is the data to be stored in the ConfigMap
	// Default: {"hccl.json": '{"status":"initializing"}'}
	ConfigMapData map[string]string

	// ConfigMapAnnotations are the annotations to be added to the ConfigMap
	// Default: empty map
	ConfigMapAnnotations map[string]string

	// Compiled template for ConfigMap name (for performance)
	configMapNameTemplate *template.Template
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		ConfigMapNameTemplate: "rings-config-{{.Name}}",
		VolumeName:            "ascend-910-config",
		MountPath:             "/user/serverid/devindex/config",
		ConfigMapLabels: map[string]string{
			"ring-controller.atlas": "ascend-910b",
		},
		ConfigMapData: map[string]string{
			"hccl.json": `{
        "status":"initializing"
    }`,
		},
	}
}

// ParseArguments parses plugin arguments and updates the configuration
func (c *Config) ParseArguments(arguments []string) error {
	flagSet := flag.NewFlagSet("ringsconfigmap", flag.ContinueOnError)

	// Define flags
	flagSet.StringVar(&c.ConfigMapNameTemplate, "configmap-name-template", c.ConfigMapNameTemplate,
		"Template for ConfigMap name (supports Go template syntax, e.g., 'rings-config-{{.Name}}')")
	flagSet.StringVar(&c.VolumeName, "volume-name", c.VolumeName,
		"Name of the volume to be added to pods")
	flagSet.StringVar(&c.MountPath, "mount-path", c.MountPath,
		"Path where the ConfigMap will be mounted")

	// ConfigMap labels, data, and annotations configuration
	var configMapLabelsStr string
	var configMapDataStr string
	var configMapAnnotationsStr string
	flagSet.StringVar(&configMapLabelsStr, "configmap-labels", "",
		"ConfigMap labels in JSON format (e.g., '{\"key1\":\"value1\",\"key2\":\"value2\"}')")
	flagSet.StringVar(&configMapDataStr, "configmap-data", "",
		"ConfigMap data in JSON format (e.g., '{\"file1.json\":\"{\\\"key\\\":\\\"value\\\"}\"}')")
	flagSet.StringVar(&configMapAnnotationsStr, "configmap-annotations", "",
		"ConfigMap annotations in JSON format (e.g., '{\"key1\":\"value1\",\"key2\":\"value2\"}')")

	// Parse arguments
	if err := flagSet.Parse(arguments); err != nil {
		klog.Errorf("Failed to parse ringsconfigmap plugin arguments: %v", err)
		return err
	}

	// Parse ConfigMap labels if provided
	if configMapLabelsStr != "" {
		if err := json.Unmarshal([]byte(configMapLabelsStr), &c.ConfigMapLabels); err != nil {
			return fmt.Errorf("failed to parse configmap-labels: %v", err)
		}
	}

	// Parse ConfigMap data if provided
	if configMapDataStr != "" {
		if err := json.Unmarshal([]byte(configMapDataStr), &c.ConfigMapData); err != nil {
			return fmt.Errorf("failed to parse configmap-data: %v", err)
		}
	}

	// Parse ConfigMap annotations if provided
	if configMapAnnotationsStr != "" {
		if err := json.Unmarshal([]byte(configMapAnnotationsStr), &c.ConfigMapAnnotations); err != nil {
			return fmt.Errorf("failed to parse configmap-annotations: %v", err)
		}
	}

	// Compile template for performance
	if err := c.compileTemplate(); err != nil {
		return err
	}

	// Validate configuration
	if err := c.validate(); err != nil {
		return err
	}

	return nil
}

// validate validates the configuration
func (c *Config) validate() error {
	if c.ConfigMapNameTemplate == "" {
		return fmt.Errorf("configmap-name-template cannot be empty")
	}
	if c.VolumeName == "" {
		return fmt.Errorf("volume-name cannot be empty")
	}
	if c.MountPath == "" {
		return fmt.Errorf("mount-path cannot be empty")
	}
	return nil
}

// compileTemplate compiles the ConfigMap name template for performance
func (c *Config) compileTemplate() error {
	tmpl, err := template.New("configmap-name").Parse(c.ConfigMapNameTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse ConfigMap name template: %v", err)
	}
	c.configMapNameTemplate = tmpl
	return nil
}

// GetConfigMapName generates the ConfigMap name using the compiled template
func (c *Config) GetConfigMapName(job *batch.Job) (string, error) {
	if c.configMapNameTemplate == nil {
		// Fallback to compile template if not already compiled
		if err := c.compileTemplate(); err != nil {
			return "", err
		}
	}

	var buf bytes.Buffer
	err := c.configMapNameTemplate.Execute(&buf, job)
	if err != nil {
		return "", fmt.Errorf("failed to execute ConfigMap name template: %v", err)
	}

	name := strings.TrimSpace(buf.String())
	if name == "" {
		return "", fmt.Errorf("ConfigMap name template resulted in empty string")
	}

	return name, nil
}
