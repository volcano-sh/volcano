package vgpu4pd

import (
	v1 "k8s.io/api/core/v1"
)

func rmNodeDevice(devices *GPUDevices, node *v1.Node) {
	/*
		m.mutex.Lock()
		defer m.mutex.Unlock()
		if devices == nil || len(devices.Device) == 0 {
			return
		}
		klog.Infoln("before rm:", devices.Device, "needs remove", nodeInfo.Devices)
		tmp := make([]DeviceInfo, 0, len(m.nodes[nodeID].Devices)-len(nodeInfo.Devices))
		for _, val := range m.nodes[nodeID].Devices {
			found := false
			for _, rmval := range nodeInfo.Devices {
				if strings.Compare(val.ID, rmval.ID) == 0 {
					found = true
					break
				}
			}
			if !found && len(val.ID) > 0 {
				tmp = append(tmp, val)
			}
		}
		m.nodes[nodeID].Devices = tmp
		klog.Infoln("Rm Devices res:", m.nodes[nodeID].Devices)*/
}
