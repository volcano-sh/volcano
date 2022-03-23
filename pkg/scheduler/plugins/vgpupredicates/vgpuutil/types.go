/*
 * Copyright © 2021 peizhaoyou <peizhaoyou@4paradigm.com>
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package vgpuutil

const (
	//ResourceName = "nvidia.com/gpu"
	//ResourceName = "4pd.io/vgpu"
	AssignedTimeAnnotations = "4pd.io/vgpu-time"
	AssignedIDsAnnotations  = "4pd.io/vgpu-ids-new"
	AssignedNodeAnnotations = "4pd.io/vgpu-node"

	//Set default mem to 5000m
	//DefaultMem   = 5000
	//DefaultCores = 0

	DeviceLimit = 100
	//TimeLayout = "ANSIC"
	//DefaultTimeout = time.Second * 60
)

var (
	ResourceName  = VolcanovGPUNumber
	ResourceMem   = VolcanovGPUResource
	ResourceCores string
	DebugMode     bool
)

//type ContainerDevices struct {
//    Devices []string `json:"devices,omitempty"`
//}
//
//type PodDevices struct {
//    Containers []ContainerDevices `json:"containers,omitempty"`
//}
type ContainerDevice struct {
	UUID      string
	Usedmem   int32
	Usedcores int32
}

type ContainerDeviceRequest struct {
	Nums     int32
	Memreq   int32
	Coresreq int32
}

type ContainerDevices []ContainerDevice

type PodDevices []ContainerDevices

type NodeRemainDevice struct {
	UUID   string
	Idx    int
	Memory int32
	Cores  int32
}
