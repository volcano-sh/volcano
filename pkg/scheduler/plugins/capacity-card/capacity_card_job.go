/*
Copyright 2018 The Kubernetes Authors.
Copyright 2018-2025 The Volcano Authors.

Modifications made by Volcano authors:
- Enhanced gang scheduling validation with task-level validity checks
- Improved preemption logic to respect gang scheduling constraints
- Added support for job starving detection and enhanced pipeline state management

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

package capacitycard

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	resourcehelper "k8s.io/kubectl/pkg/util/resource"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// JobInfo describes the job used in capacity plugin.
type JobInfo struct {
	*api.JobInfo

	// allocated is the allocated cpu, memory and card resource to job. Ignore none card scalar resource.
	allocated *api.Resource

	// preCheckCardResource is the card resource retrieved from job annotation for pre-check purpose.
	// Pre-check will prevent pending pod created(working in JobEnqueueableFn) when queue has no enough card resource.
	// It is suggested to set this annotation for jobs which need card resource, although it is optional.
	preCheckCardResource *api.Resource
}

// NewJobInfo creates a JobInfo instance.
func (p *Plugin) NewJobInfo(job *api.JobInfo) (*JobInfo, error) {
	// read card request from Job(PodGroup).
	preCheckCardResource := GetCardResourceFromAnnotations(
		"job"+"/"+job.Namespace+"/"+job.Name,
		job.PodGroup.Annotations,
		JobAnnotationKeyCardRequest,
	)

	// job allocated cpu, memory and card resource to job. Ignore none card scalar resource.
	allocated := api.EmptyResource()
	// job request cpu, memory and card resource to job. Ignore none card scalar resource.
	request := api.EmptyResource()
	for _, ti := range job.Tasks {
		if ti.Pod != nil {
			resReq, err := p.GetTaskRequestResources(ti)
			if err != nil {
				klog.Warningf(
					"Failed to get request resource for task <%s/%s> in job <%s/%s>: %+v",
					ti.Namespace, ti.Name, job.Namespace, job.Name, err,
				)
				continue
			}

			request.Add(resReq)
			if api.AllocatedStatus(ti.Status) {
				allocated.Add(resReq)
			}
		}
	}

	// update preCheckCardResource with job request resource
	if len(request.ScalarResources) > 0 {
		preCheckCardResource.ScalarResources = request.ScalarResources
	}

	return &JobInfo{
		JobInfo:              job,
		allocated:            allocated,
		preCheckCardResource: preCheckCardResource,
	}, nil
}

// GetMinResources return the min resources of PodGroup.
func (p *Plugin) GetMinResources(ji *JobInfo) *api.Resource {
	jobResource := api.EmptyResource()
	if ji.preCheckCardResource != nil {
		jobResource.Add(ji.preCheckCardResource)
	}

	jobMinReq := ji.JobInfo.GetMinResources()

	// add scalar resources if cardUnlimitedCpuMemory not set or has no card resources
	if (!p.isCardUnlimitedCpuMemory || !p.HasCardResource(jobResource, jobMinReq)) && jobMinReq != nil {
		jobResource.MilliCPU = jobMinReq.MilliCPU
		jobResource.Memory = jobMinReq.Memory
	}

	return jobResource
}

// GetElasticResources returns those partly resources in allocated which are more than its minResource
func (p *Plugin) GetElasticResources(ji *JobInfo) *api.Resource {
	if ji.allocated == nil {
		return api.EmptyResource()
	}
	var (
		minResource = p.GetMinResources(ji)
		elastic     = api.ExceededPart(ji.allocated, minResource)
	)
	return elastic
}

// getCardNameFromTask retrieves the card name from task's pod annotations.
func (p *Plugin) getCardNameFromTask(ti *api.TaskInfo) string {
	if ti.Pod == nil {
		return ""
	}
	return ti.Pod.Annotations[TaskAnnotationKeyCardName]
}

// getCardResourceFromTask retrieves the card resource from task's pod annotations and requests/limits.
func (p *Plugin) getCardResourceFromTask(ti *api.TaskInfo) (*api.Resource, error) {
	if ti.Pod == nil {
		return api.EmptyResource(), nil
	}

	cardName := p.getCardNameFromTask(ti)
	if cardName == "" {
		// no card requested, might be CPU type task.
		return api.EmptyResource(), nil
	}

	// if multi-cards are requested, retrieve the confirmed card name from bound node.
	if strings.Contains(cardName, MultiCardSeparator) {
		multiCardNames := strings.Split(cardName, MultiCardSeparator)
		if ti.Pod.Spec.NodeName == "" {
			podCardResource, err := p.getCardResourceFromTaskPod(multiCardNames[0], ti.Pod)
			if err != nil {
				return api.EmptyResource(), err
			}
			for _, scalarCount := range podCardResource.ScalarResources {
				// change to multi-card resource.
				return &api.Resource{
					ScalarResources: map[v1.ResourceName]float64{
						v1.ResourceName(cardName): scalarCount,
					},
				}, nil
			}
		}
		return p.getCardResourceFromNodeNameForMultiCardTask(ti, cardName)
	}
	return p.getCardResourceFromTaskPod(cardName, ti.Pod)
}

// getCardResourceFromNodeNameForMultiCardTask retrieves the real card resource from node label if pod is
// already bound to certain node.
func (p *Plugin) getCardResourceFromNodeNameForMultiCardTask(
	ti *api.TaskInfo, multiCardName string,
) (*api.Resource, error) {
	// already bound to certain node, retrieve the real card name from node label.
	node, err := p.nodeLister.Get(ti.Pod.Spec.NodeName)
	if err != nil {
		return api.EmptyResource(), fmt.Errorf(
			"failed to get node <%s> for task <%s/%s>: %+v",
			ti.Pod.Spec.NodeName, ti.Namespace, ti.Name, err,
		)
	}
	var (
		nodeCardInfo   = p.getCardResourceFromNode(node)
		multiCardNames = strings.Split(multiCardName, MultiCardSeparator)
	)
	for _, singleCardName := range multiCardNames {
		if _, ok := nodeCardInfo.CardNameToResourceName[v1.ResourceName(singleCardName)]; ok {
			podCardResource, err := p.getCardResourceFromTaskPod(singleCardName, ti.Pod)
			if err == nil {
				return podCardResource, nil
			}
		}
	}
	return api.EmptyResource(), fmt.Errorf(
		"no valid card found on node <%s> for task <%s/%s> with multi-card name <%s>",
		node.Name, ti.Namespace, ti.Name, multiCardName,
	)
}

// getCardResourceFromTaskPod retrieves the card resource from task's pod requests/limits.
func (p *Plugin) getCardResourceFromTaskPod(cardName string, pod *v1.Pod) (*api.Resource, error) {
	if pod == nil {
		return api.EmptyResource(), fmt.Errorf("invalid parameter: pod is nil")
	}

	// retrieve card resource name from requests/limits.
	// eg: cardName is "NVIDIA-H200", the resource name in requests/limits is "nvidia.com/gpu".
	cardResourceName, ok := p.cardNameToResourceName[v1.ResourceName(cardName)]
	if !ok {
		return api.EmptyResource(), fmt.Errorf("no resource name found for card <%s>", cardName)
	}
	// retrieve card count from requests/limits by card resource name.
	// eg: card name is "NVIDIA-H200", resource name is "nvidia.com/gpu",
	// the "nvidia.com/gpu" count is 2 in pod requests/limits,
	// then the "NVIDIA-H200" card count is 2.
	podRequests, podLimits := resourcehelper.PodRequestsAndLimits(pod)
	if quantity, found := podLimits[cardResourceName]; found {
		return &api.Resource{
			ScalarResources: map[v1.ResourceName]float64{
				v1.ResourceName(cardName): float64(quantity.Value() * cardCountQuantityMultiplier),
			},
		}, nil
	}
	if quantity, found := podRequests[cardResourceName]; found {
		return &api.Resource{
			ScalarResources: map[v1.ResourceName]float64{
				v1.ResourceName(cardName): float64(quantity.Value() * cardCountQuantityMultiplier),
			},
		}, nil
	}
	return api.EmptyResource(), fmt.Errorf(
		"no resource <%s> defined in requests/limits for card <%s>",
		cardResourceName, cardName,
	)
}

// GetTaskRequestResources get the task request cpu, memory and card resource to job. Ignore none card scalar resource.
func (p *Plugin) GetTaskRequestResources(task *api.TaskInfo) (*api.Resource, error) {
	totalRequest, err := p.getCardResourceFromTask(task)
	if err != nil {
		return nil, err
	}

	// add cpu and memory if cardUnlimitedCpuMemory not set or has no card resources
	if (!p.isCardUnlimitedCpuMemory || !p.HasCardResource(totalRequest, task.Resreq)) && task.Resreq != nil {
		totalRequest.MilliCPU = task.Resreq.MilliCPU
		totalRequest.Memory = task.Resreq.Memory
	}

	return totalRequest, nil
}
