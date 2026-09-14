/*
Copyright 2021 The Volcano Authors.

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

package util

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
)

const (
	SchedulerShardingModeFlag  = "--scheduler-sharding-mode"
	SchedulerShardingNameFlag  = "--scheduler-sharding-name"
)

// findVolcanoSchedulerDeployment searches all namespaces for a Deployment running the Volcano scheduler.
// It inspects container names, args, and commands for indicators that identify the Volcano scheduler.
// Returns the first matching Deployment and its namespace.
func findVolcanoSchedulerDeployment(client clientset.Interface) (*appsv1.Deployment, string, error) {
	// List deployments across all namespaces
	deployments, err := client.AppsV1().Deployments(corev1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("failed to list deployments across namespaces: %w", err)
	}

	var candidates []string // For debugging: list all candidate deployments found

	// Check each deployment for indicators that it runs the Volcano scheduler
	for i := range deployments.Items {
		dep := &deployments.Items[i]

		// Check labels first (fastest path)
		if isSchedulerByLabels(dep) {
			return dep, dep.Namespace, nil
		}

		// If labels don't match, check container specifications
		if isSchedulerByContainers(dep) {
			return dep, dep.Namespace, nil
		}

		// If matches any criterion, add to candidates for debugging
		if isSchedulerByLabels(dep) || isSchedulerByContainers(dep) {
			candidates = append(candidates, fmt.Sprintf("%s/%s", dep.Namespace, dep.Name))
		}
	}

	// No matches found - return detailed error with candidates
	var candidatesStr string
	if len(candidates) > 0 {
		candidatesStr = fmt.Sprintf("\nCandidate deployments found:\n  %s", strings.Join(candidates, "\n  "))
	}

	return nil, "", fmt.Errorf(
		"no volcano scheduler deployment found after inspecting all %d deployments across all namespaces.\n"+
			"searched for:\n"+
			"  - labels: app=volcano-scheduler or app.kubernetes.io/name=volcano\n"+
			"  - container names containing 'scheduler'\n"+
			"  - container args/commands containing 'volcano-scheduler' or '--scheduler-name=volcano'\n"+
			"ensure Volcano is deployed%s",
		len(deployments.Items),
		candidatesStr,
	)
}

// isSchedulerByLabels checks if a Deployment has labels indicating it's a Volcano scheduler
func isSchedulerByLabels(dep *appsv1.Deployment) bool {
	labels := dep.Labels
	if labels == nil {
		return false
	}

	// Match label: app=volcano-scheduler
	if labels["app"] == "volcano-scheduler" {
		return true
	}

	// Match label: app.kubernetes.io/name=volcano
	if labels["app.kubernetes.io/name"] == "volcano" {
		return true
	}

	return false
}

// isSchedulerByContainers checks if a Deployment's containers indicate it runs the Volcano scheduler
func isSchedulerByContainers(dep *appsv1.Deployment) bool {
	containers := dep.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return false
	}

	for _, container := range containers {
		// Check if container name contains "scheduler"
		if strings.Contains(strings.ToLower(container.Name), "scheduler") {
			return true
		}

		// Check container image for volcano indicators
		if strings.Contains(strings.ToLower(container.Image), "volcano-scheduler") ||
			strings.Contains(strings.ToLower(container.Image), "scheduler") && strings.Contains(strings.ToLower(container.Image), "volcano") {
			return true
		}

		// Check args for volcano-scheduler indicators
		for _, arg := range container.Args {
			arg_lower := strings.ToLower(arg)
			if strings.Contains(arg_lower, "volcano-scheduler") ||
				strings.Contains(arg_lower, "--scheduler-name=volcano") ||
				strings.HasPrefix(arg_lower, "--scheduler-name") && strings.Contains(arg_lower, "volcano") {
				return true
			}
		}

		// Check command for volcano-scheduler indicators
		for _, cmd := range container.Command {
			cmd_lower := strings.ToLower(cmd)
			if strings.Contains(cmd_lower, "volcano-scheduler") ||
				strings.Contains(cmd_lower, "scheduler") && strings.Contains(cmd_lower, "volcano") {
				return true
			}
		}
	}

	return false
}

// PatchVolcanoSchedulerSharding patches the Volcano scheduler Deployment with sharding flags.
//
// Parameters:
//   - mode: sharding mode value (e.g., "none", "soft", "hard")
//   - name: optional scheduler shard name (can be empty string)
//
// This function:
// 1. Searches all namespaces for a Deployment with Volcano scheduler labels
// 2. Updates the container args to include the sharding mode flag
// 3. Includes the sharding name flag if name is provided
// 4. Removes old sharding flags if present
// 5. Applies the patch to the Deployment
// 6. Waits for the rollout to complete
func PatchVolcanoSchedulerSharding(mode, name string) error {
	if mode == "" {
		return fmt.Errorf("scheduler sharding mode cannot be empty")
	}

	// Find the Volcano scheduler Deployment by searching all namespaces
	deployment, namespace, err := findVolcanoSchedulerDeployment(KubeClient)
	if err != nil {
		return fmt.Errorf("failed to find volcano scheduler deployment: %w", err)
	}

	// Find the scheduler container
	containerIdx := -1
	for i, container := range deployment.Spec.Template.Spec.Containers {
		// Look for container that contains "scheduler" in name
		if strings.Contains(strings.ToLower(container.Name), "scheduler") {
			containerIdx = i
			break
		}
	}

	if containerIdx < 0 {
		return fmt.Errorf("no scheduler container found in deployment %s/%s", namespace, deployment.Name)
	}

	container := &deployment.Spec.Template.Spec.Containers[containerIdx]

	// Remove old sharding flags and add new ones
	newArgs := []string{}
	for _, arg := range container.Args {
		// Skip existing sharding flags
		if strings.HasPrefix(arg, SchedulerShardingModeFlag) ||
			strings.HasPrefix(arg, SchedulerShardingNameFlag) {
			continue
		}
		newArgs = append(newArgs, arg)
	}

	// Add new sharding mode flag
	newArgs = append(newArgs, fmt.Sprintf("%s=%s", SchedulerShardingModeFlag, mode))

	// Add sharding name flag if provided
	if name != "" {
		newArgs = append(newArgs, fmt.Sprintf("%s=%s", SchedulerShardingNameFlag, name))
	}

	container.Args = newArgs

	// Update the deployment spec directly (Merge strategy will only update the changed container)
	deployment.Spec.Template.Spec.Containers[containerIdx].Args = newArgs

	// Use Update() API which handles the full deployment object correctly
	_, err = KubeClient.AppsV1().Deployments(namespace).Update(
		context.TODO(),
		deployment,
		metav1.UpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to update deployment %s/%s: %w", namespace, deployment.Name, err)
	}

	// Wait for rollout to complete
	err = WaitForDeploymentRollout(KubeClient, namespace, deployment.Name, TenMinute)
	if err != nil {
		return fmt.Errorf("failed waiting for deployment %s/%s rollout: %w", namespace, deployment.Name, err)
	}

	return nil
}

// WaitForDeploymentRollout waits for a Deployment to complete its rollout.
// It checks that the Deployment has the desired number of ready replicas.
func WaitForDeploymentRollout(client clientset.Interface, namespace, deploymentName string, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(
		context.TODO(),
		time.Second,
		timeout,
		false,
		func(ctx context.Context) (bool, error) {
			deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
			if err != nil {
				return false, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, deploymentName, err)
			}

			// Check if all desired replicas are ready
			if deployment.Status.UpdatedReplicas == *deployment.Spec.Replicas &&
				deployment.Status.Replicas == *deployment.Spec.Replicas &&
				deployment.Status.AvailableReplicas == *deployment.Spec.Replicas &&
				deployment.Status.ObservedGeneration >= deployment.Generation {
				return true, nil
			}

			return false, nil
		},
	)
}

// VerifyVolcanoSchedulerArgs verifies that the Volcano scheduler Deployment has
// the expected sharding flags in its container args.
func VerifyVolcanoSchedulerArgs(expectedMode string, expectedName string) error {
	deployment, namespace, err := findVolcanoSchedulerDeployment(KubeClient)
	if err != nil {
		return fmt.Errorf("failed to find volcano scheduler deployment: %w", err)
	}

	// Find the scheduler container
	var container *corev1.Container
	for i := range deployment.Spec.Template.Spec.Containers {
		if strings.Contains(strings.ToLower(deployment.Spec.Template.Spec.Containers[i].Name), "scheduler") {
			container = &deployment.Spec.Template.Spec.Containers[i]
			break
		}
	}

	if container == nil {
		return fmt.Errorf("no scheduler container found in deployment %s/%s", namespace, deployment.Name)
	}

	// Verify sharding mode flag
	modeFound := false
	nameFound := false

	for _, arg := range container.Args {
		if strings.HasPrefix(arg, SchedulerShardingModeFlag) {
			parts := strings.Split(arg, "=")
			if len(parts) == 2 && parts[1] == expectedMode {
				modeFound = true
			} else {
				return fmt.Errorf("sharding mode mismatch: expected %s, got %s", expectedMode, arg)
			}
		}

		if strings.HasPrefix(arg, SchedulerShardingNameFlag) {
			parts := strings.Split(arg, "=")
			if len(parts) == 2 && parts[1] == expectedName {
				nameFound = true
			} else if expectedName == "" {
				return fmt.Errorf("unexpected sharding name flag: %s", arg)
			} else {
				return fmt.Errorf("sharding name mismatch: expected %s, got %s", expectedName, arg)
			}
		}
	}

	if !modeFound {
		return fmt.Errorf("sharding mode flag not found in deployment args")
	}

	if expectedName != "" && !nameFound {
		return fmt.Errorf("sharding name flag %s not found in deployment args", expectedName)
	}

	return nil
}
