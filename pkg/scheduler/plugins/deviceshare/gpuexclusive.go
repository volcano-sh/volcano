/*
Copyright 2026 The Volcano Authors.

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
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/api/devices/nvidia/vgpu"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	// GPUExclusiveVGPUResourceNameKey is the argument key for configuring the vGPU resource name.
	GPUExclusiveVGPUResourceNameKey = "deviceshare.GPUExclusiveVGPUResourceName"
	// GPUExclusiveRulesKey is the argument key for exclusivity rules.
	// Each rule is a map of label key → value. Pods matching ALL labels in a rule
	// get exclusive GPU access — no sharing with other rule-matching pods.
	GPUExclusiveRulesKey = "deviceshare.GPUExclusiveRules"

	defaultGPUExclusiveVGPUResourceName = "volcano.sh/vgpu-number"
)

// exclusiveRule defines a set of label key-value pairs.
// A pod matches this rule if it carries ALL specified labels with matching values.
type exclusiveRule struct {
	labels map[string]string
}

func (r exclusiveRule) String() string {
	return fmt.Sprintf("%v", r.labels)
}

type gpuExclusiveConfig struct {
	vgpuResourceName string
	rules            []exclusiveRule
}

func loadGPUExclusiveConfig(args framework.Arguments) gpuExclusiveConfig {
	cfg := gpuExclusiveConfig{
		vgpuResourceName: defaultGPUExclusiveVGPUResourceName,
	}
	if v, ok := args[GPUExclusiveVGPUResourceNameKey].(string); ok && v != "" {
		cfg.vgpuResourceName = v
	}
	if rawRules, ok := args[GPUExclusiveRulesKey]; ok {
		cfg.rules = parseExclusiveRules(rawRules)
	}
	return cfg
}

// parseExclusiveRules converts the raw YAML-decoded rules into typed exclusiveRule slices.
// Expected input: []interface{} where each element is a map of label key → value.
func parseExclusiveRules(raw interface{}) []exclusiveRule {
	slice, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var rules []exclusiveRule
	for _, item := range slice {
		labels := make(map[string]string)
		switch m := item.(type) {
		case map[string]interface{}:
			for k, v := range m {
				if sv, ok := v.(string); ok {
					labels[k] = sv
				}
			}
		case map[interface{}]interface{}:
			for k, v := range m {
				if sk, ok := k.(string); ok {
					if sv, ok := v.(string); ok {
						labels[sk] = sv
					}
				}
			}
		}
		if len(labels) > 0 {
			rules = append(rules, exclusiveRule{labels: labels})
		}
	}
	return rules
}

// matchingRules returns the indices of rules that the pod matches.
// A pod matches a rule if it has ALL label:value pairs specified in that rule.
func matchingRules(pod *v1.Pod, rules []exclusiveRule) []int {
	if pod.Labels == nil {
		return nil
	}
	var matched []int
	for i, rule := range rules {
		if podMatchesRule(pod, rule) {
			matched = append(matched, i)
		}
	}
	return matched
}

// podMatchesRule returns true if the pod has ALL label:value pairs in the rule.
func podMatchesRule(pod *v1.Pod, rule exclusiveRule) bool {
	if pod.Labels == nil {
		return false
	}
	for k, v := range rule.labels {
		if podVal, ok := pod.Labels[k]; !ok || podVal != v {
			return false
		}
	}
	return true
}

// exclusiveGPUDevices wraps vgpu.GPUDevices to enforce that pods matching
// an exclusivity rule get dedicated physical GPUs, not shared with other rule-matching pods.
//
// For pods that don't match any rule, all operations delegate directly to the
// inner GPUDevices.
//
// For pods matching one or more rules, FilterNode and Allocate temporarily cap
// reserved GPUs (set Number = UsedNum) so the vGPU allocator skips them,
// then restore Number afterwards.
type exclusiveGPUDevices struct {
	inner    *vgpu.GPUDevices
	cfg      gpuExclusiveConfig
	nodeName string
	plugin   *deviceSharePlugin
	// ruleGPUs[ruleIndex] = set of GPU indices used by pods matching that rule
	ruleGPUs map[int]map[int]struct{}
	// podRules[podName] = set of rule indices the pod matches
	podRules map[string]map[int]struct{}
	// podUIDs maps podName → podUID for PodMap lookups (upstream uses UID as key)
	podUIDs map[string]string
}

// Compile-time check that exclusiveGPUDevices implements api.Devices.
var _ api.Devices = (*exclusiveGPUDevices)(nil)

// reservedGPUsForPod returns the union of GPU indices reserved by ALL rules
// if the pod matches at least one rule. This ensures cross-rule exclusivity:
// a training pod avoids GPUs used by high-priority batch pods and vice versa.
func (a *exclusiveGPUDevices) reservedGPUsForPod(pod *v1.Pod) map[int]struct{} {
	matched := matchingRules(pod, a.cfg.rules)
	if len(matched) == 0 {
		return nil
	}
	result := make(map[int]struct{})
	for _, gpuSet := range a.ruleGPUs {
		for gpuIdx := range gpuSet {
			result[gpuIdx] = struct{}{}
		}
	}
	return result
}

// capGPUs temporarily sets Number = UsedNum on the given GPU indices so the
// vGPU allocator skips them. Returns saved values for restoreGPUs.
func (a *exclusiveGPUDevices) capGPUs(gpuIndices map[int]struct{}) map[int]uint {
	saved := make(map[int]uint, len(gpuIndices))
	for idx := range gpuIndices {
		if dev, ok := a.inner.Device[idx]; ok && dev != nil {
			saved[idx] = dev.Number
			dev.Number = dev.UsedNum
		}
	}
	return saved
}

func (a *exclusiveGPUDevices) restoreGPUs(saved map[int]uint) {
	for idx, num := range saved {
		if dev, ok := a.inner.Device[idx]; ok && dev != nil {
			dev.Number = num
		}
	}
}

func (a *exclusiveGPUDevices) snapshotUsedNum() map[int]uint {
	snap := make(map[int]uint, len(a.inner.Device))
	for idx, dev := range a.inner.Device {
		snap[idx] = dev.UsedNum
	}
	return snap
}

// trackPodFromPodMap registers a pod in podRules and adds GPU mappings based
// on PodMap entries only. This is safe to call from AddResource because it
// does NOT rebuild ruleGPUs from scratch — it only adds new entries.
func (a *exclusiveGPUDevices) trackPodFromPodMap(pod *v1.Pod) {
	matched := matchingRules(pod, a.cfg.rules)
	if len(matched) == 0 {
		return
	}
	ruleSet := make(map[int]struct{}, len(matched))
	for _, idx := range matched {
		ruleSet[idx] = struct{}{}
	}
	a.podRules[pod.Name] = ruleSet

	// Only add GPU mappings for this pod if it appears in PodMap.
	podUID := string(pod.UID)
	for gpuIdx, dev := range a.inner.Device {
		if _, ok := dev.PodMap[podUID]; ok {
			for ruleIdx := range ruleSet {
				if a.ruleGPUs[ruleIdx] == nil {
					a.ruleGPUs[ruleIdx] = make(map[int]struct{})
				}
				a.ruleGPUs[ruleIdx][gpuIdx] = struct{}{}
			}
		}
	}
}

// untrackPod removes a pod's rule associations and GPU reservations.
func (a *exclusiveGPUDevices) untrackPod(pod *v1.Pod) {
	ruleSet, ok := a.podRules[pod.Name]
	if !ok {
		return
	}
	delete(a.podRules, pod.Name)

	for ruleIdx := range ruleSet {
		gpuSet := a.ruleGPUs[ruleIdx]
		if gpuSet == nil {
			continue
		}
		for gpuIdx := range gpuSet {
			dev, ok := a.inner.Device[gpuIdx]
			if !ok {
				continue
			}
			podUID := string(pod.UID)
			stillUsed := false
			for uid := range dev.PodMap {
				if uid != podUID {
					for otherName := range a.podRules {
						if a.podUIDs[otherName] == uid {
							stillUsed = true
							break
						}
					}
					if stillUsed {
						break
					}
				}
			}
			if !stillUsed {
				delete(gpuSet, gpuIdx)
			}
		}
		if len(gpuSet) == 0 {
			delete(a.ruleGPUs, ruleIdx)
		}
	}
}

// --- api.Devices interface ---

func (a *exclusiveGPUDevices) AddResource(pod *v1.Pod) {
	a.inner.AddResource(pod)
	a.podUIDs[pod.Name] = string(pod.UID)
	a.trackPodFromPodMap(pod)
}

func (a *exclusiveGPUDevices) SubResource(pod *v1.Pod) {
	a.inner.SubResource(pod)
	a.untrackPod(pod)
	delete(a.podUIDs, pod.Name)
}

func (a *exclusiveGPUDevices) AddQueueResource(pod *v1.Pod) map[string]float64 {
	return a.inner.AddQueueResource(pod)
}

func (a *exclusiveGPUDevices) HasDeviceRequest(pod *v1.Pod) bool {
	return a.inner.HasDeviceRequest(pod)
}

func (a *exclusiveGPUDevices) FilterNode(pod *v1.Pod, policy string) (int, string, error) {
	reserved := a.reservedGPUsForPod(pod)
	if len(reserved) > 0 {
		saved := a.capGPUs(reserved)
		code, msg, err := a.inner.FilterNode(pod, policy)
		a.restoreGPUs(saved)
		return code, msg, err
	}
	return a.inner.FilterNode(pod, policy)
}

func (a *exclusiveGPUDevices) ScoreNode(pod *v1.Pod, policy string) float64 {
	return a.inner.ScoreNode(pod, policy)
}

func (a *exclusiveGPUDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	matched := matchingRules(pod, a.cfg.rules)
	if len(matched) == 0 {
		return a.inner.Allocate(kubeClient, pod)
	}

	before := a.snapshotUsedNum()

	reserved := a.reservedGPUsForPod(pod)
	klog.V(4).Infof("gpuexclusive: Allocate pod=%s, matched=%v, reserved=%v, ruleGPUs=%v", pod.Name, matched, reserved, a.ruleGPUs)
	if len(reserved) > 0 {
		saved := a.capGPUs(reserved)
		err := a.inner.Allocate(kubeClient, pod)
		a.restoreGPUs(saved)
		if err != nil {
			return err
		}
	} else {
		if err := a.inner.Allocate(kubeClient, pod); err != nil {
			return err
		}
	}

	ruleSet := make(map[int]struct{}, len(matched))
	for _, idx := range matched {
		ruleSet[idx] = struct{}{}
	}
	a.podRules[pod.Name] = ruleSet

	newGPUs := make(map[int]struct{})
	for idx, dev := range a.inner.Device {
		if dev.UsedNum > before[idx] {
			newGPUs[idx] = struct{}{}
			for ruleIdx := range ruleSet {
				if a.ruleGPUs[ruleIdx] == nil {
					a.ruleGPUs[ruleIdx] = make(map[int]struct{})
				}
				a.ruleGPUs[ruleIdx][idx] = struct{}{}
			}
		}
	}

	// Persist across scheduling sessions.
	if a.plugin != nil && a.nodeName != "" {
		if a.plugin.persistedGPUs[a.nodeName] == nil {
			a.plugin.persistedGPUs[a.nodeName] = make(map[string]map[int]struct{})
		}
		a.plugin.persistedGPUs[a.nodeName][pod.Name] = newGPUs
		if a.plugin.persistedPodRules[a.nodeName] == nil {
			a.plugin.persistedPodRules[a.nodeName] = make(map[string]map[int]struct{})
		}
		a.plugin.persistedPodRules[a.nodeName][pod.Name] = ruleSet
	}

	klog.V(4).Infof("gpuexclusive: allocated pod %s, newGPUs=%v, ruleGPUs=%v",
		pod.Name, newGPUs, a.ruleGPUs)
	return nil
}

func (a *exclusiveGPUDevices) Release(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	err := a.inner.Release(kubeClient, pod)
	if err != nil {
		return err
	}
	a.untrackPod(pod)
	return nil
}

func (a *exclusiveGPUDevices) GetIgnoredDevices() []string {
	return a.inner.GetIgnoredDevices()
}

func (a *exclusiveGPUDevices) GetStatus() string {
	return a.inner.GetStatus()
}

// wrapGPUDevicesForExclusivity wraps each node's GPUDevices with the exclusivity-aware
// wrapper during OnSessionOpen. Called from deviceshare's OnSessionOpen.
func (dp *deviceSharePlugin) wrapGPUDevicesForExclusivity(ssn *framework.Session) {
	cfg := loadGPUExclusiveConfig(dp.pluginArguments)

	klog.V(4).Infof("gpuexclusive config: vgpuResourceName=%s, rules=%v",
		cfg.vgpuResourceName, cfg.rules)

	if len(cfg.rules) == 0 {
		klog.V(2).Info("gpuexclusive: no rules configured, skipping GPU exclusivity wrapping")
		return
	}

	for _, node := range ssn.Nodes {
		if node.Others == nil {
			continue
		}
		devObj, ok := node.Others[vgpu.DeviceName]
		if !ok || devObj == nil {
			continue
		}
		inner, ok := devObj.(*vgpu.GPUDevices)
		if !ok || inner == nil {
			continue
		}

		// Find existing pods on this node and compute their rule matches.
		podRules := make(map[string]map[int]struct{})
		podUIDs := make(map[string]string)
		uidToName := make(map[string]string)
		for _, task := range node.Tasks {
			if task.Pod == nil {
				continue
			}
			uid := string(task.Pod.UID)
			podUIDs[task.Pod.Name] = uid
			uidToName[uid] = task.Pod.Name
			matched := matchingRules(task.Pod, cfg.rules)
			if len(matched) == 0 {
				continue
			}
			ruleSet := make(map[int]struct{}, len(matched))
			for _, idx := range matched {
				ruleSet[idx] = struct{}{}
			}
			podRules[task.Pod.Name] = ruleSet
		}

		// Build UUID → device index map for annotation-based lookup.
		uuidToIdx := make(map[string]int, len(inner.Device))
		for idx, dev := range inner.Device {
			if dev != nil {
				uuidToIdx[dev.UUID] = idx
			}
		}

		// Build initial ruleGPUs from multiple sources.
		ruleGPUs := make(map[int]map[int]struct{})

		// Source 1: PodMap
		for gpuIdx, dev := range inner.Device {
			for podUID := range dev.PodMap {
				podName := uidToName[podUID]
				if ruleSet, ok := podRules[podName]; ok {
					for ruleIdx := range ruleSet {
						if ruleGPUs[ruleIdx] == nil {
							ruleGPUs[ruleIdx] = make(map[int]struct{})
						}
						ruleGPUs[ruleIdx][gpuIdx] = struct{}{}
					}
				}
			}
		}

		// Source 2: Pod annotations
		for podName, ruleSet := range podRules {
			podUID := podUIDs[podName]
			alreadyTracked := false
			for _, dev := range inner.Device {
				if _, ok := dev.PodMap[podUID]; ok {
					alreadyTracked = true
					break
				}
			}
			if alreadyTracked {
				continue
			}
			for _, task := range node.Tasks {
				if task.Pod == nil || task.Pod.Name != podName {
					continue
				}
				ann, ok := task.Pod.Annotations[vgpu.AssignedIDsAnnotations]
				if !ok || ann == "" {
					break
				}
				for _, contDevs := range vgpu.DecodePodDevices(ann) {
					for _, cd := range contDevs {
						if gpuIdx, ok := uuidToIdx[cd.UUID]; ok {
							for ruleIdx := range ruleSet {
								if ruleGPUs[ruleIdx] == nil {
									ruleGPUs[ruleIdx] = make(map[int]struct{})
								}
								ruleGPUs[ruleIdx][gpuIdx] = struct{}{}
							}
						}
					}
				}
				break
			}
		}

		// Source 3: Persisted state from previous scheduling cycles.
		if persisted, ok := dp.persistedGPUs[node.Name]; ok {
			persistedRules := dp.persistedPodRules[node.Name]
			for podName, gpuSet := range persisted {
				if _, inPodRules := podRules[podName]; !inPodRules {
					continue
				}
				podUID := podUIDs[podName]
				alreadyTracked := false
				for _, dev := range inner.Device {
					if _, ok := dev.PodMap[podUID]; ok {
						alreadyTracked = true
						break
					}
				}
				if alreadyTracked {
					continue
				}
				ruleSet := persistedRules[podName]
				if ruleSet == nil {
					continue
				}
				for gpuIdx := range gpuSet {
					for ruleIdx := range ruleSet {
						if ruleGPUs[ruleIdx] == nil {
							ruleGPUs[ruleIdx] = make(map[int]struct{})
						}
						ruleGPUs[ruleIdx][gpuIdx] = struct{}{}
					}
				}
			}
		}

		// Prune persisted entries for pods no longer on this node.
		activePods := make(map[string]bool, len(node.Tasks))
		for _, task := range node.Tasks {
			if task.Pod != nil {
				activePods[task.Pod.Name] = true
			}
		}
		if persisted, ok := dp.persistedGPUs[node.Name]; ok {
			for podName := range persisted {
				if !activePods[podName] {
					delete(persisted, podName)
					if pr, ok := dp.persistedPodRules[node.Name]; ok {
						delete(pr, podName)
					}
				}
			}
		}

		wrapper := &exclusiveGPUDevices{
			inner:    inner,
			cfg:      cfg,
			nodeName: node.Name,
			plugin:   dp,
			ruleGPUs: ruleGPUs,
			podRules: podRules,
			podUIDs:  podUIDs,
		}
		node.Others[vgpu.DeviceName] = wrapper

		klog.V(4).Infof("gpuexclusive: OnSessionOpen node=%s, podRules=%v, ruleGPUs=%v, tasks=%d",
			node.Name, podRules, ruleGPUs, len(node.Tasks))
	}
}
