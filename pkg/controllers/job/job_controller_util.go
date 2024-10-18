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
	"sort"
	"strconv"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/apis/pkg/apis/bus/v1alpha1"
	"volcano.sh/apis/pkg/apis/helpers"
	schedulingv2 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/controllers/apis"
	jobhelpers "volcano.sh/volcano/pkg/controllers/job/helpers"
	"volcano.sh/volcano/pkg/controllers/util"
)

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

	// If no priority class specified in pod template, use priority class specified in job
	if len(pod.Spec.PriorityClassName) == 0 && len(job.Spec.PriorityClassName) != 0 {
		pod.Spec.PriorityClassName = job.Spec.PriorityClassName
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

	index := strconv.Itoa(ix)
	pod.Annotations[batch.TaskIndex] = index
	pod.Annotations[batch.TaskSpecKey] = tsKey
	pgName := job.Name + "-" + string(job.UID)
	pod.Annotations[schedulingv2.KubeGroupNameAnnotationKey] = pgName
	pod.Annotations[batch.JobNameKey] = job.Name
	pod.Annotations[batch.QueueNameKey] = job.Spec.Queue
	pod.Annotations[batch.JobVersion] = fmt.Sprintf("%d", job.Status.Version)
	pod.Annotations[batch.PodTemplateKey] = fmt.Sprintf("%s-%s", job.Name, template.Name)
	pod.Annotations[batch.JobRetryCountKey] = strconv.Itoa(int(job.Status.RetryCount))

	if topologyPolicy != "" {
		pod.Annotations[schedulingv2.NumaPolicyKey] = string(topologyPolicy)
	}

	if len(job.Annotations) > 0 {
		if value, found := job.Annotations[schedulingv2.PodPreemptable]; found {
			pod.Annotations[schedulingv2.PodPreemptable] = value
		}
		if value, found := job.Annotations[schedulingv2.CooldownTime]; found {
			pod.Annotations[schedulingv2.CooldownTime] = value
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
	pod.Labels[batch.TaskIndex] = index
	pod.Labels[batch.JobNameKey] = job.Name
	pod.Labels[batch.TaskSpecKey] = tsKey
	pod.Labels[batch.JobNamespaceKey] = job.Namespace
	pod.Labels[batch.QueueNameKey] = job.Spec.Queue
	if len(job.Labels) > 0 {
		if value, found := job.Labels[schedulingv2.PodPreemptable]; found {
			pod.Labels[schedulingv2.PodPreemptable] = value
		}
		if value, found := job.Labels[schedulingv2.CooldownTime]; found {
			pod.Labels[schedulingv2.CooldownTime] = value
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

	// Solve the scenario: When pod events accumulate and vcjobs with the same name are frequently created,
	// it is easy for the pod to cause abnormal status of the newly created vcjob with the same name.
	if len(req.JobUid) != 0 && job != nil && req.JobUid != job.UID {
		klog.V(2).Infof("The req belongs to job(%s/%s) and job uid is %v, but the uid of job(%s/%s) is %v in cache, perform %v action",
			req.Namespace, req.JobName, req.JobUid, job.Namespace, job.Name, job.UID, v1alpha1.SyncJobAction)
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

// CalcFirstCountResources return the first count tasks resource, sorted by priority
func (p TasksPriority) CalcFirstCountResources(count int32) v1.ResourceList {
	sort.Sort(p)
	minReq := v1.ResourceList{}

	for _, task := range p {
		if count <= task.Replicas {
			minReq = quotav1.Add(minReq, calTaskRequests(&v1.Pod{Spec: task.Template.Spec}, count))
			break
		} else {
			minReq = quotav1.Add(minReq, calTaskRequests(&v1.Pod{Spec: task.Template.Spec}, task.Replicas))
			count -= task.Replicas
		}
	}
	return minReq
}

// CalcPGMinResources sums up all task's min available; if not enough, then fill up to jobMinAvailable via task's replicas
func (p TasksPriority) CalcPGMinResources(jobMinAvailable int32) v1.ResourceList {
	sort.Sort(p)
	minReq := v1.ResourceList{}
	podCnt := int32(0)

	// 1. first sum up those tasks whose MinAvailable is set
	for _, task := range p {
		if task.MinAvailable == nil { // actually, all task's min available is set by webhook
			continue
		}

		validReplics := *task.MinAvailable
		if left := jobMinAvailable - podCnt; left < validReplics {
			validReplics = left
		}
		minReq = quotav1.Add(minReq, calTaskRequests(&v1.Pod{Spec: task.Template.Spec}, validReplics))
		podCnt += validReplics
		if podCnt >= jobMinAvailable {
			break
		}
	}

	if podCnt >= jobMinAvailable {
		return minReq
	}

	// 2. fill up the count of pod to jobMinAvailable with tasks whose replicas is not used up, higher priority first
	leftCnt := jobMinAvailable - podCnt
	for _, task := range p {
		left := task.Replicas
		if task.MinAvailable != nil {
			if *task.MinAvailable == task.Replicas {
				continue
			} else {
				left = task.Replicas - *task.MinAvailable
			}
		}

		if leftCnt >= left {
			minReq = quotav1.Add(minReq, calTaskRequests(&v1.Pod{Spec: task.Template.Spec}, left))
			leftCnt -= left
		} else {
			minReq = quotav1.Add(minReq, calTaskRequests(&v1.Pod{Spec: task.Template.Spec}, leftCnt))
			leftCnt = 0
		}
		if leftCnt <= 0 {
			break
		}
	}
	return minReq
}

// calTaskRequests returns requests resource with validReplica replicas
func calTaskRequests(pod *v1.Pod, validReplica int32) v1.ResourceList {
	minReq := v1.ResourceList{}
	usage := *util.GetPodQuotaUsage(pod)
	for i := int32(0); i < validReplica; i++ {
		minReq = quotav1.Add(minReq, usage)
	}
	return minReq
}
