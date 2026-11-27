package util

import (
	"sync"

	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/dynamic-resource-allocation/structured"
	"k8s.io/utils/ptr"
)

// nodeSlices lazily indexes PreFilter's ResourceSlices by node on first use,
// avoiding the indexing cost when preemption never runs. Each index is built
// once and only read afterwards, so concurrent Filter calls need no locking.
type nodeSlices struct {
	slices []*resourceapi.ResourceSlice

	indexOnce sync.Once
	// byPool has all slices of each pool, of every generation. nodePools
	// has the pools with a slice for a node via spec.nodeName, sharedPools
	// those with a slice without spec.nodeName.
	byPool      map[poolID][]*resourceapi.ResourceSlice
	nodePools   map[string]sets.Set[poolID]
	sharedPools sets.Set[poolID]

	// devicesOnce is separate from indexOnce because Filter on a node with a
	// hold only needs forNode, and building the devices of every node there
	// would be wasted.
	devicesOnce sync.Once
	// localDevicesByNode holds the IDs of the devices restricted to each
	// node, either via slice-level spec.nodeName or device-level nodeName
	// under PerDeviceNodeSelection, in the latest generation of each pool.
	localDevicesByNode map[string]sets.Set[structured.DeviceID]
}

type poolID struct {
	driver, pool string
}

func (s *nodeSlices) buildIndex() {
	s.indexOnce.Do(func() {
		s.byPool = make(map[poolID][]*resourceapi.ResourceSlice)
		s.nodePools = make(map[string]sets.Set[poolID])
		s.sharedPools = sets.New[poolID]()
		for _, slice := range s.slices {
			id := poolID{driver: slice.Spec.Driver, pool: slice.Spec.Pool.Name}
			s.byPool[id] = append(s.byPool[id], slice)
			nodeName := ptr.Deref(slice.Spec.NodeName, "")
			if nodeName == "" {
				s.sharedPools.Insert(id)
				continue
			}
			if s.nodePools[nodeName] == nil {
				s.nodePools[nodeName] = sets.New[poolID]()
			}
			s.nodePools[nodeName].Insert(id)
		}
	})
}

// forNode returns all slices of the pools which have a slice for nodeName or a
// slice without spec.nodeName. Pools are returned whole because the allocator
// checks the completeness and the generation of a pool against all of its
// slices. Leaving out the pools of other nodes keeps per-node allocators small.
func (s *nodeSlices) forNode(nodeName string) []*resourceapi.ResourceSlice {
	s.buildIndex()
	var result []*resourceapi.ResourceSlice
	for id := range s.nodePools[nodeName].Union(s.sharedPools) {
		result = append(result, s.byPool[id]...)
	}
	return result
}

// buildLocalDevices build the local devices only of the nodes being checked (nodePools plus
// PerDeviceNodeSelection slices) instead of every node.
func (s *nodeSlices) buildLocalDevices() {
	s.devicesOnce.Do(func() {
		// The allocator only uses each pool's latest generation, so ignore
		// older slices.
		latest := make(map[poolID]int64)
		for _, slice := range s.slices {
			id := poolID{driver: slice.Spec.Driver, pool: slice.Spec.Pool.Name}
			latest[id] = max(latest[id], slice.Spec.Pool.Generation)
		}
		s.localDevicesByNode = make(map[string]sets.Set[structured.DeviceID])
		for _, slice := range s.slices {
			if slice.Spec.Pool.Generation < latest[poolID{driver: slice.Spec.Driver, pool: slice.Spec.Pool.Name}] {
				continue
			}
			sliceNodeName := ptr.Deref(slice.Spec.NodeName, "")
			if sliceNodeName == "" && !ptr.Deref(slice.Spec.PerDeviceNodeSelection, false) {
				continue
			}
			for _, device := range slice.Spec.Devices {
				nodeName := sliceNodeName
				if nodeName == "" {
					nodeName = ptr.Deref(device.NodeName, "")
				}
				if nodeName == "" {
					continue
				}
				devices := s.localDevicesByNode[nodeName]
				if devices == nil {
					devices = sets.New[structured.DeviceID]()
					s.localDevicesByNode[nodeName] = devices
				}
				devices.Insert(structured.MakeDeviceID(slice.Spec.Driver, slice.Spec.Pool.Name, device.Name))
			}
		}
	})
}
