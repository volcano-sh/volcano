## 1. Build device relationships using the official NVML library

[github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml](http://github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml)

```
// github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml.go
type DeviceStatus struct {
	Power       *uint
	FanSpeed    *uint
	Temperature *uint
	Utilization UtilizationInfo
	Memory      MemoryInfo
	Clocks      ClockInfo
	PCI         PCIStatusInfo
	Processes   []ProcessInfo
	Throttle    ThrottleReason
	Performance PerfState
}
```

### retrieve device topo using NVML library

```
// device type
const (
	P2PLinkUnknown P2PLinkType = iota
	P2PLinkCrossCPU
	P2PLinkSameCPU
	P2PLinkHostBridge
	P2PLinkMultiSwitch
	P2PLinkSingleSwitch
	P2PLinkSameBoard
	SingleNVLINKLink
	TwoNVLINKLinks
	ThreeNVLINKLinks
	FourNVLINKLinks
	FiveNVLINKLinks
	SixNVLINKLinks
	SevenNVLINKLinks
	EightNVLINKLinks
	NineNVLINKLinks
	TenNVLINKLinks
	ElevenNVLINKLinks
	TwelveNVLINKLinks
)
func GetP2PLink(dev1, dev2 *Device) (link P2PLinkType, err error) {
	level, err := deviceGetTopologyCommonAncestor(dev1.handle, dev2.handle)
	if err != nil || level == nil {
		return P2PLinkUnknown, err
	}

	switch *level {
	case C.NVML_TOPOLOGY_INTERNAL:
		link = P2PLinkSameBoard
	case C.NVML_TOPOLOGY_SINGLE:
		link = P2PLinkSingleSwitch
	case C.NVML_TOPOLOGY_MULTIPLE:
		link = P2PLinkMultiSwitch
	case C.NVML_TOPOLOGY_HOSTBRIDGE:
		link = P2PLinkHostBridge
	case C.NVML_TOPOLOGY_CPU:
		link = P2PLinkSameCPU
	case C.NVML_TOPOLOGY_SYSTEM:
		link = P2PLinkCrossCPU
	default:
		err = ErrUnsupportedP2PLink
	}
	return
}

func GetNVLink(dev1, dev2 *Device) (link P2PLinkType, err error) {
	nvbusIds1, err := dev1.handle.deviceGetAllNvLinkRemotePciInfo()
	if err != nil || nvbusIds1 == nil {
		return P2PLinkUnknown, err
	}

	nvlink := P2PLinkUnknown
	for _, nvbusID1 := range nvbusIds1 {
		if *nvbusID1 == dev2.PCI.BusID {
			switch nvlink {
			case P2PLinkUnknown:
				nvlink = SingleNVLINKLink
			case SingleNVLINKLink:
				nvlink = TwoNVLINKLinks
			case TwoNVLINKLinks:
				nvlink = ThreeNVLINKLinks
			case ThreeNVLINKLinks:
				nvlink = FourNVLINKLinks
			case FourNVLINKLinks:
				nvlink = FiveNVLINKLinks
			case FiveNVLINKLinks:
				nvlink = SixNVLINKLinks
			case SixNVLINKLinks:
				nvlink = SevenNVLINKLinks
			case SevenNVLINKLinks:
				nvlink = EightNVLINKLinks
			case EightNVLINKLinks:
				nvlink = NineNVLINKLinks
			case NineNVLINKLinks:
				nvlink = TenNVLINKLinks
			case TenNVLINKLinks:
				nvlink = ElevenNVLINKLinks
			case ElevenNVLINKLinks:
				nvlink = TwelveNVLINKLinks
			}
		}
	}

	// TODO(klueska): Handle NVSwitch semantics
	return nvlink, nil
}
```

### retrieve topo type between two cards

```
//bindings/go/nvml/bindings.go
func deviceGetTopologyCommonAncestor(h1, h2 handle) (*uint, error) {
	r := dl.lookupSymbol("nvmlDeviceGetTopologyCommonAncestor")
	if r == C.NVML_ERROR_FUNCTION_NOT_FOUND {
		return nil, nil
	}

	var level C.nvmlGpuTopologyLevel_t
	r = C.nvmlDeviceGetTopologyCommonAncestor(h1.dev, h2.dev, &level)
	if r == C.NVML_ERROR_NOT_SUPPORTED {
		return nil, nil
	}

	return uintPtr(C.uint(level)), errorString(r)
}
```

## 2. Build topo trees from bottom to up using Mask

### The structure of GPU Node:

```
//SchedulerCache contains allocatable resource of GPU
type SchedulerCache struct {
	Cores  int64
	Memory int64
}

//DeviceMeta contains metadata of GPU device
type DeviceMeta struct {
	ID          int
	MinorID     int
	UsedMemory  uint64
	TotalMemory uint64
	Pids        []uint
	BusId       string
	Utilization uint
	UUID        string
}

type NvidiaNode struct {
	Meta            DeviceMeta
	AllocatableMeta SchedulerCache

	Parent   *NvidiaNode
	Children []*NvidiaNode
	Mask     uint32

	pendingReset bool
	vchildren    map[int]*NvidiaNode
	ntype        nvml.GpuTopologyLevel
	tree         *NvidiaTree
}
```

`Mask` matters most which represent which GPU node is usable under the Node.

The ith position of `Mask` is 1, meaning the ith GPU Node is usable.  (if 0, not usable).

### Retrieve usable leaf nodes(GPU Nodes) under some node

```
//GetAvailableLeaves returns leaves of this NvidiaNode
//which available for allocating.
func (n *NvidiaNode) GetAvailableLeaves() []*NvidiaNode {
	var leaves []*NvidiaNode

	mask := n.Mask

	for mask != 0 {
		id := uint32(bits.TrailingZeros32(mask))
		klog.V(2).Infof("Pick up %d mask %b", id, n.tree.leaves[id].Mask)
		leaves = append(leaves, n.tree.leaves[id])
		mask ^= one << id
	}

	return leaves
}
```

### GPU Tree

```
//NvidiaTree represents a Nvidia GPU in a tree.
type NvidiaTree struct {
	sync.Mutex

	root   *NvidiaNode
	leaves []*NvidiaNode

	realMode     bool
	query        map[string]*NvidiaNode
	index        int
	samplePeriod time.Duration
}
```

The key process to **initialize the Nvidia Tree** is **parseFromLibrary** function

```
func (t *NvidiaTree) parseFromLibrary() error {
	if err := nvml.Init(); err != nil {
		return err
	}

	defer nvml.Shutdown()

	num, err := nvml.DeviceGetCount()
	if err != nil {
		return err
	}

	klog.V(2).Infof("Detect %d gpu cards", num)

	nodes := make(LevelMap)
	t.leaves = make([]*NvidiaNode, num)

	for i := 0; i < int(num); i++ {
		dev, _ := nvml.DeviceGetHandleByIndex(uint(i))
		_, _, totalMem, _ := dev.DeviceGetMemoryInfo()
		pciInfo, _ := dev.DeviceGetPciInfo()
		minorID, _ := dev.DeviceGetMinorNumber()
		uuid, _ := dev.DeviceGetUUID()

		n := t.allocateNode(i)
		n.AllocatableMeta.Cores = HundredCore
		n.AllocatableMeta.Memory = int64(totalMem)
		n.Meta.TotalMemory = totalMem
		n.Meta.BusId = pciInfo.BusID
		n.Meta.MinorID = int(minorID)
		n.Meta.UUID = uuid

		t.addNode(n)
	}

	for cardA := uint(0); cardA < num; cardA++ {
		devA, _ := nvml.DeviceGetHandleByIndex(cardA)
		for cardB := cardA + 1; cardB < num; cardB++ {
			devB, _ := nvml.DeviceGetHandleByIndex(cardB)
			ntype, err := nvml.DeviceGetTopologyCommonAncestor(devA, devB)
			if err != nil {
				return err
			}

			multi, err := devA.DeviceGetMultiGpuBoard()
			if err != nil {
				return err
			}

			if multi > 0 && ntype == nvml.TOPOLOGY_INTERNAL {
				ntype = nvml.TOPOLOGY_SINGLE
			}

			if newNode := t.join(nodes, ntype, int(cardA), int(cardB)); newNode != nil {
				klog.V(2).Infof("New node, type %d, mask %b", int(ntype), newNode.Mask)
				nodes[ntype] = append(nodes[ntype], newNode)
			}
		}
	}

	for t, ns := range nodes {
		klog.V(2).Infof("type: %d, len %d", int(t), len(ns))
	}
	
// join operation has not built the connection, but only create some nodes(virtual nodes and leaf nodes), so buildTree function takes the responsiblity of building the tree.
	t.buildTree(nodes)
	return nil
}
```

We can retrieve the topo type between two cards using `nvml.DeviceGetTopologyCommonAncestor(devA, devB)`, then build the non-leaf nodes of `GPUTree`.

```
//bindings/go/nvml/bindings.go
func deviceGetTopologyCommonAncestor(h1, h2 handle) (*uint, error) {
	r := dl.lookupSymbol("nvmlDeviceGetTopologyCommonAncestor")
	if r == C.NVML_ERROR_FUNCTION_NOT_FOUND {
		return nil, nil
	}

	var level C.nvmlGpuTopologyLevel_t
	r = C.nvmlDeviceGetTopologyCommonAncestor(h1.dev, h2.dev, &level)
	if r == C.NVML_ERROR_NOT_SUPPORTED {
		return nil, nil
	}

	return uintPtr(C.uint(level)), errorString(r)
}
func (t *NvidiaTree) buildTree(nodes LevelMap) {
	// Create connections
	for _, cur := range t.leaves {
		level := int(nvml.TOPOLOGY_SINGLE)
		self := cur

		for {
			for _, upperNode := range nodes[nvml.GpuTopologyLevel(level)] {
				if (upperNode.Mask & self.Mask) != 0 {
					self.setParent(upperNode)
					self = upperNode
					break
				}
			}

			level += levelStep

			if level > int(nvml.TOPOLOGY_SYSTEM) {
				break
			}
		}
	}

	// Find the root level
	var firstLevel []*NvidiaNode
	level := int(nvml.TOPOLOGY_SYSTEM)

	t.root = NewNvidiaNode(t)
	t.root.Parent = nil
	for level > 0 {
		if len(nodes[nvml.GpuTopologyLevel(level)]) == 0 {
			level -= levelStep
			continue
		}

		firstLevel = nodes[nvml.GpuTopologyLevel(level)]
		break
	}

	if len(firstLevel) == 0 {
		klog.Errorf("No topology level found at %d", level)

		if len(t.leaves) == 1 {
			klog.Infof("Only one card topology")
			t.root.Mask |= t.leaves[0].Mask
			t.leaves[0].setParent(t.root)

			t.root.Children = append(t.root.Children, t.leaves[0])
			return
		}

		klog.Fatalf("Should not reach here")
	}

	for _, n := range firstLevel {
		t.root.Mask |= n.Mask
		n.setParent(t.root)
	}

	// Transform vchildren to children
	for _, n := range t.leaves {
		cur := n.Parent

		for cur != nil {
			if len(cur.Children) == 0 {
				cur.Children = make([]*NvidiaNode, 0)

				for _, child := range cur.vchildren {
					cur.Children = append(cur.Children, child)
				}
			}

			cur = cur.Parent
		}
	}
}
```

![Failure](https://gateway.pinata.cloud/ipfs/QmNo9ASGcZgHRSDocrVHaXXZtjM11SPjEsLzDr2Fus2HDD?preview=1)

There may exist some problems e.g. for one specific leaf node, the program may break the loop if one level has more than one parent nodes which leads to nodes being missed.

### 3. Maintain the usable GPU nodes in Topo Trees using Pod Informer

Watch pod events using **Pod Informer** (mainly focusing on `delete` and `update` events of pods of corresponding  GPU resources). The controller runs in the main loop of the device plugin. Then we will deal with the handlers of these two events.

**delete handler**

```
func (c *controller) deletePodFunc(obj interface{}) {
	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	case clientgocache.DeletedFinalStateUnknown:
		var ok bool
		pod, ok = t.Obj.(*v1.Pod)
		if !ok {
			klog.Warningf("cannot convert to *v1.Pod: %v", t.Obj)
			return
		}
	default:
		klog.Warningf("cannot convert to *v1.Pod: %v", t)
		return
	}

	delDevs := strings.Split(pod.Annotations[resourceName], ",")
	klog.V(2).Infof("delete pod %s in ns %s, deleted devs: %v", pod.Name, pod.Namespace, delDevs)
	if err := c.devicePlugin.UpdatePodDevice(nil, delDevs); err != nil {
		klog.Errorf("Failed to update PCI device: %v", err)
	}
	return
}
```

**Delete handler** mainly retrieves **device ids** of corresponding resources from **Annotations** of the pod. And update the topo trees by **UpdatePodDevice** function to maintain usable GPU nodes in topo trees.

**update handler**

```
func (c *controller) updatePodFunc(o, obj interface{}) {

	var pod *v1.Pod
	switch t := obj.(type) {
	case *v1.Pod:
		pod = t
	default:
		klog.Warningf("cannot convert to *v1.Pod: %v", t)
		return
	}
	klog.V(2).Infof("add pod[%v]", pod.UID)
	var kubeletDeviceManagerCheckpoint = filepath.Join(pluginapi.DevicePluginPath, "kubelet_internal_checkpoint")
	registeredDevs := make(map[string][]string)
	devEntries := make([]checkpoint.PodDevicesEntry, 0)
	cp := checkpoint.New(devEntries, registeredDevs)
	blob, err := ioutil.ReadFile(kubeletDeviceManagerCheckpoint)
	if err != nil {
		klog.Errorf("Failed to read content from %s: %v", kubeletDeviceManagerCheckpoint, err)
		return
	}
	err = cp.UnmarshalCheckpoint(blob)
	if err != nil {
		klog.Errorf("Failed to unmarshal content: %v", err)
		return
	}
	var env = []string{}
	data, _ := cp.GetData()
	for _, pde := range data {
		if pde.PodUID != string(pod.UID) {
			continue
		}
		for _, devID := range pde.DeviceIDs {
			if val, ok := c.devicePlugin.shadowMap[devID]; ok && val != "" {
				env = append(env, val)
				delete(c.devicePlugin.shadowMap, devID)
			}
		}
	}
	klog.V(2).Infof("Pod[%v] want to be updated: %v", pod.UID, env)
	if len(env) == 0 {
		return
	}
	old := pod.DeepCopy()
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string, 0)
	}
	pod.Annotations[resourceName] = strings.Join(env, ",")
	// update pod annotation
	err = patchPodObject(c.clientset, old, pod)
	if err != nil {
		klog.Error(err)
	}
}
```

**Update handler** mainly filters out the GPUs used by pods in current events from kubelet_internal_checkpoint file of kubelet, then patch to Annotations of Pod. Here we do not update the topo trees because we already finished it during Allocate operation of device plugin.

### 4. Differentiate the allocation process of GPU by one card / multiple cards situations.

The core of allocating GPU logic is implemented in Allocate function of device plugin.

**Allocate**

```
// Allocate which return list of devices.
func (m *NvidiaDevicePlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	devs := m.devs
	responses := pluginapi.AllocateResponse{}
	for _, req := range reqs.ContainerRequests {
		klog.V(2).Infof("request device IDs: %v", req.DevicesIDs)
		topoDevs := m.findBestDevice(resourceName, len(req.DevicesIDs))
		if len(topoDevs) == 0 {
			topoDevs = req.DevicesIDs
		}
		klog.V(2).Infof("find best device IDs: %v", topoDevs)
		response := pluginapi.ContainerAllocateResponse{
			Envs: map[string]string{
				"NVIDIA_VISIBLE_DEVICES": strings.Join(topoDevs, ","),
			},
			Annotations: map[string]string{
				resourceName: strings.Join(topoDevs, ","),
			},
		}

		for i, id := range req.DevicesIDs {
			if !deviceExists(devs, id) {
				return nil, fmt.Errorf("invalid allocation request: unknown device: %s", id)
			}
			m.shadowMap[id] = topoDevs[i]
		}

		responses.ContainerResponses = append(responses.ContainerResponses, &response)
		m.UpdatePodDevice(topoDevs, nil)
	}
	klog.Infof("Allocate response: %#v", responses)
	return &responses, nil
}
```

**Allocate Funtion** can be divided into **two steps**:

1. Find  len(req.DevicesIDs) GPU devices using **findBestDevice** function to cover req.DevicesIDs.
2. Update topo trees using **UpdatePodDevice** funtion.

**UpdatePodDevice** has been introduced above, then we will introduce **findBestDevice** function.

```
func (dp *NvidiaDevicePlugin) findBestDevice(t string, n int) []string {
	devs := []string{}
	switch t {
	case resourceName:
		// XXX: we divide the user's request into two parts:
		// a. request 1 GPU card, select the best 1 GPU card, make sure the left GPU cards will be most valuable
		// b. request more than 1 GPU card, based on the score of the least enough leaves branch
		if n == 1 {
			// request 1 GPU card, select the best 1 GPU card,
			// make sure the left GPU cards will be most valuable
			devs = append(devs, find1GPUDevice(dp.root))
		} else {
			// find the least enough leaves node
			// find the higher score when the two nodes have same number leaves
			// add the leaves into the result
			devs = append(devs, findNGPUDevice(dp.root, n)...)
		}
		return devs
	}

	return devs
}
// find1GPUDevice: travese all nodes，find single GPU node has the minimum score
func find1GPUDevice(root *pciDevice) string {
	// if the current node has maximum GPU devices, select the first one
	// else find the one to make sure left GPU devices have highest score
	// XXX: consider GPU connect type
	if root == nil {
		return ""
	}
	if root.dev.Attributes.OSDevType == topology.HwlocObjOSDevGPU {
		return root.nvidiaUUID
	}
	var min = float64(1 << 10)
	var minDev *pciDevice
	for _, child := range root.children {
		if child.availDevices == 0 {
			continue
		}
		if child.score < min {
			min = child.score
			minDev = child
		}
	}
	return find1GPUDevice(minDev)
}
// findNGPUDevice：find nodes with the maximum score which also satisfy the condition that the number of usable nodes is minimized but more than n, then traverse the nodes, return usable leaf GPU nodes.
// Attention: filterd nodes may not have n usable nodes. Here we ignore the situation.
func findNGPUDevice(root *pciDevice, n int) []string {
	var max float64
	var queue = []*pciDevice{root}
	var tmp = []*pciDevice{}
	for len(queue) > 0 {
		l := len(queue)
		max = 0
		for i := 0; i < l; i++ {
			if queue[i].availDevices < n {
				continue
			}
			if queue[i].score > max {
				max = queue[i].score
			}
		}
		if max == 0 {
			break
		} else {
			tmp = []*pciDevice{}
		}
		for i := 0; i < l; i++ {
			if queue[i].score < max {
				continue
			}
			if queue[i].score == max {
				tmp = append(tmp, queue[i])
			}
			for _, c := range queue[i].children {
				if c.availDevices == 0 {
					continue
				}
				queue = append(queue, c)
			}
		}
		queue = queue[l:]
	}
	var res = []string{}
	for _, pci := range tmp {
		res = append(res, pci.getAvailableGPUs()...)
		if len(res) == n {
			break
		}
	}
	return res
}
```

### 5. Patch topo info on nodes and update regularly, and implement corresponding schedule plugin.

We patch all current topo nodes(including topo connection relationships between two nodes) to node in main loop of device plugin.

```
// RegisterToSched register the nvml link info to extender sched
func (m *NvidiaDevicePlugin) RegisterToSched(kubeClient *kubernetes.Clientset, endpoint string) error {
	var t = &Topology{
		GPUDevice: make([]*nvml.Device, len(m.devs)),
	}
	copy(t.GPUDevice, m.devs)
	topo, err := json.Marshal(t)
	if err != nil {
		return err
	}
	klog.V(2).Infof("System topology: %s", string(topo))

	if endpoint != "" {
		// TODO register the node topology to sched server by endpoint
	}

	if err := patchNode(kubeClient, m.nodeName, func(n *corev1.Node) {
		n.Annotations[resourceName] = string(topo)
	}); err != nil {
		klog.Warningf("Failed to patch GPU topology to the node %s, %v", m.nodeName, err)
		return err
	}
	return nil
}
```

![Result](https://gateway.pinata.cloud/ipfs/QmQHc91fjiuJdnsVVfEGjFhP4aYjFxTDYaorniEUoUkYFV?preview=1)

<iframe class="embed" src="https://gateway.pinata.cloud/ipfs/QmQHc91fjiuJdnsVVfEGjFhP4aYjFxTDYaorniEUoUkYFV?preview=1" contenteditable="false"></iframe>

## 6. Problems

### 6.1 fail to build tree - Cut apart

![Problem 1](https://gateway.pinata.cloud/ipfs/QmNo9ASGcZgHRSDocrVHaXXZtjM11SPjEsLzDr2Fus2HDD?preview=1)
