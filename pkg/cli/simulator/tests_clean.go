/*
Copyright 2025 The Volcano Authors.

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

package simulator

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/cli/util"
)

type cleanTestsFlags struct {
	util.CommonFlags

	Config string
}

var cleanTestsFlag = &cleanTestsFlags{}

// InitCleanTestsFlags is used to init all flags during clean tests.
func InitCleanTestsFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &cleanTestsFlag.CommonFlags)
}

// CleanTests is used to clean tests.
func CleanTests(ctx context.Context) error {
	// 1. Create Kubernetes clientset
	clientset, err := createKubeClient(cleanTestsFlag.CommonFlags)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %v", err)
	}

	// 2. Get all namespaces
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list namespaces: %v", err)
	}

	// 3. Iterate through all namespaces and delete pods with the specified label.
	for _, namespace := range namespaces.Items {
		err := deletePodsWithLabel(ctx, clientset, namespace.Name, "simulator.volcano.sh/usage", "simulator")
		if err != nil {
			klog.Errorf("Warning: Failed to clean pods in namespace %s: %v", namespace.Name, err)
			// Continue to clean up other namespaces without immediately returning an error
		}
	}
	return nil
}

// deletePodsWithLabel deletes all pods in a specific namespace that match the given label key-value pair
func deletePodsWithLabel(ctx context.Context, clientset kubernetes.Interface, namespace, labelKey, labelValue string) error {
	// Create label selector string
	labelSelector := fmt.Sprintf("%s=%s", labelKey, labelValue)

	// List all pods matching the label selector in the namespace
	pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list pods in namespace %s: %v", namespace, err)
	}

	// If no pods found with the specified label, print message and return
	if len(pods.Items) == 0 {
		return nil
	}

	// Set delete policy to foreground to ensure proper cleanup of dependent resources
	deletePolicy := metav1.DeletePropagationForeground
	deleteOptions := metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}

	// Delete each matching pod individually
	for _, pod := range pods.Items {
		err := clientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, deleteOptions)
		if err != nil {
			klog.Errorf("Warning: Failed to delete pod %s in namespace %s: %v", pod.Name, namespace, err)
			// Continue deleting other pods instead of immediately returning an error
		} else {
			klog.Infof("Deleted pod: %s in namespace %s", pod.Name, namespace)
		}
	}

	return nil
}
