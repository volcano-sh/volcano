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

package deviceshare

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func TestArguments(t *testing.T) {
	framework.RegisterPluginBuilder(PluginName, New)
	defer framework.CleanupPluginBuilders()

	arguments := framework.Arguments{
		"deviceshare.VGPUEnable":     true,
		"deviceshare.SchedulePolicy": "binpack",
		"deviceshare.ScheduleWeight": 10,
	}

	builder, ok := framework.GetPluginBuilder(PluginName)

	if !ok {
		t.Fatalf("should have plugin named %s", PluginName)
	}

	plugin := builder(arguments)
	deviceshare, ok := plugin.(*deviceSharePlugin)

	if !ok {
		t.Fatalf("plugin should be %T, but not %T", deviceshare, plugin)
	}

	weight := deviceshare.scheduleWeight

	if weight != 10 {
		t.Errorf("weight should be 10, but not %v", weight)
	}

	if deviceshare.schedulePolicy != "binpack" {
		t.Errorf("policy should be binpack, but not %s", deviceshare.schedulePolicy)
	}
}

func addResource(resourceList v1.ResourceList, name v1.ResourceName, need string) {
	resourceList[name] = resource.MustParse(need)
}

func TestVgpuScore(t *testing.T) {
	gpuNode1 := vgpu.GPUDevices{
		Name:   "node1",
		Score:  float64(0),
		Device: make(map[int]*vgpu.GPUDevice),
	}
	gpuNode1.Device[0] = vgpu.NewGPUDevice(0, 30000)
	gpuNode1.Device[0].Type = "NVIDIA"
	gpuNode1.Device[0].Number = 10
	gpuNode1.Device[0].UsedNum = 1
	gpuNode1.Device[0].UsedMem = 3000
	var ok bool
	gpuNode1.Sharing, ok = vgpu.GetSharingHandler("hami-core")
	if !ok {
		t.Errorf("get shring handler failed")
	}

	gpunumber := v1.ResourceName("volcano.sh/vgpu-number")
	gpumemory := v1.ResourceName("volcano.sh/vgpu-memory")

	vgpu.VGPUEnable = true

	p1 := util.BuildPod("c1", "p3", "", v1.PodPending, api.BuildResourceList("2", "10Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p1.Spec.Containers[0].Resources.Requests, gpunumber, "1")
	addResource(p1.Spec.Containers[0].Resources.Requests, gpumemory, "1000")
	p1.Spec.Containers[0].Resources.Limits = make(v1.ResourceList)
	addResource(p1.Spec.Containers[0].Resources.Limits, gpunumber, "1")
	addResource(p1.Spec.Containers[0].Resources.Limits, gpumemory, "1000")

	canAccess, _, err := gpuNode1.FilterNode(p1, "binpack")
	if err != nil || canAccess != 0 {
		t.Errorf("binpack filter failed %s", err.Error())
	}

	score := gpuNode1.ScoreNode(p1, "binpack")
	if score-float64(4000*100)/float64(30000) > 0.05 {
		t.Errorf("score failed expected %f, get %f", float64(4000*100)/float64(30000), score)
	}

	gpuNode2 := vgpu.GPUDevices{
		Name:   "node2",
		Score:  float64(0),
		Device: make(map[int]*vgpu.GPUDevice),
	}
	gpuNode2.Device[0] = vgpu.NewGPUDevice(0, 30000)
	gpuNode2.Device[0].Type = "NVIDIA"
	gpuNode2.Device[0].Number = 10
	gpuNode2.Device[0].UsedNum = 0
	gpuNode2.Device[0].UsedMem = 0
	gpuNode2.Sharing, _ = vgpu.GetSharingHandler("hami-core")

	p2 := util.BuildPod("c2", "p4", "", v1.PodPending, api.BuildResourceList("2", "10Gi"), "pg1", make(map[string]string), make(map[string]string))
	addResource(p2.Spec.Containers[0].Resources.Requests, gpunumber, "1")
	addResource(p2.Spec.Containers[0].Resources.Requests, gpumemory, "1000")
	p2.Spec.Containers[0].Resources.Limits = make(v1.ResourceList)
	addResource(p2.Spec.Containers[0].Resources.Limits, gpunumber, "1")
	addResource(p2.Spec.Containers[0].Resources.Limits, gpumemory, "1000")

	canAccess, _, err = gpuNode2.FilterNode(p2, "spread")
	if err != nil || canAccess != 0 {
		t.Errorf("binpack filter failed %s", err.Error())
	}

	score = gpuNode2.ScoreNode(p1, "spread")
	if score-float64(100) > 0.05 {
		t.Errorf("score failed expected %f, get %f", float64(4000*100)/float64(30000), score)
	}
}
