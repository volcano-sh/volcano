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
Package test is using for HuaWei Ascend pin scheduling test.
*/
package test

import (
	"fmt"
	"os"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/tools/record"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const (
	annoCards  = "Ascend910-0,Ascend910-1,Ascend910-2,Ascend910-3,Ascend910-4,Ascend910-5,Ascend910-6,Ascend910-7"
	maxTaskNum = 10000
)

// AddResource add resource into resourceList
func AddResource(resourceList v1.ResourceList, name v1.ResourceName, need string) {
	resourceList[name] = resource.MustParse(need)
}

// AddJobIntoFakeSSN Add test job into fake SSN.
func AddJobIntoFakeSSN(ssn *framework.Session, info ...*api.JobInfo) {
	for _, testJob := range info {
		ssn.Jobs[testJob.UID] = testJob
	}
}

// AddConfigIntoFakeSSN Add test node into fake SSN.
func AddConfigIntoFakeSSN(ssn *framework.Session, configs []conf.Configuration) {
	ssn.Configurations = configs
}

// FakeNormalSSN fake normal test ssn.
func FakeNormalSSN(confs []conf.Configuration) *framework.Session {
	binder := util.NewFakeBinder(0)
	//binder := &util.FakeBinder{
	//	Binds:   map[string]string{},
	//	Channel: make(chan string),
	//}
	schedulerCache := &cache.SchedulerCache{
		Nodes:         make(map[string]*api.NodeInfo),
		Jobs:          make(map[api.JobID]*api.JobInfo),
		Queues:        make(map[api.QueueID]*api.QueueInfo),
		Binder:        binder,
		StatusUpdater: &util.FakeStatusUpdater{},
		//VolumeBinder:  &util.FakeVolumeBinder{},

		Recorder: record.NewFakeRecorder(npuIndex3),
	}

	nodes := FakeNormalTestNodes(fakeNodeNum)
	for _, node := range nodes {
		node.Node.Labels[acceleratorKey] = acceleratorValue
		node.Node.Labels[serverTypeKey] = fake910ServerType
		node.Node.Labels[chipTypeKey] = fakeChipName + fakeChipType
		node.Node.Annotations[NPU910CardName] = annoCards
		schedulerCache.AddNode(node.Node)
	}

	jobInf := FakeNormalTestJob("pg0", npuIndex3)
	var minRes = make(v1.ResourceList, npuIndex3)
	for _, task := range jobInf.Tasks {
		for k, v := range task.Resreq.ScalarResources {
			minRes[k] = resource.MustParse(fmt.Sprintf("%f", v))
		}
		schedulerCache.AddPod(task.Pod)
	}
	jobInf.PodGroup.Spec.MinResources = &minRes
	snapshot := schedulerCache.Snapshot()
	ssn := &framework.Session{
		UID:            uuid.NewUUID(),
		Jobs:           map[api.JobID]*api.JobInfo{jobInf.UID: jobInf},
		Nodes:          snapshot.Nodes,
		RevocableNodes: snapshot.RevocableNodes,
		Queues:         snapshot.Queues,
		NamespaceInfo:  snapshot.NamespaceInfo,
		Configurations: confs,
		NodeList:       util.GetNodeList(snapshot.Nodes, snapshot.NodeList),
	}
	return ssn
}

// AddJobInfoIntoSsn add job to ssn jobInfos
func AddJobInfoIntoSsn(ssn *framework.Session, job *api.JobInfo) {
	if ssn.Jobs == nil {
		ssn.Jobs = make(map[api.JobID]*api.JobInfo)
	}
	ssn.Jobs[job.UID] = job
}

// FakeJobInfoByName fake job info by job name
func FakeJobInfoByName(jobName string, taskNum int) *api.JobInfo {
	if taskNum < 0 || taskNum > maxTaskNum {
		return nil
	}
	jobInfo := FakeNormalTestJob(jobName, taskNum)
	var minRes = make(v1.ResourceList, taskNum)
	for _, task := range jobInfo.Tasks {
		for k, v := range task.Resreq.ScalarResources {
			minRes[k] = resource.MustParse(fmt.Sprintf("%f", v))
		}
	}
	jobInfo.MinAvailable = int32(taskNum)
	jobInfo.PodGroup.Spec.MinResources = &minRes
	return jobInfo
}

// AddJobInfoLabel add job info label
func AddJobInfoLabel(job *api.JobInfo, key, value string) {
	if job.PodGroup.Labels == nil {
		job.PodGroup.Labels = map[string]string{}
	}
	job.PodGroup.Labels[key] = value
}

// AddJobInfoAnnotations add job info annotations
func AddJobInfoAnnotations(job *api.JobInfo, key, value string) {
	if job.PodGroup.Annotations == nil {
		job.PodGroup.Annotations = map[string]string{}
	}
	job.PodGroup.Annotations[key] = value
}

// FakeConfigurations fake volcano frame config
func FakeConfigurations() []conf.Configuration {
	return []conf.Configuration{
		{
			Name: "init-params",
			Arguments: map[string]interface{}{
				overTimeKey:              overTimeValue,
				nslbVersionKey:           nslbVersionValue,
				sharedTorNumKey:          sharedTorNumValue,
				superPodSizeKey:          superPodSizeValue,
				reserveNodesKey:          reserveNodesValue,
				presetVirtualDeviceKey:   presetVirtualDeviceValue,
				useClusterInfoManagerKey: useClusterInfoManagerValue,
			},
		},
	}
}

// PatchReset go monkey patch reset
func PatchReset(patch *gomonkey.Patches) {
	if patch != nil {
		patch.Reset()
	}
}

// FakeConfigmap fake config map
func FakeConfigmap(name, nameSpace string, data map[string]string) *v1.ConfigMap {
	fakeCm := &v1.ConfigMap{}
	fakeCm.Name = name
	fakeCm.Namespace = nameSpace
	fakeCm.Data = data
	return fakeCm
}

// FakeTorNodeData Fake tor node date for
func FakeTorNodeData() map[string]string {
	torNodeDataPath := "../testdata/tor/tor-node.json"
	torInfoCMKey := "tor_info"
	data, err := os.ReadFile(torNodeDataPath)
	if err != nil {
		return nil
	}
	return map[string]string{
		torInfoCMKey: string(data),
	}
}
