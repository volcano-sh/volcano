package resourcequota

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

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
	pendingResources := make(map[string]*api.Resource)

	ssn.AddJobEnqueueableFn(rq.Name(), func(obj interface{}) int {
		job := obj.(*api.JobInfo)

		resourcesRequests := job.TotalRequest

		if resourcesRequests == nil {
			return util.Permit
		}

		if ssn.NamespaceInfo[api.NamespaceName(job.Namespace)] == nil {
			return util.Permit
		}

		quotas := ssn.NamespaceInfo[api.NamespaceName(job.Namespace)].QuotaStatus
		for _, resourceQuota := range quotas {
			hardResources := api.NewResource(resourceQuota.Hard)
			usedResources := api.NewResource(resourceQuota.Used)

			if pendingUsed, found := pendingResources[job.Namespace]; found {
				usedResources.Add(pendingUsed)
			}

			newUsage := usedResources.Add(resourcesRequests)
			if res, err := newUsage.LessEqualWithReason(hardResources, api.Infinity); res != true {
				klog.V(2).Infof("enqueueable false for job: %s/%s, because :%s", job.Namespace, job.Name, err)
				klog.V(4).Infof("job: %s/%s, request resources: %v, newUsage resources: %v, hardResources: %v",
					job.Namespace, job.Name, resourcesRequests, newUsage, hardResources)
				ssn.RecordPodGroupEvent(job.PodGroup, v1.EventTypeNormal, string(scheduling.PodGroupUnschedulableType), err)
				return util.Reject
			}
		}

		if _, found := pendingResources[job.Namespace]; !found {
			pendingResources[job.Namespace] = &api.Resource{}
		}
		pendingResources[job.Namespace].Add(resourcesRequests)
		return util.Permit
	})
}

func (rq *resourceQuotaPlugin) OnSessionClose(session *framework.Session) {
}
