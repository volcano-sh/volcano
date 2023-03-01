package vgpu4pd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var kubeClient kubernetes.Interface

func init() {
	var err error
	kubeClient, err = NewClient()
	if err != nil {
		klog.Errorf("init kubeclient in 4pdvgpu failed", err.Error())
	} else {
		klog.Infoln("init kubeclient success")
	}
}

// NewClient connects to an API server
func NewClient() (kubernetes.Interface, error) {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig == "" {
		kubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, err
		}
	}
	client, err := kubernetes.NewForConfig(config)
	kubeClient = client
	return client, err
}

func PatchNodeAnnotations(node *v1.Node, annotations map[string]string) error {
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
		//Spec     patchSpec     `json:"spec,omitempty"`
	}

	p := patchPod{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = kubeClient.CoreV1().Nodes().
		Patch(context.Background(), node.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Infof("patch pod %v failed, %v", node.Name, err)
	}
	return err
}

func DecodeNodeDevices(name string, str string) *GPUDevices {
	if !strings.Contains(str, ":") {
		return nil
	}
	tmp := strings.Split(str, ":")
	retval := &GPUDevices{
		Name:   name,
		Device: make(map[int]*GPUDevice),
	}
	for index, val := range tmp {
		if strings.Contains(val, ",") {
			items := strings.Split(val, ",")
			count, _ := strconv.Atoi(items[1])
			devmem, _ := strconv.Atoi(items[2])
			health, _ := strconv.ParseBool(items[4])
			i := GPUDevice{
				ID:     index,
				UUID:   items[0],
				Number: uint(count),
				Memory: uint(devmem),
				Type:   items[3],
				Health: health,
			}
			retval.Device[index] = &i
		}
	}
	return retval
}

func checkVGPUResourcesInPod(pod *v1.Pod) bool {
	for _, container := range pod.Spec.Containers {
		_, ok := container.Resources.Limits[VolcanoVGPUMemory]
		if ok {
			return true
		}
		_, ok = container.Resources.Limits[VolcanoVGPUNumber]
		if ok {
			return true
		}
	}
	return false
}

func resourcereqs(pod *v1.Pod) (counts []ContainerDeviceRequest) {
	resourceName := v1.ResourceName(VolcanoVGPUNumber)
	resourceMem := v1.ResourceName(VolcanoVGPUMemory)
	resourceMemPercentage := v1.ResourceName(VolcanoVGPUMemoryPercentage)
	resourceCores := v1.ResourceName(VolcanoVGPUCores)
	counts = make([]ContainerDeviceRequest, len(pod.Spec.Containers))
	singledevice := false
	//Count Nvidia GPU
	for i := 0; i < len(pod.Spec.Containers); i++ {
		v, ok := pod.Spec.Containers[i].Resources.Limits[resourceName]
		if !ok {
			v, ok = pod.Spec.Containers[i].Resources.Limits[resourceMem]
			singledevice = true
		}
		if ok {
			n := int64(1)
			if !singledevice {
				n, _ = v.AsInt64()
			}
			memnum := 0
			mem, ok := pod.Spec.Containers[i].Resources.Limits[resourceMem]
			if !ok {
				mem, ok = pod.Spec.Containers[i].Resources.Requests[resourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int(memnums)
				}
			}
			mempnum := int32(101)
			mem, ok = pod.Spec.Containers[i].Resources.Limits[resourceMemPercentage]
			if !ok {
				mem, ok = pod.Spec.Containers[i].Resources.Requests[resourceMemPercentage]
			}
			if ok {
				mempnums, ok := mem.AsInt64()
				if ok {
					mempnum = int32(mempnums)
				}
			}
			if mempnum == 101 && memnum == 0 {
				mempnum = 100
			}
			corenum := 0
			core, ok := pod.Spec.Containers[i].Resources.Limits[resourceCores]
			if !ok {
				core, ok = pod.Spec.Containers[i].Resources.Requests[resourceCores]
			}
			if ok {
				corenums, ok := core.AsInt64()
				if ok {
					corenum = int(corenums)
				}
			}
			counts = append(counts, ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             DeviceName,
				Memreq:           int32(memnum),
				MemPercentagereq: int32(mempnum),
				Coresreq:         int32(corenum),
			})
		}
	}
	klog.Infoln("counts=", counts)
	return counts
}

func checkGPUtype(annos map[string]string, cardtype string) bool {
	inuse, ok := annos[GPUInUse]
	if ok {
		if !strings.Contains(inuse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(inuse)) {
				return true
			}
		} else {
			for _, val := range strings.Split(inuse, ",") {
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return true
				}
			}
		}
		return false
	}
	nouse, ok := annos[GPUNoUse]
	if ok {
		if !strings.Contains(nouse, ",") {
			if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(nouse)) {
				return true
			}
		} else {
			for _, val := range strings.Split(nouse, ",") {
				if strings.Contains(strings.ToUpper(cardtype), strings.ToUpper(val)) {
					return false
				}
			}
		}
		return true
	}
	return true
}

func checkType(annos map[string]string, d GPUDevice, n ContainerDeviceRequest) bool {
	//General type check, NVIDIA->NVIDIA MLU->MLU
	if !strings.Contains(d.Type, n.Type) {
		return false
	}
	if strings.Compare(n.Type, NvidiaGPUDevice) == 0 {
		return checkGPUtype(annos, d.Type)
	}
	klog.Infof("Unrecognized device", n.Type)
	return false
}

func getGPUDeviceSnapShot(snap *GPUDevices) *GPUDevices {
	ret := GPUDevices{
		Name:   snap.Name,
		Device: make(map[int]*GPUDevice),
	}
	for index, val := range snap.Device {
		ret.Device[index] = &GPUDevice{
			ID:       val.ID,
			UUID:     val.UUID,
			PodMap:   val.PodMap,
			Memory:   val.Memory,
			Number:   val.Number,
			Type:     val.Type,
			Health:   val.Health,
			UsedNum:  val.UsedNum,
			UsedMem:  val.UsedMem,
			UsedCore: val.UsedCore,
		}
	}
	return &ret
}

// checkNodeGPUSharingPredicate checks if a pod with gpu requirement can be scheduled on a node.
func checkNodeGPUSharingPredicate(pod *v1.Pod, gssnap *GPUDevices) (bool, []ContainerDevice, error) {
	// no gpu sharing request
	if !checkVGPUResourcesInPod(pod) {
		return true, []ContainerDevice{}, nil
	}
	ctrReq := resourcereqs(pod)
	if len(ctrReq) == 0 {
		return true, []ContainerDevice{}, nil
	}
	gs := getGPUDeviceSnapShot(gssnap)
	devs := []ContainerDevice{}
	for _, val := range ctrReq {
		if int(val.Nums) > len(gs.Device) {
			return false, []ContainerDevice{}, fmt.Errorf("no enough gpu cards on node %s", gs.Name)
		}
		klog.Infoln("Allocating device for container request", val)

		for i := len(gs.Device) - 1; i >= 0; i-- {
			klog.Info("Scoring pod ", val.Memreq, ":", val.MemPercentagereq, ":", val.Coresreq, ":", val.Nums, "i", i, "device:", gs.Device[i].ID)
			if gs.Device[i].Number <= uint(gs.Device[i].UsedNum) {
				continue
			}
			if val.MemPercentagereq != 101 && val.Memreq == 0 {
				val.Memreq = int32(gs.Device[i].Memory * uint(val.MemPercentagereq/100))
			}
			if gs.Device[i].Memory-gs.Device[i].UsedMem < uint(val.Memreq) {
				continue
			}
			if 100-gs.Device[i].UsedCore < uint(val.Coresreq) {
				continue
			}
			// Coresreq=100 indicates it want this card exclusively
			if val.Coresreq == 100 && gs.Device[i].UsedNum > 0 {
				continue
			}
			// You can't allocate core=0 job to an already full GPU
			if gs.Device[i].UsedCore == 100 && val.Coresreq == 0 {
				continue
			}
			if !checkType(pod.Annotations, *gs.Device[i], val) {
				continue
			}
			//total += gs.Devices[i].Count
			//free += node.Devices[i].Count - node.Devices[i].Used
			if val.Nums > 0 {
				klog.Infoln("device", gs.Device[i].ID, "fitted")
				val.Nums--
				gs.Device[i].UsedNum++
				gs.Device[i].UsedMem += uint(val.Memreq)
				gs.Device[i].UsedCore += uint(val.Coresreq)
				devs = append(devs, ContainerDevice{
					UUID:      gs.Device[i].UUID,
					Type:      val.Type,
					Usedmem:   val.Memreq,
					Usedcores: val.Coresreq,
				})
			}
			if val.Nums == 0 {
				break
			}
		}
		if val.Nums > 0 {
			return false, []ContainerDevice{}, fmt.Errorf("not enough gpu fitted on this node")
		}
	}
	return true, devs, nil
}
