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

package devices

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
)

type DeviceInfo struct {
	ID              string          `json:"id,omitempty"`
	Index           uint            `json:"index,omitempty"`
	Count           int32           `json:"count,omitempty"`
	Devmem          int32           `json:"devmem,omitempty"`
	Devcore         int32           `json:"devcore,omitempty"`
	Type            string          `json:"type,omitempty"`
	Numa            int             `json:"numa,omitempty"`
	Mode            string          `json:"mode,omitempty"`
	Health          bool            `json:"health,omitempty"`
	DeviceVendor    string          `json:"devicevendor,omitempty"`
	CustomInfo      map[string]any  `json:"custominfo,omitempty"`
	DevicePairScore DevicePairScore `json:"devicepairscore,omitempty"`
}

type DevicePairScores []DevicePairScore
type DevicePairScore struct {
	ID     string         `json:"uuid,omitempty"`
	Scores map[string]int `json:"score,omitempty"`
}

type DeviceUsage struct {
	Used      int32
	Usedmem   int32
	Usedcores int32
}

type ResourceNames struct {
	ResourceCountName  string
	ResourceMemoryName string
	ResourceCoreName   string
}

type ContainerDevice struct {
	// TODO current Idx cannot use, because EncodeContainerDevices method not encode this filed.
	Idx        int
	UUID       string
	Type       string
	Usedmem    int32
	Usedcores  int32
	CustomInfo map[string]any
}

type ContainerDeviceRequest struct {
	Nums             int32
	Type             string
	Memreq           int32
	MemPercentagereq int32
	Coresreq         int32
}

const (
	// OneContainerMultiDeviceSplitSymbol this is when one container use multi device, use : symbol to join device info.
	OneContainerMultiDeviceSplitSymbol = ":"

	// OnePodMultiContainerSplitSymbol this is when one pod having multi container and more than one container use device, use ; symbol to join device info.
	OnePodMultiContainerSplitSymbol = ";"
)

var (
	HandshakeAnnos   = map[string]string{}
	InRequestDevices = map[string]string{}
	SupportDevices   = map[string]string{}
)

type ContainerDevices []ContainerDevice
type ContainerDeviceRequests map[string]ContainerDeviceRequest
type PodDeviceRequests []ContainerDeviceRequests
type PodSingleDevice []ContainerDevices
type PodDevices map[string]PodSingleDevice

func CheckHealth(devType string, n *corev1.Node) (bool, bool) {
	handshake := n.Annotations[HandshakeAnnos[devType]]
	if strings.Contains(handshake, "Requesting") {
		formertime, _ := time.Parse(time.DateTime, strings.Split(handshake, "_")[1])
		return time.Now().Before(formertime.Add(time.Second * 60)), false
	} else if strings.Contains(handshake, "Deleted") {
		return true, false
	} else {
		return true, true
	}
}

func UnMarshalNodeDevices(str string) ([]*DeviceInfo, error) {
	var dlist []*DeviceInfo
	err := json.Unmarshal([]byte(str), &dlist)
	return dlist, err
}

func EncodeContainerDevices(cd ContainerDevices) string {
	tmp := ""
	for _, val := range cd {
		tmp += val.UUID + "," + val.Type + "," + strconv.Itoa(int(val.Usedmem)) + "," + strconv.Itoa(int(val.Usedcores)) + OneContainerMultiDeviceSplitSymbol
	}
	klog.Infof("Encoded container Devices: %s", tmp)
	return tmp
	//return strings.Join(cd, ",")
}

func EncodePodSingleDevice(pd PodSingleDevice) string {
	res := ""
	for _, ctrdevs := range pd {
		res = res + EncodeContainerDevices(ctrdevs)
		res = res + OnePodMultiContainerSplitSymbol
	}
	klog.Infof("Encoded pod single devices %s", res)
	return res
}

func DecodeContainerDevices(str string) (ContainerDevices, error) {
	if len(str) == 0 {
		return ContainerDevices{}, nil
	}
	cd := strings.Split(str, OneContainerMultiDeviceSplitSymbol)
	contdev := ContainerDevices{}
	tmpdev := ContainerDevice{}
	klog.V(5).Infof("Start to decode container device %s", str)
	for _, val := range cd {
		if strings.Contains(val, ",") {
			//fmt.Println("cd is ", val)
			tmpstr := strings.Split(val, ",")
			if len(tmpstr) < 4 {
				return ContainerDevices{}, fmt.Errorf("pod annotation format error; information missing, please do not use nodeName field in task")
			}
			tmpdev.UUID = tmpstr[0]
			tmpdev.Type = tmpstr[1]
			devmem, _ := strconv.ParseInt(tmpstr[2], 10, 32)
			tmpdev.Usedmem = int32(devmem)
			devcores, _ := strconv.ParseInt(tmpstr[3], 10, 32)
			tmpdev.Usedcores = int32(devcores)
			contdev = append(contdev, tmpdev)
		}
	}
	klog.V(5).Infof("Finished decoding container devices. Total devices: %d", len(contdev))
	return contdev, nil
}

func DecodePodDevices(checklist map[string]string, annos map[string]string) (PodDevices, error) {
	klog.V(5).Infof("checklist is [%+v], annos is [%+v]", checklist, annos)
	if len(annos) == 0 {
		return PodDevices{}, nil
	}
	pd := make(PodDevices)
	for devID, devs := range checklist {
		str, ok := annos[devs]
		if !ok {
			continue
		}
		pd[devID] = make(PodSingleDevice, 0)
		for _, s := range strings.Split(str, OnePodMultiContainerSplitSymbol) {
			cd, err := DecodeContainerDevices(s)
			if err != nil {
				return PodDevices{}, err
			}
			if len(cd) == 0 {
				continue
			}
			pd[devID] = append(pd[devID], cd)
		}
	}
	klog.V(5).InfoS("Decoded pod annos", "poddevices", pd)
	return pd, nil
}
