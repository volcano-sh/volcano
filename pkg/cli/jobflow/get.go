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
	// Name of the jobflow
	Name string
	// Namespace of the jobflow
	Namespace string
}

var getJobFlowFlags = &getFlags{}

// InitGetFlags is used to init all flags.
func InitGetFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &getJobFlowFlags.CommonFlags)
	cmd.Flags().StringVarP(&getJobFlowFlags.Name, "name", "N", "", "the name of jobflow")
	cmd.Flags().StringVarP(&getJobFlowFlags.Namespace, "namespace", "n", "default", "the namespace of jobflow")
}

// GetJobFlow gets a jobflow.
func GetJobFlow(ctx context.Context) error {
	config, err := util.BuildConfig(getJobFlowFlags.Master, getJobFlowFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if getJobFlowFlags.Name == "" {
		err := fmt.Errorf("name is mandatory to get the particular jobflow details")
		return err
	}

	jobFlowClient := versioned.NewForConfigOrDie(config)
	jobFlow, err := jobFlowClient.FlowV1alpha1().JobFlows(getJobFlowFlags.Namespace).Get(ctx, getJobFlowFlags.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	PrintJobFlow(jobFlow, os.Stdout)

	return nil
}

// PrintJobFlow prints the jobflow details.
func PrintJobFlow(jobFlow *v1alpha1.JobFlow, writer io.Writer) {
	maxNameLen := len(Name)
	maxNamespaceLen := len(Namespace)
	maxPhaseLen := len(Phase)
	maxAgeLen := len(Age)
	if len(jobFlow.Name) > maxNameLen {
		maxNameLen = len(jobFlow.Name)
	}
	if len(jobFlow.Namespace) > maxNamespaceLen {
		maxNamespaceLen = len(jobFlow.Namespace)
	}
	if len(jobFlow.Status.State.Phase) > maxPhaseLen {
		maxPhaseLen = len(jobFlow.Status.State.Phase)
	}
	age := translateTimestampSince(jobFlow.CreationTimestamp)
	if len(age) > maxAgeLen {
		maxAgeLen = len(age)
	}

	columnSpacing := 4
	maxNameLen += columnSpacing
	maxNamespaceLen += columnSpacing
	maxPhaseLen += columnSpacing
	maxAgeLen += columnSpacing
	// Find the max length of the name, namespace.
	formatStr := fmt.Sprintf("%%-%ds%%-%ds%%-%ds%%-%ds\n", maxNameLen, maxNamespaceLen, maxPhaseLen, maxAgeLen)

	// Print the header.
	_, err := fmt.Fprintf(writer, formatStr, Name, Namespace, Phase, Age)
	if err != nil {
		fmt.Printf("Failed to print JobFlow command result: %s.\n", err)
	}
	// Print the separator.
	_, err = fmt.Fprintf(writer, formatStr, jobFlow.Name, jobFlow.Namespace, jobFlow.Status.State.Phase, age)
	if err != nil {
		fmt.Printf("Failed to print JobFlow command result: %s.\n", err)
	}
}
