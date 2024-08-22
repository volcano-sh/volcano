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
	"time"

	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/duration"

	"volcano.sh/apis/pkg/apis/flow/v1alpha1"
	"volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/cli/util"
)

const (
	// Name jobflow name
	Name string = "Name"
	// Namespace jobflow namespace
	Namespace string = "Namespace"
	// Phase jobflow phase
	Phase string = "Phase"
	// Age jobflow age
	Age string = "Age"
)

type listFlags struct {
	util.CommonFlags
	// Namespace jobflow namespace
	Namespace string
	// AllNamespace all namespace flag
	AllNamespace bool
}

var listJobFlowFlags = &listFlags{}

// InitListFlags inits all flags.
func InitListFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &listJobFlowFlags.CommonFlags)
	cmd.Flags().StringVarP(&listJobFlowFlags.Namespace, "namespace", "n", "default", "the namespace of jobflow")
	cmd.Flags().BoolVarP(&listJobFlowFlags.AllNamespace, "all-namespaces", "", false, "list jobflows in all namespaces")
}

// ListJobFlow lists all jobflow.
func ListJobFlow(ctx context.Context) error {
	config, err := util.BuildConfig(listJobFlowFlags.Master, listJobFlowFlags.Kubeconfig)
	if err != nil {
		return err
	}

	if listJobFlowFlags.AllNamespace {
		listJobFlowFlags.Namespace = ""
	}

	jobClient := versioned.NewForConfigOrDie(config)
	jobFlows, err := jobClient.FlowV1alpha1().JobFlows(listJobFlowFlags.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	if len(jobFlows.Items) == 0 {
		fmt.Printf("No resources found\n")
		return nil
	}
	PrintJobFlows(jobFlows, os.Stdout)

	return nil
}

// PrintJobFlows prints all the jobflows.
func PrintJobFlows(jobFlows *v1alpha1.JobFlowList, writer io.Writer) {
	// Calculate the max length of the name, namespace phase age  on list.
	maxNameLen, maxNamespaceLen, maxPhaseLen, maxAgeLen := calculateMaxInfoLength(jobFlows)
	columnSpacing := 4
	maxNameLen += columnSpacing
	maxNamespaceLen += columnSpacing
	maxPhaseLen += columnSpacing
	maxAgeLen += columnSpacing
	formatStr := fmt.Sprintf("%%-%ds%%-%ds%%-%ds%%-%ds\n", maxNameLen, maxNamespaceLen, maxPhaseLen, maxAgeLen)
	// Print the header.
	_, err := fmt.Fprintf(writer, formatStr, Name, Namespace, Phase, Age)
	if err != nil {
		fmt.Printf("Failed to print JobFlow command result: %s.\n", err)
	}
	// Print the jobflows.
	for _, jobFlow := range jobFlows.Items {
		_, err := fmt.Fprintf(writer, formatStr, jobFlow.Name, jobFlow.Namespace, jobFlow.Status.State.Phase, translateTimestampSince(jobFlow.CreationTimestamp))
		if err != nil {
			fmt.Printf("Failed to print JobFlow command result: %s.\n", err)
		}
	}
}

// calculateMaxInfoLength calculates the maximum length of the Name, Namespace Phase fields.
func calculateMaxInfoLength(jobFlows *v1alpha1.JobFlowList) (int, int, int, int) {
	maxNameLen := len(Name)
	maxNamespaceLen := len(Namespace)
	maxStatusLen := len(Phase)
	maxAgeLen := len(Age)
	for _, jobFlow := range jobFlows.Items {
		if len(jobFlow.Name) > maxNameLen {
			maxNameLen = len(jobFlow.Name)
		}
		if len(jobFlow.Namespace) > maxNamespaceLen {
			maxNamespaceLen = len(jobFlow.Namespace)
		}
		if len(jobFlow.Status.State.Phase) > maxStatusLen {
			maxStatusLen = len(jobFlow.Status.State.Phase)
		}
		ageLen := translateTimestampSince(jobFlow.CreationTimestamp)
		if len(ageLen) > maxAgeLen {
			maxAgeLen = len(ageLen)
		}
	}
	return maxNameLen, maxNamespaceLen, maxStatusLen, maxAgeLen
}

// translateTimestampSince translates a timestamp into a human-readable string using time.Since.
func translateTimestampSince(timestamp metav1.Time) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}
	return duration.HumanDuration(time.Since(timestamp.Time))
}
