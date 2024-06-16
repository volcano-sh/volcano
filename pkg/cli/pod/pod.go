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

package pod

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/duration"
	kubeclientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"
	"k8s.io/kubernetes/pkg/util/node"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/cli/util"
)

const (
	// Name pod name
	Name string = "Name"
	// Ready pod ready
	Ready string = "Ready"
	// Status pod status
	Status string = "Status"
	// Restart pod restart
	Restart string = "Restart"
	// Age pod age
	Age string = "Age"
)

type listFlags struct {
	util.CommonFlags
	// Namespace pod namespace
	Namespace string
	// JobName represents the pod created under this vcjob,
	// filtered by volcano.sh/job-name label
	// the default value is empty, which means
	// that all pods under vcjob will be obtained.
	JobName string
	// allNamespace represents getting all namespaces
	allNamespace bool
	// QueueName represents queue name
	QueueName string
}

var listPodFlags = &listFlags{}

// InitListFlags init list command flags.
func InitListFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &listPodFlags.CommonFlags)

	cmd.Flags().StringVarP(&listPodFlags.QueueName, "queue", "q", "", "list pod with specified queue name")
	cmd.Flags().StringVarP(&listPodFlags.JobName, "job", "j", "", "list pod with specified job name")
	cmd.Flags().StringVarP(&listPodFlags.Namespace, "namespace", "n", "default", "the namespace of job")
	cmd.Flags().BoolVarP(&listPodFlags.allNamespace, "all-namespaces", "", false, "list jobs in all namespaces")
}

// ListPods lists all pods details created by vcjob
func ListPods(ctx context.Context) error {
	config, err := util.BuildConfig(listPodFlags.Master, listPodFlags.Kubeconfig)
	if err != nil {
		return err
	}
	if listPodFlags.allNamespace {
		listPodFlags.Namespace = ""
	}

	var pods corev1.PodList

	// if job name are specified, use job name to filter pods
	// if the job is specified, it means that no matter whether the queue name is specified or not,
	// we can just use the job name query. Because vcjob itself will specify a specific queue
	if listPodFlags.JobName != "" {
		labelSelector, err := createPodLabelSelectorByJobName(listPodFlags.JobName)
		if err != nil {
			return err
		}
		listVcjobPodsRes, err := listPodByLabel(ctx, config, labelSelector)
		if err != nil {
			return err
		}
		pods.Items = append(pods.Items, listVcjobPodsRes.Items...)
		if listPodFlags.QueueName != "" {
			// if queue is specified, check if the queue name used by the pod matches with the one passed in
			if !matchPodsLabel(listVcjobPodsRes, v1alpha1.QueueNameKey, listPodFlags.QueueName) {
				return fmt.Errorf("the input vcjob %s does not match the queue %s",
					listPodFlags.JobName, listPodFlags.QueueName)
			}
		}
	} else if listPodFlags.QueueName != "" {
		// if queue is specified, use queue name to filter pods
		// we need to consider both the vcjob pod and other workload pods

		// first, list all pods
		listAllPodsRes, err := listPodByLabel(ctx, config, labels.Everything())
		if err != nil {
			return err
		}
		// then, filter all vcjobs's pods belong to the queue
		listVcJobPodsRes := filterPodsByLabel(listAllPodsRes, v1alpha1.QueueNameKey, listPodFlags.QueueName)

		// then, filter all other workload pods belong to the queue
		listNormalPodsRes := filterPodsByAnnotation(listAllPodsRes, schedulingv1beta1.QueueNameAnnotationKey, listPodFlags.QueueName)

		pods.Items = append(pods.Items, listVcJobPodsRes.Items...)
		// append only not exist
		pods.Items = appendIfNotExists(pods.Items, listNormalPodsRes.Items)
	} else {
		// if neither job name nor queue name are specified,
		// use default label selector, for all vcjobs's pods
		labelSelector, err := createDefaultLabelSelector()
		if err != nil {
			return err
		}
		listPodsRes, err := listPodByLabel(ctx, config, labelSelector)
		if err != nil {
			return err
		}
		pods.Items = append(pods.Items, listPodsRes.Items...)
	}

	if len(pods.Items) == 0 {
		fmt.Printf("No resources found\n")
		return nil
	}
	PrintPods(&pods, os.Stdout)

	return nil
}

func PrintPods(pods *corev1.PodList, writer io.Writer) {
	maxNameLen := 0
	maxReadyLen := 0
	maxStatusLen := 0
	maxRestartLen := 0
	maxAgeLen := 0

	var infoList []PodInfo
	for _, pod := range pods.Items {
		info := printPod(&pod)
		infoList = append(infoList, info)
		// update max length for each column
		if len(info.Name) > maxNameLen {
			maxNameLen = len(info.Name)
		}
		if len(info.ReadyContainers) > maxReadyLen {
			maxReadyLen = len(info.ReadyContainers)
		}
		if len(info.Status) > maxStatusLen {
			maxStatusLen = len(info.Status)
		}
		if len(info.Restarts) > maxRestartLen {
			maxRestartLen = len(info.Restarts)
		}
		if len(info.CreationTimestamp) > maxAgeLen {
			maxAgeLen = len(info.CreationTimestamp)
		}
	}
	columnSpacing := 8
	maxNameLen += columnSpacing
	maxReadyLen += columnSpacing
	maxStatusLen += columnSpacing
	maxRestartLen += columnSpacing
	maxAgeLen += columnSpacing
	formatStr := fmt.Sprintf("%%-%ds%%-%ds%%-%ds%%-%ds%%-%ds\n", maxNameLen, maxReadyLen, maxStatusLen, maxRestartLen, maxAgeLen)
	_, err := fmt.Fprintf(writer, formatStr, Name, Ready, Status, Restart, Age)
	if err != nil {
		fmt.Printf("Failed to print Pod information: %s.\n", err)
		return
	}
	for _, info := range infoList {
		_, err := fmt.Fprintf(writer, formatStr, info.Name, info.ReadyContainers, info.Status, info.Restarts, info.CreationTimestamp)
		if err != nil {
			fmt.Printf("Failed to print Pod information: %s.\n", err)
			return
		}
	}
}

// matchPodsLabel check if the pods match the labelKey and labelValue
func matchPodsLabel(pods *corev1.PodList, labelKey, labelValue string) bool {
	for _, pod := range pods.Items {
		if value, exist := pod.Labels[labelKey]; exist {
			if value != labelValue {
				return false
			}
		}
	}
	return true
}

// filterPodsByAnnotation filter pods based on annotationKey and annotationValue
func filterPodsByAnnotation(pods *corev1.PodList, annotationKey, annotationValue string) *corev1.PodList {
	filteredPods := &corev1.PodList{}
	for _, pod := range pods.Items {
		if value, exist := pod.Annotations[annotationKey]; exist {
			if value == annotationValue {
				filteredPods.Items = append(filteredPods.Items, pod)
			}
		}
	}
	return filteredPods
}

// filterPodsByLabel filter pods based on labelKey and labelValue
func filterPodsByLabel(pods *corev1.PodList, labelKey, labelValue string) *corev1.PodList {
	filteredPods := &corev1.PodList{}
	for _, pod := range pods.Items {
		if value, exist := pod.Labels[labelKey]; exist {
			if value == labelValue {
				filteredPods.Items = append(filteredPods.Items, pod)
			}
		}
	}
	return filteredPods
}

// listPodByLabel lists pods based on label selector
func listPodByLabel(ctx context.Context, config *rest.Config, labelSelector labels.Selector) (*corev1.PodList, error) {
	client := kubeclientset.NewForConfigOrDie(config)
	// Construct the list options based on label and annotation selectors
	opts := metav1.ListOptions{
		LabelSelector: labelSelector.String(),
	}
	return client.CoreV1().Pods(listPodFlags.Namespace).List(ctx, opts)
}

// createPodLabelSelectorByJobName creates a label selector for selecting pods belong to a vcjob.
func createPodLabelSelectorByJobName(jobName string) (labels.Selector, error) {
	var labelSelector labels.Selector
	inRequirement, err := labels.NewRequirement(v1alpha1.JobNameKey, selection.In, []string{jobName})
	if err != nil {
		return nil, err
	}
	labelSelector = labels.NewSelector().Add(*inRequirement)
	return labelSelector, nil
}

// createDefaultLabelSelector creates a label selector for all vcjobs's pods.
func createDefaultLabelSelector() (labels.Selector, error) {
	var labelSelector labels.Selector
	// If job name is not provided, select all pods created by vcjobs.
	inRequirement, err := labels.NewRequirement(v1alpha1.JobNameKey, selection.Exists, []string{})
	if err != nil {
		return nil, err
	}
	labelSelector = labels.NewSelector().Add(*inRequirement)
	return labelSelector, nil
}

// Helper function to append items to a slice if they do not already exist
func appendIfNotExists(existing, toAppend []corev1.Pod) []corev1.Pod {
	for _, pod := range toAppend {
		exists := false
		for _, existingPod := range existing {
			if existingPod.Name == pod.Name {
				exists = true
				break
			}
		}
		if !exists {
			existing = append(existing, pod)
		}
	}
	return existing
}

// translateTimestampSince translates a timestamp into a human-readable string using time.Since.
func translateTimestampSince(timestamp metav1.Time) string {
	if timestamp.IsZero() {
		return "<unknown>"
	}
	return duration.HumanDuration(time.Since(timestamp.Time))
}

// PodInfo holds information about a pod.
type PodInfo struct {
	Name              string
	ReadyContainers   string
	Status            string
	Restarts          string
	CreationTimestamp string
}

// printPod information in a tabular format.
// The reference implementation comes from:
// https://github.com/kubernetes/kubernetes/blob/master/pkg/printers/internalversion/printers.go
func printPod(pod *corev1.Pod) PodInfo {
	restarts := 0
	restartableInitContainerRestarts := 0
	totalContainers := len(pod.Spec.Containers)
	readyContainers := 0
	lastRestartDate := metav1.NewTime(time.Time{})
	lastRestartableInitContainerRestartDate := metav1.NewTime(time.Time{})

	podPhase := pod.Status.Phase
	reason := string(podPhase)
	if pod.Status.Reason != "" {
		reason = pod.Status.Reason
	}

	// If the Pod carries {type:PodScheduled, reason:SchedulingGated}, set reason to 'SchedulingGated'.
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled && condition.Reason == corev1.PodReasonSchedulingGated {
			reason = corev1.PodReasonSchedulingGated
		}
	}

	row := metav1.TableRow{
		Object: runtime.RawExtension{Object: pod},
	}

	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		row.Conditions = podSuccessConditions
	case corev1.PodFailed:
		row.Conditions = podFailedConditions
	}

	initContainers := make(map[string]*corev1.Container)
	for i := range pod.Spec.InitContainers {
		initContainers[pod.Spec.InitContainers[i].Name] = &pod.Spec.InitContainers[i]
		if isRestartableInitContainer(&pod.Spec.InitContainers[i]) {
			totalContainers++
		}
	}

	initializing := false
	for i := range pod.Status.InitContainerStatuses {
		container := pod.Status.InitContainerStatuses[i]
		restarts += int(container.RestartCount)
		if container.LastTerminationState.Terminated != nil {
			terminatedDate := container.LastTerminationState.Terminated.FinishedAt
			if lastRestartDate.Before(&terminatedDate) {
				lastRestartDate = terminatedDate
			}
		}
		if isRestartableInitContainer(initContainers[container.Name]) {
			restartableInitContainerRestarts += int(container.RestartCount)
			if container.LastTerminationState.Terminated != nil {
				terminatedDate := container.LastTerminationState.Terminated.FinishedAt
				if lastRestartableInitContainerRestartDate.Before(&terminatedDate) {
					lastRestartableInitContainerRestartDate = terminatedDate
				}
			}
		}
		switch {
		case container.State.Terminated != nil && container.State.Terminated.ExitCode == 0:
			continue
		case isRestartableInitContainer(initContainers[container.Name]) &&
			container.Started != nil && *container.Started:
			if container.Ready {
				readyContainers++
			}
			continue
		case container.State.Terminated != nil:
			// initialization is failed
			if len(container.State.Terminated.Reason) == 0 {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Init:Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("Init:ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else {
				reason = "Init:" + container.State.Terminated.Reason
			}
			initializing = true
		case container.State.Waiting != nil && len(container.State.Waiting.Reason) > 0 && container.State.Waiting.Reason != "PodInitializing":
			reason = "Init:" + container.State.Waiting.Reason
			initializing = true
		default:
			reason = fmt.Sprintf("Init:%d/%d", i, len(pod.Spec.InitContainers))
			initializing = true
		}
		break
	}

	if !initializing || isPodInitializedConditionTrue(&pod.Status) {
		restarts = restartableInitContainerRestarts
		lastRestartDate = lastRestartableInitContainerRestartDate
		hasRunning := false
		for i := len(pod.Status.ContainerStatuses) - 1; i >= 0; i-- {
			container := pod.Status.ContainerStatuses[i]

			restarts += int(container.RestartCount)
			if container.LastTerminationState.Terminated != nil {
				terminatedDate := container.LastTerminationState.Terminated.FinishedAt
				if lastRestartDate.Before(&terminatedDate) {
					lastRestartDate = terminatedDate
				}
			}
			if container.State.Waiting != nil && container.State.Waiting.Reason != "" {
				reason = container.State.Waiting.Reason
			} else if container.State.Terminated != nil && container.State.Terminated.Reason != "" {
				reason = container.State.Terminated.Reason
			} else if container.State.Terminated != nil && container.State.Terminated.Reason == "" {
				if container.State.Terminated.Signal != 0 {
					reason = fmt.Sprintf("Signal:%d", container.State.Terminated.Signal)
				} else {
					reason = fmt.Sprintf("ExitCode:%d", container.State.Terminated.ExitCode)
				}
			} else if container.Ready && container.State.Running != nil {
				hasRunning = true
				readyContainers++
			}
		}

		// change pod status back to "Running" if there is at least one container still reporting as "Running" status
		if reason == "Completed" && hasRunning {
			if hasPodReadyCondition(pod.Status.Conditions) {
				reason = "Running"
			} else {
				reason = "NotReady"
			}
		}
	}

	if pod.DeletionTimestamp != nil && pod.Status.Reason == node.NodeUnreachablePodReason {
		reason = "Unknown"
	} else if pod.DeletionTimestamp != nil && !podutil.IsPodPhaseTerminal(corev1.PodPhase(podPhase)) {
		reason = "Terminating"
	}

	restartsStr := strconv.Itoa(restarts)
	if restarts != 0 && !lastRestartDate.IsZero() {
		restartsStr = fmt.Sprintf("%d (%s ago)", restarts, translateTimestampSince(lastRestartDate))
	}

	podInfo := PodInfo{
		Name:              pod.Name,
		ReadyContainers:   fmt.Sprintf("%d/%d", readyContainers, totalContainers),
		Status:            reason,
		Restarts:          restartsStr,
		CreationTimestamp: translateTimestampSince(pod.CreationTimestamp),
	}
	return podInfo
}

var (
	podSuccessConditions = []metav1.TableRowCondition{{Type: metav1.RowCompleted, Status: metav1.ConditionTrue, Reason: string(corev1.PodSucceeded), Message: "The pod has completed successfully."}}
	podFailedConditions  = []metav1.TableRowCondition{{Type: metav1.RowCompleted, Status: metav1.ConditionTrue, Reason: string(corev1.PodFailed), Message: "The pod failed."}}
)

// hasPodReadyCondition returns true if the pod has a ready condition
func hasPodReadyCondition(conditions []corev1.PodCondition) bool {
	for _, condition := range conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// isRestartableInitContainer returns true if the given init container is restartable
func isRestartableInitContainer(initContainer *corev1.Container) bool {
	if initContainer == nil {
		return false
	}
	if initContainer.RestartPolicy == nil {
		return false
	}

	return *initContainer.RestartPolicy == corev1.ContainerRestartPolicyAlways
}

// isPodInitializedConditionTrue returns true if the PodInitialized condition is true
func isPodInitializedConditionTrue(status *corev1.PodStatus) bool {
	for _, condition := range status.Conditions {
		if condition.Type != corev1.PodInitialized {
			continue
		}

		return condition.Status == corev1.ConditionTrue
	}
	return false
}
