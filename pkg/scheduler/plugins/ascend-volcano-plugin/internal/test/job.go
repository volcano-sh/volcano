/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package test is using for HuaWei Ascend testing.
*/
package test

import (
	"os"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

const (
	fakeNodeName = "node0"
	annoCards    = "Ascend910-0,Ascend910-1,Ascend910-2,Ascend910-3,Ascend910-4,Ascend910-5,Ascend910-6,Ascend910-7"
	unhealthyNPU = "huawei.com/Ascend910-Unhealthy"
)

// FakeSchedulerJobAttrByJob fake scheduler attr by job
func FakeSchedulerJobAttrByJob(job *api.JobInfo) util.SchedulerJobAttr {
	attr := util.SchedulerJobAttr{
		ComJob: util.ComJob{
			Name:      job.UID,
			NameSpace: job.Namespace,
			Selector:  nil,
			Label:     nil,
		},
	}
	name, num, err := plugin.GetVCJobReqNPUTypeFromJobInfo(job)
	if err != nil {
		return attr
	}
	NPUJob := &util.NPUJob{
		ReqNPUName: name,
		ReqNPUNum:  num,
		Tasks:      plugin.GetJobNPUTasks(job),
	}
	NPUJob.NPUTaskNum = len(NPUJob.Tasks)
	attr.NPUJob = NPUJob
	return attr
}

// NewDefaultHandler new default handler
func NewDefaultHandler() *plugin.ScheduleHandler {
	scheduleHandler := &plugin.ScheduleHandler{
		NPUPlugins: make(sets.String),
		ScheduleEnv: plugin.ScheduleEnv{
			FrameAttr:               plugin.NewVolcanoFrame(),
			JobScheduleInfoRecorder: plugin.NewJobScheduleInfoRecorder(),
			ClusterCache:            plugin.NewClusterCache(),
		},
	}
	scheduleHandler.PolicyBuilder = func() plugin.SchedulerPluginNeed {
		return nil
	}
	return scheduleHandler
}

// FakeScheduleEnv fake normal schedule env
func FakeScheduleEnv() *plugin.ScheduleEnv {
	sHandle := NewDefaultHandler()
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	for _, jobInfo := range ssn.Jobs {
		test.SetJobStatusRunning(jobInfo)
	}
	sHandle.InitVolcanoFrameFromSsn(ssn)
	sHandle.InitNodesFromSsn(ssn)
	sHandle.InitJobsFromSsn(ssn)
	task := sHandle.Jobs["vcjob/pg0"].Tasks["vcjob-pod1"]
	task.Status = util.TaskStatusAllocate
	sHandle.Jobs["vcjob/pg0"].Tasks["vcjob-pod1"] = task
	sHandle.ScheduleEnv.FrameAttr.KubeClient = fake.NewSimpleClientset()
	node0 := sHandle.ScheduleEnv.Nodes[fakeNodeName]
	node0.Annotation[unhealthyNPU] = annoCards
	sHandle.ScheduleEnv.Nodes[fakeNodeName] = node0
	return &sHandle.ScheduleEnv
}

// PatchGetCm go monkey patch get cm
func PatchGetCm(name, nameSpace string, data map[string]string) *gomonkey.Patches {
	return gomonkey.ApplyFunc(k8s.GetConfigMap, func(client kubernetes.Interface, namespace, cmName string) (
		*v1.ConfigMap, error) {
		return test.FakeConfigmap(name, nameSpace, data), nil
	})
}

// InitNormalsHandlerBySsnFunc init normal sHandler by ssn func
func InitNormalsHandlerBySsnFunc(ssn *framework.Session, initSsnFunc ...func(ssn *framework.Session)) {
	if ssn == nil {
		return
	}
	for _, initFunc := range initSsnFunc {
		initFunc(ssn)
	}
}

// FakeTorNodeData Fake tor node date for
func FakeTorNodeData() map[string]string {
	torNodeDataPath := "../../testdata/tor/tor-node.json"
	torInfoCMKey := "tor_info"
	data, err := os.ReadFile(torNodeDataPath)
	if err != nil {
		return nil
	}
	return map[string]string{
		torInfoCMKey: string(data),
	}
}
