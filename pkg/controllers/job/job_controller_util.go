/*
Copyright 2017 The Volcano Authors.

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

package job

import (
	"fmt"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	schedulingv2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/apis"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
)

var detectionPeriodOfDependsOntask time.Duration

// MakePodName append podname,jobname,taskName and index and returns the string.
func MakePodName(jobName string, taskName string, index int) string {
	return fmt.Sprintf(jobhelpers.PodNameFmt, jobName, taskName, index)
}

func createJobPod(job *batch.Job, template *v1.PodTemplateSpec, topologyPolicy batch.NumaPolicy, ix int, jobForwarding bool) *v1.Pod {
	templateCopy := template.DeepCopy()

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobhelpers.MakePodName(job.Name, template.Name, ix),
			Namespace: job.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(job, helpers.JobKind),
			},
			Labels:      templateCopy.Labels,
			Annotations: templateCopy.Annotations,
		},
		Spec: templateCopy.Spec,
	}

	// If no scheduler name in Pod, use scheduler name from Job.
	if len(pod.Spec.SchedulerName) == 0 {
		pod.Spec.SchedulerName = job.Spec.SchedulerName
	}

	volumeMap := make(map[string]string)
	for _, volume := range job.Spec.Volumes {
		vcName := volume.VolumeClaimName
		name := fmt.Sprintf("%s-%s", job.Name, jobhelpers.GenRandomStr(12))
		if _, ok := volumeMap[vcName]; !ok {
			volume := v1.Volume{
				Name: name,
				VolumeSource: v1.VolumeSource{
					PersistentVolumeClaim: &v1.PersistentVolumeClaimVolumeSource{
						ClaimName: vcName,
					},
				},
			}
			pod.Spec.Volumes = append(pod.Spec.Volumes, volume)
			volumeMap[vcName] = name
		} else {
			// duplicate volumes, should be prevented
			continue
		}

		for i, c := range pod.Spec.Containers {
			vm := v1.VolumeMount{
				MountPath: volume.MountPath,
				Name:      name,
			}
			pod.Spec.Containers[i].VolumeMounts = append(c.VolumeMounts, vm)
		}
	}

	tsKey := templateCopy.Name
	if len(tsKey) == 0 {
		tsKey = batch.DefaultTaskSpec
	}

	if len(pod.Annotations) == 0 {
		pod.Annotations = make(map[string]string)
	}

	pod.Annotations[batch.TaskSpecKey] = tsKey
	pod.Annotations[schedulingv2.KubeGroupNameAnnotationKey] = job.Name
	pod.Annotations[batch.JobNameKey] = job.Name
	pod.Annotations[batch.QueueNameKey] = job.Spec.Queue
	pod.Annotations[batch.JobVersion] = fmt.Sprintf("%d", job.Status.Version)
	pod.Annotations[batch.PodTemplateKey] = fmt.Sprintf("%s-%s", job.Name, template.Name)

	if topologyPolicy != "" {
		pod.Annotations[schedulingv2.NumaPolicyKey] = string(topologyPolicy)
	}

	if len(job.Annotations) > 0 {
		if value, found := job.Annotations[schedulingv2.PodPreemptable]; found {
			pod.Annotations[schedulingv2.PodPreemptable] = value
		}
		if value, found := job.Annotations[schedulingv2.RevocableZone]; found {
			pod.Annotations[schedulingv2.RevocableZone] = value
		}

		if value, found := job.Annotations[schedulingv2.JDBMinAvailable]; found {
			pod.Annotations[schedulingv2.JDBMinAvailable] = value
		} else if value, found := job.Annotations[schedulingv2.JDBMaxUnavailable]; found {
			pod.Annotations[schedulingv2.JDBMaxUnavailable] = value
		}
	}

	if len(pod.Labels) == 0 {
		pod.Labels = make(map[string]string)
	}

	// Set pod labels for Service.
	pod.Labels[batch.JobNameKey] = job.Name
	pod.Labels[batch.TaskSpecKey] = tsKey
	pod.Labels[batch.JobNamespaceKey] = job.Namespace
	pod.Labels[batch.QueueNameKey] = job.Spec.Queue
	if len(job.Labels) > 0 {
		if value, found := job.Labels[schedulingv2.PodPreemptable]; found {
			pod.Labels[schedulingv2.PodPreemptable] = value
		}
	}

	if jobForwarding {
		pod.Annotations[batch.JobForwardingKey] = "true"
		pod.Labels[batch.JobForwardingKey] = "true"
	}

	return pod
}

func applyPolicies(job *batch.Job, req *apis.Request) v1alpha1.Action {
	if len(req.Action) != 0 {
		return req.Action
	}

	if req.Event == v1alpha1.OutOfSyncEvent {
		return v1alpha1.SyncJobAction
	}

	// For all the requests triggered from discarded job resources will perform sync action instead
	if req.JobVersion < job.Status.Version {
		klog.Infof("Request %s is outdated, will perform sync instead.", req)
		return v1alpha1.SyncJobAction
	}

	// Overwrite Job level policies
	if len(req.TaskName) != 0 {
		// Parse task level policies
		for _, task := range job.Spec.Tasks {
			if task.Name == req.TaskName {
				for _, policy := range task.Policies {
					policyEvents := getEventlist(policy)

					if len(policyEvents) > 0 && len(req.Event) > 0 {
						if checkEventExist(policyEvents, req.Event) || checkEventExist(policyEvents, v1alpha1.AnyEvent) {
							return policy.Action
						}
					}

					// 0 is not an error code, is prevented in validation admission controller
					if policy.ExitCode != nil && *policy.ExitCode == req.ExitCode {
						return policy.Action
					}
				}
				break
			}
		}
	}

	// Parse Job level policies
	for _, policy := range job.Spec.Policies {
		policyEvents := getEventlist(policy)

		if len(policyEvents) > 0 && len(req.Event) > 0 {
			if checkEventExist(policyEvents, req.Event) || checkEventExist(policyEvents, v1alpha1.AnyEvent) {
				return policy.Action
			}
		}

		// 0 is not an error code, is prevented in validation admission controller
		if policy.ExitCode != nil && *policy.ExitCode == req.ExitCode {
			return policy.Action
		}
	}

	return v1alpha1.SyncJobAction
}

func getEventlist(policy batch.LifecyclePolicy) []v1alpha1.Event {
	policyEventsList := policy.Events
	if len(policy.Event) > 0 {
		policyEventsList = append(policyEventsList, policy.Event)
	}
	return policyEventsList
}

func checkEventExist(policyEvents []v1alpha1.Event, reqEvent v1alpha1.Event) bool {
	for _, event := range policyEvents {
		if event == reqEvent {
			return true
		}
	}
	return false
}

func addResourceList(list, req, limit v1.ResourceList) {
	for name, quantity := range req {
		if value, ok := list[name]; !ok {
			list[name] = quantity.DeepCopy()
		} else {
			value.Add(quantity)
			list[name] = value
		}
	}

	if req != nil {
		return
	}

	// If Requests is omitted for a container,
	// it defaults to Limits if that is explicitly specified.
	for name, quantity := range limit {
		if value, ok := list[name]; !ok {
			list[name] = quantity.DeepCopy()
		} else {
			value.Add(quantity)
			list[name] = value
		}
	}
}

// TaskPriority structure.
type TaskPriority struct {
	priority int32

	batch.TaskSpec
}

// TasksPriority is a slice of TaskPriority.
type TasksPriority []TaskPriority

func (p TasksPriority) Len() int { return len(p) }

func (p TasksPriority) Less(i, j int) bool {
	return p[i].priority > p[j].priority
}

func (p TasksPriority) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

func isControlledBy(obj metav1.Object, gvk schema.GroupVersionKind) bool {
	controllerRef := metav1.GetControllerOf(obj)
	if controllerRef == nil {
		return false
	}
	if controllerRef.APIVersion == gvk.GroupVersion().String() && controllerRef.Kind == gvk.Kind {
		return true
	}
	return false
}

func SetDetectionPeriodOfDependsOntask(period time.Duration) {
	detectionPeriodOfDependsOntask = period
}
