/*
Copyright 2025 The Volcano Authors.

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

package vgpu

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
)

type MIGFactory struct{}

func init() {
	RegisterFactory(vGPUControllerMIG, MIGFactory{})
}

func (f MIGFactory) TryAddPod(gd *GPUDevice, mem uint, core uint) (bool, string) {
	found, dev, usedMem := findMatch(gd.UUID, mem, gd.MigUsage, gd.MigTemplate)
	if !found {
		return false, ""
	}

	gd.UsedNum++
	gd.UsedMem += usedMem
	gd.UsedCore += core
	return true, dev
}

func (f MIGFactory) AddPod(gd *GPUDevice, mem uint, core uint, podUID string, devID string) error {
	group, index, err := decodeMIGID(devID)
	if err != nil {
		klog.ErrorS(err, "Failed to add pod")
		return err
	}

	_, ok := gd.PodMap[podUID]
	if !ok {
		gd.PodMap[podUID] = &GPUUsage{
			UsedMem:  0,
			UsedCore: 0,
		}
	} else {
		return nil
	}
	usedMem := addMigUsed(gd, group, index)
	gd.UsedNum++
	gd.UsedMem += usedMem
	gd.UsedCore += core

	gd.PodMap[podUID].UsedMem += usedMem
	gd.PodMap[podUID].UsedCore += core

	klog.V(4).Infoln("add Pod: ", podUID, usedMem, gd.PodMap[podUID].UsedMem, gd.PodMap[podUID].UsedCore)
	return nil
}

func (f MIGFactory) SubPod(gd *GPUDevice, mem uint, core uint, podUID string, devID string) error {
	groupName, index, err := decodeMIGID(devID)
	if err != nil {
		return fmt.Errorf("Failed to sub pod: %v", err)
	}

	_, ok := gd.PodMap[podUID]
	if !ok {
		return fmt.Errorf("pod not exist in GPU pod map")
	}

	usedMem := subMigUsed(gd, groupName, index)
	gd.UsedNum--
	gd.UsedMem -= mem
	gd.UsedCore -= core

	gd.PodMap[podUID].UsedMem -= usedMem
	gd.PodMap[podUID].UsedCore -= core

	klog.V(4).Infoln("sub Pod: ", podUID, usedMem, gd.PodMap[podUID].UsedMem, gd.PodMap[podUID].UsedCore)
	return nil
}

// Try to find a match
func findMatch(
	uuid string,
	requestMem uint,
	usage config.MigInUse,
	allowedGeometries []config.Geometry,
) (bool, string, uint) {
	// If a group is already in use
	if usage.Index >= 0 {
		group := allowedGeometries[usage.Index]
		fitted, position, realMem := pickFromGroup(group, usage.UsageList, requestMem)
		if fitted {
			MIGID := encodeMIGID(uuid, group.Group, position)
			return true, MIGID, realMem
		} else {
			return false, "", 0
		}
	}

	// No group in use yet, try groups in order
	for _, group := range allowedGeometries {
		fitted, position, realMem := pickFromGroup(group, nil, requestMem)
		if fitted {
			MIGID := encodeMIGID(uuid, group.Group, position)
			return true, MIGID, realMem
		} else {
			continue
		}
	}
	return false, "", 0
}

/*
The uuid for mig device is like this: GPU-0fc3eda5-e98b-a25b-5b0d-cf5c855d1448[group2,3]
The group2 is the name of the group; The "3" is the position in the group which is
resource count before this resource group + in-resource index - 1.
For example: the group define like this:
  - models: [ "A100-SXM4-80GB", "A100 80GB PCIe", "A100-PCIE-80GB"]
    allowedGeometries:
  - group: "group2"
    geometries:
  - name: 2g.20gb
    memory: 20480
    count: 3
  - name: 1g.10gb
    memory: 10240
    count: 1

The position of "1g.10gb" in group2 is 3 + 1 - 1. "3" is the resource count before
"1g.10gb", the "1" is in-resource index.
*/
func pickFromGroup(group config.Geometry, usage config.MIGS, requestMemory uint) (bool, int, uint) {
	type MigTemplateWithIndex struct {
		Index    int
		Instance config.MigTemplate
	}
	// Sort instances by memory ascending
	var instances []MigTemplateWithIndex
	for i, inst := range group.Instances {
		instances = append(instances, MigTemplateWithIndex{
			Index:    i,
			Instance: inst,
		})
	}
	sort.Slice(instances, func(i, j int) bool {
		return instances[i].Instance.Memory < instances[j].Instance.Memory
	})

	for _, inst := range instances {
		if len(usage) == 0 && inst.Instance.Memory >= requestMemory {
			position := getPosition(group, inst.Instance.Count, []int{}, inst.Index)
			klog.V(4).Infoln("pick mig group with no used group: ", inst.Index, inst.Instance.Memory, group.Group)
			return true, position, inst.Instance.Memory
		}
		for _, usedInst := range usage {
			if usedInst.Name == inst.Instance.Name {
				available := inst.Instance.Count - len(usedInst.UsedIndex)
				if available > 0 && inst.Instance.Memory >= requestMemory {
					position := getPosition(group, inst.Instance.Count, usedInst.UsedIndex, inst.Index)
					klog.V(4).Infoln("pick mig group with used group: ", usedInst.Name, inst.Instance.Name, position)
					return true, position, inst.Instance.Memory
				}
			}
		}
	}
	klog.V(2).Infoln("pick mig group but no suitalbe")
	return false, -1, 0
}

func getPosition(group config.Geometry, count int, usedIndex []int, index int) int {
	position := 0
	for i := 0; i < index; i++ {
		position += group.Instances[i].Count
	}

	for i := 0; i < count; i++ {
		found := false
		for _, v := range usedIndex {
			if v == i {
				found = true
				break
			}
		}
		if !found {
			position += i
			break
		}
	}

	return position
}

func findPosition(group config.Geometry, position int) (instanceIndex, resourceIndex int) {
	sum := 0
	for i, instance := range group.Instances {
		if position < sum+instance.Count {
			return i, position - sum
		}
		sum += instance.Count
	}
	return -1, -1
}

func encodeMIGID(uuid, group string, position int) string {
	return fmt.Sprintf("%s[%s-%d]", uuid, group, position)
}

func decodeMIGID(id string) (group string, position int, err error) {
	// Find the opening bracket
	start := strings.Index(id, "[")
	end := strings.Index(id, "]")

	if start == -1 || end == -1 || start > end {
		return "", 0, fmt.Errorf("invalid format")
	}

	content := id[start+1 : end]
	parts := strings.Split(content, "-")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("invalid format inside brackets")
	}

	group = parts[0]
	position, err = strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid index: %v", err)
	}

	return group, position, nil
}

func addMigUsed(gd *GPUDevice, groupName string, position int) uint {
	for groupIndex, group := range gd.MigTemplate {
		if group.Group == groupName {
			klog.V(4).Infoln("add mig used: ", group.Group, groupIndex)
			gd.MigUsage.Index = groupIndex
			instanceIndex, resourceIndex := findPosition(group, position)
			migName := group.Instances[instanceIndex].Name
			klog.V(4).Infoln("add mig: ", migName, gd.MigUsage.UsageList)
			for i := range gd.MigUsage.UsageList {
				if gd.MigUsage.UsageList[i].Name == migName {
					gd.MigUsage.UsageList[i].UsedIndex = insert(gd.MigUsage.UsageList[i].UsedIndex, resourceIndex)
					return gd.MigUsage.UsageList[i].Memory
				}
			}
			newUsage := config.MigTemplateUsage{
				Name:      group.Instances[instanceIndex].Name,
				Memory:    group.Instances[instanceIndex].Memory,
				InUse:     true,
				UsedIndex: []int{resourceIndex},
			}
			gd.MigUsage.UsageList = append(gd.MigUsage.UsageList, newUsage)
			return group.Instances[instanceIndex].Memory
		}
	}
	return 0
}

func subMigUsed(gd *GPUDevice, groupName string, position int) uint {
	for groupIndex, group := range gd.MigTemplate {
		if group.Group == groupName {
			gd.MigUsage.Index = groupIndex
			instanceIndex, resourceIndex := findPosition(group, position)
			migName := group.Instances[instanceIndex].Name
			for i := range gd.MigUsage.UsageList {
				if gd.MigUsage.UsageList[i].Name != migName {
					continue
				}
				gd.MigUsage.UsageList[i].UsedIndex = remove(gd.MigUsage.UsageList[i].UsedIndex, resourceIndex)
				klog.V(4).Infoln("sub mig used after remove:", gd.MigUsage.UsageList[i].UsedIndex)
				mem := gd.MigUsage.UsageList[i].Memory
				if len(gd.MigUsage.UsageList[i].UsedIndex) == 0 {
					gd.MigUsage.UsageList = append(gd.MigUsage.UsageList[:i], gd.MigUsage.UsageList[i+1:]...)
				}
				return mem
			}
			return 0
		}
	}
	return 0
}

// Insert a value in order
func insert(list []int, value int) []int {
	klog.V(4).Infoln("insert mig used list before: ", list, value)
	// Find the correct index to insert
	i := 0
	for ; i < len(list); i++ {
		if value < list[i] {
			break
		} else if value == list[i] {
			klog.Error("insert mig used list but invalid gpu location: ", list, value)
			return list
		}
	}
	// Insert at index i
	list = append(list, 0)
	copy(list[i+1:], list[i:])
	list[i] = value
	klog.V(4).Infoln("insert mig used list after: ", list)
	return list
}

// Remove first occurrence of a value
func remove(list []int, value int) []int {
	klog.V(4).Info("remove mig used list before: ", list, value)
	for i, v := range list {
		if v == value {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list // value not found
}
