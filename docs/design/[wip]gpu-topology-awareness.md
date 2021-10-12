# Report on GPU Topology Awareness Based on Device Plugin Mechanism

<center>吴建博（牧瑜）</center>
<center>16th August 2021 - 30th September 2021</center>

选型：

* 计划选用官方的 github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml 库来构建设备之间的关系，功能比较全

* 自下而上通过 mask 构建拓扑树  
* 通过 pod informer 来维护拓扑树中 gpu 节点的可用情况
* GPU 分配过程按照单卡/多卡区分不同的算法
* 在 Node 上 patch 上拓扑信息**并定时更新**，同时实现相应调度插件

### 1、选用官方的 nvml 库来构建设备之间的关系

```go
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

nvml获取设备拓扑结构

```go
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
```



```go
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

得到两张卡之间的拓扑类型

```go
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



### 2、自下而上通过 mask 构建拓扑树

GPU Node 的结构体如下：

```go
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

其中比较重要的是 `Mask`，表示该 Node 下可用的 GPU 节点(第 i 位为 1 的话表示第 i 个 GPU 节点可用)。

获取某节点下可用的叶子节点(GPU 节点)：

```go
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



GPU Tree 结构体如下：

```go
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



初始化 NvidiaTree 关键操作就是 parseFromLibrary 函数：

```go
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
	
    // join 未建立连接关系，只是创建了一堆节点(虚拟节点和叶子节点)，所以构建树的操作都放在了 buildTree
	t.buildTree(nodes)

	return nil
}


```

在程序中可以由` nvml.DeviceGetTopologyCommonAncestor(devA, devB) `得到两张卡之间的拓扑类型，进而构造` GPUTree` 的非叶子节点。

```go
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



```go
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

<img src="C:\Users\realPolymath\Downloads\未命名文件 (5).png" alt="未命名文件 (5)" style="zoom:50%;" />

该buildTree实现方案可能会出现这样的问题：针对一个叶子节点来说，其可能在某一层具有超过多余一个的父亲节点，但是在12行break掉，导致出现遗漏节点的情况，故需要进行修改。



### 3、通过 pod informer 来维护拓扑树中 gpu 节点的可用情况

通过 informer 监听 pod 事件(主要监听声明对应 gpu 资源 pods 的 delete 和 update 事件)，这个 controller 在 device plugin 的主循环中运行。这里着重分析下上述两个事件的 Handler。

**delete handler**

```go
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

删除 handler 主要从 pod 的 Annotations 中获取对应资源的设备 ids，然后通过 UpdatePodDevice 去更新拓扑树，维护拓扑树中可用的 GPU 节点。

**update handler**

```go
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

更新 handler 主要从 kubelet 的 kubelet_internal_checkpoint 文件中过滤出当前事件中 pod 的使用 gpus，然后 patch 到 pod 的 Annotations 上，这里并没有去更新拓扑树是因为这一步在 device plugin Allocate 的时候去做了。



### 4、GPU 分配过程按照单卡/多卡区分不同的算法

核心的分配 GPU 逻辑在 device plugin 的 Allocate 函数中实现。

**Allocate**

```go
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

可以看到核心有两步：

* 通过 findBestDevice 函数找到 len(req.DevicesIDs) 个 GPU 设备来覆盖 req.DevicesIDs。
* 通过 UpdatePodDevice 去更新拓扑树。


UpdatePodDevice 上述已经介绍过，主要来看 findBestDevice 函数。

```go
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
```

```go
// find1GPUDevice: 遍历所有节点，找到分数最低的单个 GPU 节点
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
```

```go
// findNGPUDevice: 找到分数最高的，且满足可用节点最少但是 >=n 的节点，然后遍历这些符合条件的节点，返回其可用的叶子 GPU 节点(这里其实有个问题，过滤出来的节点不一定可用节点就等于 n，这里没考虑这种情况)
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

### 5、在 Node 上 patch 上拓扑信息**并定时更新**，同时实现相应调度插件

在 device plugin 的主循环中，将当前的所有拓扑节点(包括两两之间的拓扑连接关系)都 patch 到了 node 上。

```go
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

node patch 了设备之间的拓扑信息，但调度层面没有看到相关的优化项目



最终结果图如下：

<img src="C:\Users\realPolymath\Downloads\按表画 (5).png" alt="按表画 (5)" style="zoom:50%;" />



### 6、 存在的问题

#### 6.1 buildTree构树失败 割裂

<img src="C:\Users\realPolymath\Downloads\按表画 (6).png" alt="按表画 (6)" style="zoom:50%;" />





