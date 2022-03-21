package resourcequota

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	quotav1 "k8s.io/apiserver/pkg/quota/v1"
	"k8s.io/klog"

	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// PluginName indicates name of volcano scheduler plugin.
const PluginName = "resourcequota"

// resourceQuota scope not supported
type resourceQuotaPlugin struct {
	// Arguments given for the plugin
	pluginArguments framework.Arguments
}

// New return resourcequota plugin
func New(arguments framework.Arguments) framework.Plugin {
	return &resourceQuotaPlugin{
		pluginArguments: arguments,
	}
}

func (rq *resourceQuotaPlugin) Name() string {
	return PluginName
}

func (rq *resourceQuotaPlugin) OnSessionOpen(ssn *framework.Session) {
	pendingResources := make(map[string]v1.ResourceList)

	ssn.AddJobEnqueueableFn(rq.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)

		resourcesRequests := job.PodGroup.Spec.MinResources

		if resourcesRequests == nil {
			return util.Permit
		}

		quotas := ssn.NamespaceInfo[api.NamespaceName(job.Namespace)].QuotaStatus
		for _, resourceQuota := range quotas {
			hardResources := quotav1.ResourceNames(resourceQuota.Hard)
			requestedUsage := quotav1.Mask(*resourcesRequests, hardResources)

			var resourcesUsed = resourceQuota.Used
			if pendingUse, found := pendingResources[job.Namespace]; found {
				resourcesUsed = quotav1.Add(pendingUse, resourcesUsed)
			}
			newUsage := quotav1.Add(resourcesUsed, requestedUsage)
			maskedNewUsage := quotav1.Mask(newUsage, quotav1.ResourceNames(requestedUsage))

			if allowed, exceeded := quotav1.LessThanOrEqual(maskedNewUsage, resourceQuota.Hard); !allowed {
				failedRequestedUsage := quotav1.Mask(requestedUsage, exceeded)
				failedUsed := quotav1.Mask(resourceQuota.Used, exceeded)
				failedHard := quotav1.Mask(resourceQuota.Hard, exceeded)
				msg := fmt.Sprintf("resource quota insufficient, requested: %v, used: %v, limited: %v",
					failedRequestedUsage,
					failedUsed,
					failedHard,
				)
				klog.V(4).Infof("enqueueable false for job: %s/%s, because :%s", job.Namespace, job.Name, msg)
				ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), msg)
				return util.Reject
			}
		}
		if _, found := pendingResources[job.Namespace]; !found {
			pendingResources[job.Namespace] = v1.ResourceList{}
		}
		pendingResources[job.Namespace] = quotav1.Add(pendingResources[job.Namespace], *resourcesRequests)
		return util.Permit
	})
}

func (rq *resourceQuotaPlugin) OnSessionClose(session *framework.Session) {
}
