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

package hami

import (
	"encoding/json"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api/devices"
	"volcano.sh/volcano/pkg/scheduler/api/devices/config"
	"volcano.sh/volcano/pkg/scheduler/plugins/util/nodelock"
	"volcano.sh/volcano/third_party/hami/util"
)

const (
	DeviceBindAllocating = "allocating"
	DeviceBindFailed     = "failed"
	DeviceBindSuccess    = "success"

	Ascend910Prefix        = "Ascend910"
	Ascend910NetworkWeight = 10
	// binpack means the lower device memory remained after this allocation, the better
	binpackPolicy = "binpack"
	// spread means better put this task into an idle GPU card than a shared GPU card
	spreadPolicy      = "spread"
	binpackMultiplier = 100
	spreadMultiplier  = 100
)

type AscendDevice struct {
	config           config.VNPUConfig
	nodeRegisterAnno string
	useUUIDAnno      string
	noUseUUIDAnno    string
	handshakeAnno    string
	DeviceInfo       *devices.DeviceInfo
	DeviceUsage      *devices.DeviceUsage
	Score            float64
}

type AscendDevices struct {
	NodeName string
	Type     string
	Devices  map[string]*AscendDevice
	Policy   string
}

type RuntimeInfo struct {
	UUID string `json:"UUID,omitempty"`
	Temp string `json:"temp,omitempty"`
}

var (
	AscendHAMiVNPUEnable bool
	NodeLockEnable       bool
)

func NewAscendDevices(name string, node *v1.Node) map[string]*AscendDevices {
	ascendDevices := make(map[string]*AscendDevices)
	if node == nil {
		klog.Warningf("Node is nil for node %s, returning empty AscendDevices", name)
		return ascendDevices
	}
	curConfig := config.GetConfig()
	if curConfig == nil {
		klog.V(5).InfoS("cur config is null. call GetDefaultDevicesConfig")
		curConfig = config.GetDefaultDevicesConfig()
	}
	devs := InitDevices(curConfig.VNPUs)
	for _, dev := range devs {
		nodeDevices, err := dev.GetNodeDevices(*node)
		if err != nil {
			klog.Warningf("Failed to get node devices. nodeName %s, deviceType %s, error %s", node.Name, dev.CommonWord(), err)
			continue
		}
		asDevices := &AscendDevices{
			NodeName: name,
			Type:     dev.CommonWord(),
			Devices:  make(map[string]*AscendDevice),
		}
		for _, nd := range nodeDevices {
			cur_dev := &AscendDevice{
				config:           dev.config,
				nodeRegisterAnno: dev.nodeRegisterAnno,
				useUUIDAnno:      dev.useUUIDAnno,
				noUseUUIDAnno:    dev.noUseUUIDAnno,
				handshakeAnno:    dev.handshakeAnno,
				DeviceInfo:       nd,
				DeviceUsage: &devices.DeviceUsage{
					Used:      0,
					Usedmem:   0,
					Usedcores: 0,
				},
			}
			asDevices.Devices[nd.ID] = cur_dev
			klog.V(5).Infof("add device. ID %s dev_info %+v", cur_dev.DeviceInfo.ID, cur_dev.DeviceInfo)
		}
		ascendDevices[dev.CommonWord()] = asDevices
	}
	return ascendDevices
}

func GetAscendDeviceNames() []string {
	curConfig := config.GetConfig()
	if curConfig == nil {
		klog.V(5).InfoS("cur config is null. call GetDefaultDevicesConfig")
		curConfig = config.GetDefaultDevicesConfig()
	}
	deviceNames := make([]string, 0, len(curConfig.VNPUs))
	for _, vnpu := range curConfig.VNPUs {
		deviceNames = append(deviceNames, vnpu.CommonWord)
	}
	return deviceNames
}

func (ads *AscendDevices) AddResourceUsage(id string, cores int32, mem int32) error {
	dev, ok := ads.Devices[id]
	if !ok {
		return fmt.Errorf("ascend device %s not found", id)
	}
	dev.DeviceUsage.Used++
	dev.DeviceUsage.Usedcores += cores
	dev.DeviceUsage.Usedmem += mem
	return nil
}

func (ads *AscendDevices) SubResourceUsage(id string, cores int32, mem int32) error {
	dev, ok := ads.Devices[id]
	if !ok {
		return fmt.Errorf("ascend device %s not found", id)
	}
	dev.DeviceUsage.Used--
	dev.DeviceUsage.Usedcores -= cores
	dev.DeviceUsage.Usedmem -= mem
	return nil
}

func (ads *AscendDevices) AddResource(pod *v1.Pod) {
	if ads == nil {
		return
	}
	ads.addResource(pod.Annotations, pod)
}

func (ads *AscendDevices) SubResource(pod *v1.Pod) {
	if ads == nil {
		return
	}
	ano_key := devices.InRequestDevices[ads.Type]
	ano, ok := pod.Annotations[ano_key]
	if !ok {
		return
	}
	con_devs, err := devices.DecodeContainerDevices(ano)
	if err != nil {
		klog.ErrorS(err, "failed to decode container devices", "pod", pod.Name, "annotation", ano)
		return
	}
	for _, cono_dev := range con_devs {
		ads.SubResourceUsage(cono_dev.UUID, cono_dev.Usedcores, cono_dev.Usedmem)
	}
}

func (ads *AscendDevices) addResource(annotations map[string]string, pod *v1.Pod) {
	ano_key := devices.InRequestDevices[ads.Type]
	ano, ok := annotations[ano_key]
	if !ok {
		return
	}
	con_devs, err := devices.DecodeContainerDevices(ano)
	if err != nil {
		klog.ErrorS(err, "failed to decode container devices", "pod", pod.Name, "annotation", ano)
		return
	}
	for _, cono_dev := range con_devs {
		ads.AddResourceUsage(cono_dev.UUID, cono_dev.Usedcores, cono_dev.Usedmem)
	}
}

func (ads *AscendDevices) AddQueueResource(pod *v1.Pod) map[string]float64 {
	return map[string]float64{}
}

func (ads *AscendDevices) HasDeviceRequest(pod *v1.Pod) bool {
	if !AscendHAMiVNPUEnable {
		return false
	}
	randDev, err := ads.getFirstDevice()
	if randDev == nil || err != nil {
		return false
	}
	var vnpu_config = randDev.config
	for _, container := range pod.Spec.Containers {
		_, ok := container.Resources.Limits[v1.ResourceName(vnpu_config.ResourceName)]
		if ok {
			klog.V(5).Infof("%s check HasDeviceRequest ok. %s", ads.Type, vnpu_config.ResourceName)
			return true
		}
		_, ok = container.Resources.Limits[v1.ResourceName(vnpu_config.ResourceMemoryName)]
		if ok {
			klog.V(5).Infof("%s check HasDeviceRequest ok. %s", ads.Type, vnpu_config.ResourceMemoryName)
			return true
		}
	}
	klog.V(5).Infof("%s check HasDeviceRequest false", ads.Type)
	return false
}

func (ads *AscendDevices) FilterNode(pod *v1.Pod, policy string) (int, string, error) {
	_, err := ads.selectDevices(pod, policy)
	if err != nil {
		return devices.Error, "no ascend device available", err
	}
	klog.V(4).Infoln("ascend DeviceSharing successfully filters pods. device_type:", ads.Type)
	return devices.Success, "", nil
}

func (ads *AscendDevices) ScoreNode(pod *v1.Pod, policy string) float64 {
	ads.Policy = policy
	podDevs, err := ads.selectDevices(pod, policy)
	if err != nil {
		return 0
	}
	score := 0.0
	var usedDevs []*AscendDevice
	for _, dev := range podDevs {
		dev, ok := ads.Devices[dev[0].UUID]
		if !ok {
			return 0
		}
		usedDevs = append(usedDevs, dev)
		score += CalScore(policy, dev.DeviceUsage, dev.DeviceInfo)
	}

	if strings.HasPrefix(ads.Type, Ascend910Prefix) && hasNetworkID(usedDevs) {
		klog.V(4).Infof("all devices have NetworkID. device CommonWord %s", ads.Type)
		cntMap := make(map[int]int)
		for _, dev := range usedDevs {
			if dev.DeviceInfo.CustomInfo == nil {
				return 0
			}
			if networkID, ok := dev.DeviceInfo.CustomInfo["NetworkID"]; ok {
				if id, ok := networkID.(float64); ok {
					cntMap[int(id)]++
				}
			} else {
				return 0
			}
		}
		maxCnt, totalCnt := 0, 0
		for _, cnt := range cntMap {
			if cnt > maxCnt {
				maxCnt = cnt
			}
			totalCnt += cnt
		}
		if totalCnt == 0 {
			return 0
		}
		score += (float64(maxCnt) / float64(totalCnt)) * Ascend910NetworkWeight
	}
	return score
}

func (ads *AscendDevices) Allocate(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	klog.V(4).Infof("Allocate device %s to Pod %s", ads.Type, pod.Name)
	if NodeLockEnable {
		nodelock.UseClient(kubeClient)
		err := nodelock.LockNode(ads.NodeName, ads.Type)
		if err != nil {
			return errors.Errorf("node %s locked for %s hami vnpu. lockname %s", ads.NodeName, pod.Name, err.Error())
		}
	}
	podDevs, err := ads.selectDevices(pod, ads.Policy)
	if err != nil {
		return errors.Errorf("failed to select ascend devices for pod %s: %v", pod.Name, err)
	}
	annotations := ads.CreateAnnotations(pod, podDevs)

	ads.addResource(annotations, pod)
	annotations[util.AssignedNodeAnnotations] = ads.NodeName
	annotations[util.AssignedTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)
	annotations[util.DeviceBindPhase] = "allocating"
	annotations[util.BindTimeAnnotations] = strconv.FormatInt(time.Now().Unix(), 10)

	err = devices.PatchPodAnnotations(kubeClient, pod, annotations)
	if err != nil {
		return err
	}
	if NodeLockEnable {
		nodelock.ReleaseNodeLock(ads.NodeName, ads.Type)
	}
	klog.V(4).Infof("Allocate Success. device %s Pod %s", ads.Type, pod.Name)
	return nil
}

func (ads *AscendDevices) Release(kubeClient kubernetes.Interface, pod *v1.Pod) error {
	return nil
}

func (ads *AscendDevices) GetIgnoredDevices() []string {
	randDev, err := ads.getFirstDevice()
	if randDev == nil || err != nil {
		return []string{""}
	}
	vnpuConfig := randDev.config
	return []string{vnpuConfig.ResourceMemoryName}
}

func (ads *AscendDevices) GetStatus() string {
	return ""
}

func (ads *AscendDevices) selectDevices(pod *v1.Pod, schedulePolicy string) (devices.PodSingleDevice, error) {
	dupDevs := getDeviceSnapshot(ads)
	if len(dupDevs) == 0 {
		return nil, errors.Errorf("no ascend device available")
	}
	for _, dev := range dupDevs {
		dev.Score = CalScore(schedulePolicy, dev.DeviceUsage, dev.DeviceInfo)
	}
	sort.Slice(dupDevs, func(i, j int) bool {
		return dupDevs[i].Score > dupDevs[j].Score
	})
	needTopology := false
	if strings.HasPrefix(ads.Type, Ascend910Prefix) && hasNetworkID(dupDevs) {
		klog.V(4).Infof("all devices have NetworkID. device CommonWord %s", ads.Type)
		needTopology = true
	}
	reqs := dupDevs[0].ResourceReqs(pod)
	var podDevs devices.PodSingleDevice
	usedDevs := make([]*AscendDevice, 0)
	for _, req := range reqs {
		klog.V(5).Infof("req %+v", req)
		availableDevs := make([]*AscendDevice, 0)
		for _, dev := range dupDevs {
			selected := false
			for _, usedDev := range usedDevs {
				if usedDev.DeviceInfo.ID == dev.DeviceInfo.ID {
					selected = true
					break
				}
			}
			if !selected {
				availableDevs = append(availableDevs, dev)
			}
		}
		req_nums := req.Nums
		selectedDevs := make([]*AscendDevice, 0)
		for _, dev := range availableDevs {
			klog.V(5).Infof("check fit. req %+v dev_info %+v dev_usage %+v", req, dev.DeviceInfo, dev.DeviceUsage)
			if !fit(&req, dev) {
				klog.V(5).Infof("fit false. dev ID %s", dev.DeviceInfo.ID)
				continue
			}
			selectedDevs = append(selectedDevs, dev)
			req_nums -= 1
			if req_nums <= 0 && !needTopology {
				break
			}
		}
		if req_nums > 0 {
			klog.V(5).Infof("no enough ascend device available! raw req_nums %d cur req_nums %d", req.Nums, req_nums)
			return nil, errors.Errorf("no enough ascend device available")
		}
		if needTopology {
			selectedDevs = selectDevicesWithTopology(int(req.Nums), selectedDevs)
		}
		usedDevs = append(usedDevs, selectedDevs...)
		var conDevs devices.ContainerDevices
		for _, dev := range selectedDevs {
			conDevs = append(conDevs, devices.ContainerDevice{
				UUID:       dev.DeviceInfo.ID,
				Type:       ads.Type,
				Usedmem:    req.Memreq,
				Usedcores:  req.Coresreq,
				CustomInfo: dev.DeviceInfo.CustomInfo,
			})
		}
		podDevs = append(podDevs, conDevs)
	}
	return podDevs, nil
}

func hasNetworkID(devices []*AscendDevice) bool {
	for _, dev := range devices {
		if dev.DeviceInfo.CustomInfo == nil {
			return false
		}
		if _, ok := dev.DeviceInfo.CustomInfo["NetworkID"]; !ok {
			return false
		}
	}
	return true
}

func fit(req *devices.ContainerDeviceRequest, dev *AscendDevice) bool {
	if req.Type != dev.config.CommonWord {
		return false
	}
	deviceUsage := dev.DeviceUsage
	deviceInfo := dev.DeviceInfo
	if deviceInfo.Count <= deviceUsage.Used {
		return false
	}
	if deviceInfo.Devmem-deviceUsage.Usedmem < req.Memreq {
		return false
	}
	if deviceInfo.Devcore-deviceUsage.Usedcores < req.Coresreq {
		return false
	}
	if deviceInfo.Devcore == 100 && req.Coresreq == 100 && deviceUsage.Used > 0 {
		return false
	}
	if deviceInfo.Devcore != 0 && deviceUsage.Usedcores == deviceInfo.Devcore && req.Coresreq == 0 {
		return false
	}
	return true
}

func getDeviceSnapshot(ads *AscendDevices) []*AscendDevice {
	dupDevs := make([]*AscendDevice, 0, len(ads.Devices))
	for _, dev := range ads.Devices {
		dup_dev := &AscendDevice{
			config:           dev.config,
			nodeRegisterAnno: dev.nodeRegisterAnno,
			useUUIDAnno:      dev.useUUIDAnno,
			noUseUUIDAnno:    dev.noUseUUIDAnno,
			handshakeAnno:    dev.handshakeAnno,
			DeviceInfo:       dev.DeviceInfo,
			DeviceUsage: &devices.DeviceUsage{
				Used:      dev.DeviceUsage.Used,
				Usedmem:   dev.DeviceUsage.Usedmem,
				Usedcores: dev.DeviceUsage.Usedcores,
			},
		}
		dupDevs = append(dupDevs, dup_dev)
	}
	return dupDevs
}

func selectDevicesWithTopology(req_nums int, selected_devs []*AscendDevice) []*AscendDevice {
	networkMap := make(map[int][]*AscendDevice)

	for _, dev := range selected_devs {
		if dev.DeviceInfo.CustomInfo != nil {
			if networkID, ok := dev.DeviceInfo.CustomInfo["NetworkID"]; ok {
				if id, ok := networkID.(float64); ok {
					networkMap[int(id)] = append(networkMap[int(id)], dev)
				}
			}
		}
	}
	type NetworkDeviceCount struct {
		NetworkID int
		Count     int
	}
	var sortedNetworks []NetworkDeviceCount
	for networkID, devices := range networkMap {
		sortedNetworks = append(sortedNetworks, NetworkDeviceCount{
			NetworkID: networkID,
			Count:     len(devices),
		})
	}
	sort.Slice(sortedNetworks, func(i, j int) bool {
		return sortedNetworks[i].Count > sortedNetworks[j].Count
	})
	devs := make([]*AscendDevice, 0)
	for _, item := range sortedNetworks {
		for _, dev := range networkMap[item.NetworkID] {
			devs = append(devs, dev)
			if len(devs) == req_nums {
				return devs
			}
		}
	}
	return devs
}

func (ads *AscendDevices) getFirstDevice() (*AscendDevice, error) {
	if len(ads.Devices) == 0 {
		return nil, errors.New("no ascend device available")
	}
	for _, dev := range ads.Devices {
		return dev, nil
	}
	return nil, errors.New("no ascend device available")
}

func (dev *AscendDevice) trimMemory(m int64) (int64, string) {
	for i := range dev.config.Templates {
		if m <= dev.config.Templates[i].Memory {
			return dev.config.Templates[i].Memory, dev.config.Templates[i].Name
		}
	}
	if m <= dev.config.MemoryCapacity {
		return dev.config.MemoryAllocatable, ""
	}
	return 0, ""
}

func InitDevices(config []config.VNPUConfig) []*AscendDevice {
	devs := make([]*AscendDevice, 0)
	for _, vnpu := range config {
		commonWord := vnpu.CommonWord
		dev := &AscendDevice{
			config:           vnpu,
			nodeRegisterAnno: fmt.Sprintf("%s/node-register-%s", util.HAMiAnnotationsPrefix, commonWord),
			useUUIDAnno:      fmt.Sprintf("%s/use-%s-uuid", util.HAMiAnnotationsPrefix, commonWord),
			noUseUUIDAnno:    fmt.Sprintf("%s/no-use-%s-uuid", util.HAMiAnnotationsPrefix, commonWord),
			handshakeAnno:    fmt.Sprintf("%s/node-handshake-%s", util.HAMiAnnotationsPrefix, commonWord),
		}
		sort.Slice(dev.config.Templates, func(i, j int) bool {
			return dev.config.Templates[i].Memory < dev.config.Templates[j].Memory
		})
		_, ok := devices.InRequestDevices[commonWord]
		if !ok {
			devices.InRequestDevices[commonWord] = fmt.Sprintf("%s/%s-devices-to-allocate", util.HAMiAnnotationsPrefix, commonWord)
			devices.SupportDevices[commonWord] = fmt.Sprintf("%s/%s-devices-allocated", util.HAMiAnnotationsPrefix, commonWord)
			// util.HandshakeAnnos[commonWord] = dev.handshakeAnno
		}
		devs = append(devs, dev)
		klog.Infof("load ascend vnpu config %s: %v", commonWord, dev.config)
	}
	return devs
}

func ParseConfig(fs *flag.FlagSet) {
	fs.BoolVar(&AscendHAMiVNPUEnable, "AscendHAMiVNPUEnable", false, "enable ascend device")
}

func (dev *AscendDevice) CommonWord() string {
	return dev.config.CommonWord
}

func (dev *AscendDevice) GetNodeDevices(n v1.Node) ([]*devices.DeviceInfo, error) {
	anno, ok := n.Annotations[dev.nodeRegisterAnno]
	if !ok {
		return []*devices.DeviceInfo{}, fmt.Errorf("annos not found %s", dev.nodeRegisterAnno)
	}
	nodeDevices, err := devices.UnMarshalNodeDevices(anno)
	if err != nil {
		klog.ErrorS(err, "failed to unmarshal node devices", "node", n.Name, "device annotation", anno)
		return []*devices.DeviceInfo{}, err
	}
	if len(nodeDevices) == 0 {
		klog.InfoS("no ascend device found", "node", n.Name, "device annotation", anno)
		return []*devices.DeviceInfo{}, errors.New("no device found on node")
	}
	return nodeDevices, nil
}

func (dev *AscendDevice) ResourceReqs(pod *v1.Pod) []devices.ContainerDeviceRequest {
	reqs := devices.ExtractResourceRequest(pod, dev.CommonWord(), dev.config.ResourceName, dev.config.ResourceMemoryName, "", "")
	for i := range reqs {
		req := &reqs[i]
		if req.Memreq == 0 && req.MemPercentagereq != 0 {
			req.Memreq = int32(dev.DeviceInfo.Devmem * req.MemPercentagereq / 100)
			klog.V(5).Infof("new memreq %d totalmem %d mempercentage %d", req.Memreq, dev.DeviceInfo.Devmem, req.MemPercentagereq)
		}
		if req.Memreq > 0 {
			m, _ := dev.trimMemory(int64(req.Memreq))
			klog.V(5).Infof("raw mem %d, trimed mem %d", req.Memreq, m)
			req.Memreq = int32(m)
		}
	}
	return reqs
}

func (ads *AscendDevices) CreateAnnotations(pod *v1.Pod, devList devices.PodSingleDevice) map[string]string {
	annotations := make(map[string]string)
	dev, err := ads.getFirstDevice()
	if err != nil {
		return annotations
	}
	commonWord := dev.CommonWord()

	annotations[devices.InRequestDevices[commonWord]] = devices.EncodePodSingleDevice(devList)
	annotations[devices.SupportDevices[commonWord]] = devices.EncodePodSingleDevice(devList)
	annotations["predicate-time"] = strconv.FormatInt(time.Now().Unix(), 10)
	allocateStr := fmt.Sprintf("huawei.com/%s", dev.CommonWord())
	var rtInfo []RuntimeInfo
	for _, dp := range devList {
		for _, val := range dp {
			_, temp := dev.trimMemory(int64(val.Usedmem))
			rtInfo = append(rtInfo, RuntimeInfo{
				UUID: val.UUID,
				Temp: temp,
			})
		}
	}
	s, err := json.Marshal(rtInfo)
	if err != nil {
		klog.ErrorS(err, "failed to marshal runtime info", "runtime info", rtInfo)
	}
	annotations[allocateStr] = string(s)

	return annotations
}

func (dev *AscendDevice) GetResourceNames() devices.ResourceNames {
	return devices.ResourceNames{
		ResourceCountName:  dev.config.ResourceName,
		ResourceMemoryName: dev.config.ResourceMemoryName,
		ResourceCoreName:   "",
	}
}

func CalScore(schedulePolicy string, dev_usage *devices.DeviceUsage, dev_info *devices.DeviceInfo) float64 {
	var score float64
	switch schedulePolicy {
	case binpackPolicy:
		score = binpackMultiplier * (float64(dev_usage.Usedmem) / float64(dev_info.Devmem))
	case spreadPolicy:
		if dev_usage.Used == 1 {
			score = spreadMultiplier
		}
	default:
		score = float64(0)
	}
	return score
}
