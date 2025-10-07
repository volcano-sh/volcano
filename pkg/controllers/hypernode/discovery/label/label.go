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

package label

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mitchellh/mapstructure"
	"golang.org/x/exp/maps"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/informers"
	infov1 "k8s.io/client-go/informers/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	topologyinformerv1alpha1 "volcano.sh/apis/pkg/client/informers/externalversions/topology/v1alpha1"
	topologylisterv1alpha1 "volcano.sh/apis/pkg/client/listers/topology/v1alpha1"

	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/utils"
)

const (
	networkTopologyTypeLength = 20
	loopCount                 = 20
)

func init() {
	api.RegisterDiscoverer("label", NewLabelDiscoverer)
}

// NodeLabel represents a label associated with a node in the network topology.
type NodeLabel struct {
	NodeLabel string `mapstructure:"nodeLabel"`
}

// NetworkTopologyType defines the structure that holds different types of network topologies.
type NetworkTopologyType struct {
	NetworkTopologyTypes map[string][]NodeLabel `mapstructure:"networkTopologyTypes"`
}

// HyperNodeInfo contains detailed information about a HyperNode, which is used to describe the hypernode's tier, members, and label attributes.
type HyperNodeInfo struct {
	tier    int
	members []string
	labels  map[string]string
}

// labelDiscoverer implements the Discoverer interface for label
type labelDiscoverer struct {
	nodeInformer          infov1.NodeInformer
	hyperNodeInformer     topologyinformerv1alpha1.HyperNodeInformer
	informerFactory       informers.SharedInformerFactory
	vcInformerFactory     vcinformer.SharedInformerFactory
	networkTopologyRecord map[string][]string
	outputCh              chan []*topologyv1alpha1.HyperNode
	stopCh                chan struct{}
	completedCh           chan struct{}
	queue                 workqueue.TypedRateLimitingInterface[string]
	hyperNodeLister       topologylisterv1alpha1.HyperNodeLister
}

// Start begins the topology discovery process and returns the channel for receiving discovered topology
func (l *labelDiscoverer) Start() (chan []*topologyv1alpha1.HyperNode, error) {
	l.nodeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    l.AddNode,
			UpdateFunc: l.UpdateNode,
			DeleteFunc: l.DeleteNode,
		},
	)

	l.hyperNodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: l.DeleteHyperNode,
	})
	l.informerFactory.Start(l.stopCh)
	l.vcInformerFactory.Start(l.stopCh)
	for informerType, ok := range l.informerFactory.WaitForCacheSync(l.stopCh) {
		if !ok {
			klog.Errorf("Failed to sync informer cache: %v", informerType)
		}
	}
	for informerType, ok := range l.vcInformerFactory.WaitForCacheSync(l.stopCh) {
		if !ok {
			klog.Errorf("Failed to sync informer cache: %v", informerType)
		}
	}

	l.enqueue()

	// Start discovery in a separate goroutine
	go l.work()

	return l.outputCh, nil
}

// Stop halts the discovery process
func (l *labelDiscoverer) Stop() error {
	close(l.outputCh)
	close(l.stopCh)
	l.queue.ShutDown()
	return nil
}

// ResultSynced notice the topology discovery results have been processed
func (l *labelDiscoverer) ResultSynced() {
	l.completedCh <- struct{}{}
}

// Name returns the discoverer name
func (l *labelDiscoverer) Name() string {
	return "label"
}

// NewLabelDiscoverer creates a new label topology discoverer
func NewLabelDiscoverer(cfg api.DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) api.Discoverer {
	// parse config
	networkTopologyRecord := parseCfg(cfg)

	// Create the output channel that this discoverer will manage
	outputCh := make(chan []*topologyv1alpha1.HyperNode)

	stopCh := make(chan struct{})
	completedCh := make(chan struct{})
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	nodeInformer := informerFactory.Core().V1().Nodes()

	vcInformerFactory := vcinformer.NewSharedInformerFactory(vcClient, 0)
	hyperNodeInformer := vcInformerFactory.Topology().V1alpha1().HyperNodes()
	hyperNodeLister := hyperNodeInformer.Lister()
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	return &labelDiscoverer{
		informerFactory:       informerFactory,
		nodeInformer:          nodeInformer,
		vcInformerFactory:     vcInformerFactory,
		hyperNodeInformer:     hyperNodeInformer,
		networkTopologyRecord: networkTopologyRecord,
		outputCh:              outputCh,
		stopCh:                stopCh,
		completedCh:           completedCh,
		queue:                 queue,
		hyperNodeLister:       hyperNodeLister,
	}
}

// parseCfg Parse the ConfigMap and read the topology information.
func parseCfg(cfg api.DiscoveryConfig) map[string][]string {
	klog.InfoS("Start parse label based hyperNode auto discovery config")

	networkTopologyRecord := make(map[string][]string)

	var config NetworkTopologyType

	if err := mapstructure.WeakDecode(cfg.Config, &config); err != nil {
		klog.Errorf("Cannot get networkTopologyTypes, %s %T, error is %v", cfg.Config, cfg.Config, err)
		return networkTopologyRecord
	}

	for name, labels := range config.NetworkTopologyTypes {
		if len(name) > networkTopologyTypeLength {
			klog.Errorf("The length of topologyTypeName exceeds 20. topologyTypeName is %s", name)
			continue
		}

		if err := checkLabels(labels); err != nil {
			klog.Errorf("The configMap format is incorrect, label is %v, error is %v", labels, err)
			continue
		}
		networkTopologyRecord[name] = make([]string, 0)
		// kubernetes.io/hostname refers to a node label, not a hypernode label. This level of label needs to be removed during traversal.
		for i := len(labels) - 2; i >= 0; i-- {
			networkTopologyRecord[name] = append(networkTopologyRecord[name], labels[i].NodeLabel)
		}
	}
	klog.InfoS("Successfully parsed label based hyperNode auto discovery config", "networkTopologyRecord", networkTopologyRecord)

	return networkTopologyRecord
}

func checkLabels(labels []NodeLabel) error {
	seen := make(map[string]bool)
	for _, label := range labels {
		if _, exist := seen[label.NodeLabel]; !exist {
			seen[label.NodeLabel] = true
			continue
		}
		return errors.New("there are duplicate labels")
	}
	return nil
}

func (l *labelDiscoverer) work() {
	for {
		key, shutdown := l.queue.Get()
		if shutdown {
			return
		}

		func() {
			defer l.queue.Done(key)
			if err := l.discovery(); err != nil {
				klog.ErrorS(err, "Error discover HyperNode")
				l.queue.AddRateLimited(key)
				return
			}

			l.queue.Forget(key)
		}()
		<-l.completedCh
	}
}

func (l *labelDiscoverer) enqueue() {
	// Each call triggers a full discovery process, so rate limiting is needed to prevent excessive API pressure caused by frequent calls.
	l.queue.AddAfter("update", 1000*time.Microsecond)
}

func (l *labelDiscoverer) discovery() error {
	klog.InfoS("Started label based hyperNode auto discovery")

	// Generate hyperNode information based on label information
	hyperNodeInfoMap, err := l.generateHyperNodeInfo()
	if err != nil {
		klog.Errorf("Cannot get generateHyperNodeInfo, error is %v", err)
		return err
	}

	// create HyperNodes
	hyperNodes := l.buildHyperNodes(hyperNodeInfoMap)

	// Send discovered nodes through the channel
	l.outputCh <- hyperNodes

	klog.InfoS("End label based hyperNode auto discovery")
	return err
}

// buildHyperNodes create HyperNodes
func (l *labelDiscoverer) buildHyperNodes(hyperNodeInfoMap map[string]HyperNodeInfo) []*topologyv1alpha1.HyperNode {
	hyperNodes := make([]*topologyv1alpha1.HyperNode, 0, len(hyperNodeInfoMap))

	for hyperNodeName, hyperNodeInfo := range hyperNodeInfoMap {
		// get memberType by tier
		memberType := getMemberType(hyperNodeInfo.tier)

		memberList := removeDuplicates(hyperNodeInfo.members)
		members := utils.BuildMembers(memberList, memberType)

		labelMap := map[string]string{
			api.NetworkTopologySourceLabelKey: l.Name(),
		}
		for key, value := range hyperNodeInfo.labels {
			labelMap[key] = value
		}

		// Create the HyperNode object
		hyperNode := utils.BuildHyperNode(hyperNodeName, hyperNodeInfo.tier, members, labelMap)

		// Add to the list for the hyperNode
		hyperNodes = append(hyperNodes, hyperNode)
	}
	return hyperNodes
}

// AddNode Reconstruct the hyperNode when the node changes.
func (l *labelDiscoverer) AddNode(obj interface{}) {
	labelMap := l.getNodeNetworkTopologyLabels(obj)
	if len(labelMap) > 0 {
		l.enqueue()
	}
}

// UpdateNode Reconstruct the hyperNode when the node changes.
func (l *labelDiscoverer) UpdateNode(oldObj, newObj interface{}) {
	oldLabelMap := l.getNodeNetworkTopologyLabels(oldObj)
	newLabelMap := l.getNodeNetworkTopologyLabels(newObj)
	if !maps.Equal(oldLabelMap, newLabelMap) {
		l.enqueue()
	}
}

// DeleteNode Reconstruct the hyperNode when the node changes.
func (l *labelDiscoverer) DeleteNode(obj interface{}) {
	labelMap := l.getNodeNetworkTopologyLabels(obj)
	if len(labelMap) > 0 {
		l.enqueue()
	}
}

// DeleteHyperNode Reconstruct the hyperNode when the hyperNode has been deleted.
func (l *labelDiscoverer) DeleteHyperNode(obj interface{}) {
	_, ok := obj.(*topologyv1alpha1.HyperNode)
	if !ok {
		// If we reached here it means the HyperNode was deleted but its final state is unrecorded.
		tombstones, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			klog.Errorf("Couldn't get object from tombstone %#v", obj)
			return
		}
		_, ok = tombstones.Obj.(*topologyv1alpha1.HyperNode)
		if !ok {
			klog.Errorf("Tombstone contained object that is not a HyperNode: %#v", obj)
			return
		}
	}
	l.enqueue()
}

// getLabelMap get the labelMap on the node used to construct the hyperNode
func (l *labelDiscoverer) getNodeNetworkTopologyLabels(obj interface{}) map[string]string {
	tempMap := make(map[string]string)
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert to *v1.Node: %v", obj)
		return tempMap
	}
	labelMap := node.Labels
	for _, labelKey := range l.networkTopologyRecord {
		for _, key := range labelKey {
			value, exist := labelMap[key]
			if exist {
				tempMap[key] = value
			}
		}
	}
	return tempMap
}

// generateHyperNodeInfo generate the hyperNodeInfoMap based on all node labels
func (l *labelDiscoverer) generateHyperNodeInfo() (map[string]HyperNodeInfo, error) {
	// Record the hyperNode and its info.
	hyperNodeInfoMap := make(map[string]HyperNodeInfo)

	// Record the label and hyperNodeName.
	labelHyperNodeMap := make(map[string]map[string]string)

	// Get all node
	list, err := l.nodeInformer.Lister().List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list existing Node resources, error is %v", err.Error())
		return hyperNodeInfoMap, err
	}

	for _, node := range list {
		labelMap := node.Labels
		for topologyTypeName, labelKey := range l.networkTopologyRecord {
			memberName := node.Name
			for i, key := range labelKey {
				value, exists := labelMap[key]
				if !exists {
					klog.V(5).Info("The label does not exist in the node")
					break
				}
				tier := i + 1
				hyperNodeName, exists := getHyperNodeCached(labelHyperNodeMap, key, value)
				if !exists {
					// Construct hyperNodeName
					hyperNodeName, err = l.buildHyperNodeName(topologyTypeName, key, value, tier, hyperNodeInfoMap)
					if err != nil {
						klog.Errorf("Failed to build hyperNode resources, error is %v", err.Error())
						return hyperNodeInfoMap, err
					}
				}

				// Record the members of the hyperNode
				_, exists = hyperNodeInfoMap[hyperNodeName]
				if !exists {
					hyperNodeInfoMap[hyperNodeName] = HyperNodeInfo{
						tier:    tier,
						members: make([]string, 0),
						labels:  map[string]string{key: value},
					}
				}
				hyperNodeInfo := hyperNodeInfoMap[hyperNodeName]
				hyperNodeInfo.members = append(hyperNodeInfo.members, memberName)
				hyperNodeInfoMap[hyperNodeName] = hyperNodeInfo

				// Record the label and hyperNodeName.
				if _, exists := labelHyperNodeMap[key]; exists {
					labelHyperNodeMap[key][value] = hyperNodeName
				} else {
					labelHyperNodeMap[key] = map[string]string{
						value: hyperNodeName,
					}
				}
				memberName = hyperNodeName
			}
		}
	}
	return hyperNodeInfoMap, err
}

func (l *labelDiscoverer) buildHyperNodeName(topologyTypeName, key, value string, tier int, hyperNodeInfoMap map[string]HyperNodeInfo) (string, error) {
	list, err := l.hyperNodeLister.List(labels.SelectorFromSet(labels.Set{
		key:                               value,
		api.NetworkTopologySourceLabelKey: "label",
	}))
	if err != nil {
		klog.Errorf("Failed to list existing hyperNode resources, error is %v", err.Error())
		return "", err
	}
	topologyTypeName = cleanString(topologyTypeName)
	if len(list) > 0 {
		targetName := fmt.Sprintf("hypernode-%s-tier%d", topologyTypeName, tier)
		// To ensure deterministic behavior, sort by name before iterating.
		sort.Slice(list, func(i, j int) bool {
			return list[i].Name < list[j].Name
		})
		for _, hyperNode := range list {
			if strings.HasPrefix(hyperNode.Name, targetName) {
				return hyperNode.Name, nil
			}
		}
	}
	for i := 0; i < loopCount; i++ {
		randomSuffix := rand.String(5)
		hyperNodeName := fmt.Sprintf("hypernode-%s-tier%d-%s", topologyTypeName, tier, randomSuffix)
		_, err := l.hyperNodeLister.Get(hyperNodeName)
		if err == nil {
			continue
		}
		if !apierrors.IsNotFound(err) {
			return "", err
		}
		_, exists := hyperNodeInfoMap[hyperNodeName]
		if !exists {
			return hyperNodeName, nil
		}
	}
	klog.Errorf("unable to get unique hyperNodeName after %d attempts", loopCount)
	return "", fmt.Errorf("unable to get unique hyperNodeName after %d attempts", loopCount)
}

func cleanString(s string) string {
	cleaned := make([]byte, 0, len(s))
	for _, c := range s {
		if unicode.IsLetter(c) {
			cleaned = append(cleaned, byte(unicode.ToLower(c)))
		} else if unicode.IsDigit(c) || c == '.' || c == '-' {
			cleaned = append(cleaned, byte(c))
		} else {
			cleaned = append(cleaned, '-')
		}
	}
	return string(cleaned)
}

// getMemberType Return member type based on tier
func getMemberType(tier int) topologyv1alpha1.MemberType {
	if tier == 1 {
		return topologyv1alpha1.MemberTypeNode
	}
	return topologyv1alpha1.MemberTypeHyperNode
}

// removeDuplicates Remove duplicates from the list
func removeDuplicates(memberList []string) []string {
	seen := make(map[string]struct{})
	for _, v := range memberList {
		seen[v] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}
	return result
}

func getHyperNodeCached(labelHyperNodeMap map[string]map[string]string, key, value string) (string, bool) {
	if valueMap, exists := labelHyperNodeMap[key]; exists {
		if name, exists := valueMap[value]; exists {
			return name, true
		}
	}
	return "", false
}
