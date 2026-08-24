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
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mitchellh/mapstructure"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	api.RegisterDiscovererWithOptions("label", NewLabelDiscovererWithOptions)
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
	tier     int
	tierName string
	members  []string
	labels   map[string]string
}

// labelDiscoverer implements the Discoverer interface for label
type labelDiscoverer struct {
	informerFactory       informers.SharedInformerFactory
	vcInformerFactory     vcinformer.SharedInformerFactory
	nodeInformer          infov1.NodeInformer
	hyperNodeInformer     topologyinformerv1alpha1.HyperNodeInformer
	networkTopologyRecord map[string][]string
	outputCh              chan []*topologyv1alpha1.HyperNode
	stopCh                chan struct{}
	completedCh           chan struct{}
	queue                 workqueue.TypedRateLimitingInterface[string]
	hyperNodeLister       topologylisterv1alpha1.HyperNodeLister
	vcClient              vcclientset.Interface
	nodeHandler           cache.ResourceEventHandlerRegistration
	hyperNodeHandler      cache.ResourceEventHandlerRegistration
	workerDone            chan struct{}
	started               bool
}

// hyperNodeNameResolver resolves and reuses HyperNode names within one complete
// topology discovery cycle. It lazily loads one live API snapshot when the
// informer cache misses and shares that snapshot across all name resolutions
// in the cycle. A new resolver is created for each cycle so the snapshot is
// never reused by a later discovery.
type hyperNodeNameResolver struct {
	hyperNodeLister    topologylisterv1alpha1.HyperNodeLister
	vcClient           vcclientset.Interface
	liveHyperNodes     []*topologyv1alpha1.HyperNode
	liveHyperNodeNames map[string]struct{}
	liveLoaded         bool
	liveErr            error
}

func newHyperNodeNameResolver(hyperNodeLister topologylisterv1alpha1.HyperNodeLister,
	vcClient vcclientset.Interface) *hyperNodeNameResolver {
	return &hyperNodeNameResolver{
		hyperNodeLister: hyperNodeLister,
		vcClient:        vcClient,
	}
}

// Start begins the topology discovery process and returns the channel for receiving discovered topology
func (l *labelDiscoverer) Start() (chan []*topologyv1alpha1.HyperNode, error) {
	var err error
	l.nodeHandler, err = l.nodeInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc:    l.AddNode,
			UpdateFunc: l.UpdateNode,
			DeleteFunc: l.DeleteNode,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register Node event handler: %w", err)
	}

	l.hyperNodeHandler, err = l.hyperNodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: l.DeleteHyperNode,
	})
	if err != nil {
		if removeErr := l.nodeInformer.Informer().RemoveEventHandler(l.nodeHandler); removeErr != nil {
			klog.ErrorS(removeErr, "Failed to remove Node event handler after HyperNode handler registration failed")
		}
		return nil, fmt.Errorf("failed to register HyperNode event handler: %w", err)
	}
	if l.informerFactory != nil {
		l.informerFactory.Start(l.stopCh)
		for informerType, ok := range l.informerFactory.WaitForCacheSync(l.stopCh) {
			if !ok {
				klog.Errorf("Failed to sync informer cache: %v", informerType)
			}
		}
	}
	if l.vcInformerFactory != nil {
		l.vcInformerFactory.Start(l.stopCh)
		for informerType, ok := range l.vcInformerFactory.WaitForCacheSync(l.stopCh) {
			if !ok {
				klog.Errorf("Failed to sync informer cache: %v", informerType)
			}
		}
	}
	l.enqueue()

	l.started = true
	go l.work()

	return l.outputCh, nil
}

// Stop halts the discovery process
func (l *labelDiscoverer) Stop() error {
	if l.nodeHandler != nil {
		if err := l.nodeInformer.Informer().RemoveEventHandler(l.nodeHandler); err != nil {
			klog.ErrorS(err, "Failed to remove Node event handler")
		}
	}
	if l.hyperNodeHandler != nil {
		if err := l.hyperNodeInformer.Informer().RemoveEventHandler(l.hyperNodeHandler); err != nil {
			klog.ErrorS(err, "Failed to remove HyperNode event handler")
		}
	}
	close(l.stopCh)
	l.queue.ShutDown()
	if l.started {
		<-l.workerDone
	}
	return nil
}

// ResultSynced notice the topology discovery results have been processed
func (l *labelDiscoverer) ResultSynced() {
	select {
	case l.completedCh <- struct{}{}:
	case <-l.stopCh:
	}
}

// Name returns the discoverer name
func (l *labelDiscoverer) Name() string {
	return "label"
}

// NewLabelDiscoverer creates a label discoverer using dedicated informer factories.
// Volcano processes use NewLabelDiscovererWithOptions to reuse shared informers.
func NewLabelDiscoverer(cfg api.DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) api.Discoverer {
	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)
	vcInformerFactory := vcinformer.NewSharedInformerFactory(vcClient, 0)
	discoverer, _ := newLabelDiscoverer(cfg, api.DiscovererOptions{
		KubeClient: kubeClient, VolcanoClient: vcClient,
		NodeInformer:      informerFactory.Core().V1().Nodes(),
		HyperNodeInformer: vcInformerFactory.Topology().V1alpha1().HyperNodes(),
	}, informerFactory, vcInformerFactory)
	return discoverer
}

// NewLabelDiscovererWithOptions creates a label discoverer that reuses process-provided informers.
func NewLabelDiscovererWithOptions(cfg api.DiscoveryConfig, options api.DiscovererOptions) (api.Discoverer, error) {
	if options.NodeInformer == nil || options.HyperNodeInformer == nil {
		if options.KubeClient == nil || options.VolcanoClient == nil {
			return nil, errors.New("label discoverer requires Node and HyperNode informers or Kubernetes and Volcano clients")
		}
		return NewLabelDiscoverer(cfg, options.KubeClient, options.VolcanoClient), nil
	}
	return newLabelDiscoverer(cfg, options, nil, nil)
}

func newLabelDiscoverer(cfg api.DiscoveryConfig, options api.DiscovererOptions, informerFactory informers.SharedInformerFactory,
	vcInformerFactory vcinformer.SharedInformerFactory) (api.Discoverer, error) {
	if options.NodeInformer == nil || options.HyperNodeInformer == nil {
		return nil, errors.New("label discoverer requires Node and HyperNode informers")
	}
	if options.VolcanoClient == nil {
		return nil, errors.New("label discoverer requires a Volcano client")
	}
	// parse config
	networkTopologyRecord := parseCfg(cfg)

	// Create the output channel that this discoverer will manage
	outputCh := make(chan []*topologyv1alpha1.HyperNode)

	stopCh := make(chan struct{})
	completedCh := make(chan struct{})
	nodeInformer := options.NodeInformer
	hyperNodeInformer := options.HyperNodeInformer
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	return &labelDiscoverer{
		informerFactory:       informerFactory,
		vcInformerFactory:     vcInformerFactory,
		nodeInformer:          nodeInformer,
		hyperNodeInformer:     hyperNodeInformer,
		networkTopologyRecord: networkTopologyRecord,
		outputCh:              outputCh,
		stopCh:                stopCh,
		completedCh:           completedCh,
		workerDone:            make(chan struct{}),
		queue:                 queue,
		hyperNodeLister:       hyperNodeInformer.Lister(),
		vcClient:              options.VolcanoClient,
	}, nil
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
	defer close(l.workerDone)
	defer close(l.outputCh)
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
		select {
		case <-l.completedCh:
		case <-l.stopCh:
			return
		}
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
	select {
	case l.outputCh <- hyperNodes:
	case <-l.stopCh:
		return nil
	}

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
		hyperNode := utils.BuildHyperNodeWithTierName(hyperNodeName, hyperNodeInfo.tier, hyperNodeInfo.tierName, members, labelMap)

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
	if !stringMapsEqual(oldLabelMap, newLabelMap) {
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

func stringMapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// generateHyperNodeInfo generate the hyperNodeInfoMap based on all node labels
func (l *labelDiscoverer) generateHyperNodeInfo() (map[string]HyperNodeInfo, error) {
	// Record the hyperNode and its info.
	hyperNodeInfoMap := make(map[string]HyperNodeInfo)

	// Record the label and hyperNodeName.
	labelHyperNodeMap := make(map[string]map[string]string)
	nameResolver := newHyperNodeNameResolver(l.hyperNodeLister, l.vcClient)

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
					hyperNodeName, err = nameResolver.buildHyperNodeName(topologyTypeName, key, value, tier, hyperNodeInfoMap)
					if err != nil {
						klog.Errorf("Failed to build hyperNode resources, error is %v", err.Error())
						return hyperNodeInfoMap, err
					}
				}

				// Record the members of the hyperNode
				_, exists = hyperNodeInfoMap[hyperNodeName]
				if !exists {
					hyperNodeInfoMap[hyperNodeName] = HyperNodeInfo{
						tier:     tier,
						tierName: key,
						members:  make([]string, 0),
						labels:   map[string]string{key: value},
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

func (r *hyperNodeNameResolver) buildHyperNodeName(topologyTypeName, key, value string, tier int, hyperNodeInfoMap map[string]HyperNodeInfo) (string, error) {
	selector := labels.SelectorFromSet(labels.Set{
		key:                               value,
		api.NetworkTopologySourceLabelKey: "label",
	})
	list, err := r.hyperNodeLister.List(selector)
	if err != nil {
		klog.Errorf("Failed to list existing hyperNode resources, error is %v", err.Error())
		return "", err
	}
	topologyTypeName = cleanString(topologyTypeName)
	targetName := fmt.Sprintf("hypernode-%s-tier%d", topologyTypeName, tier)
	if name, found := findExistingHyperNodeName(list, targetName); found {
		return name, nil
	}

	// An API write can complete before the shared informer observes it. Confirm
	// the cache miss against the API before generating another random name for
	// the same logical topology. A resolver loads at most one live snapshot per
	// discovery round, even when the topology contains many HyperNodes.
	liveHyperNodes, err := r.getLiveHyperNodes()
	if err != nil {
		return "", err
	}
	matchingLiveHyperNodes := make([]*topologyv1alpha1.HyperNode, 0)
	for _, hyperNode := range liveHyperNodes {
		if selector.Matches(labels.Set(hyperNode.Labels)) {
			matchingLiveHyperNodes = append(matchingLiveHyperNodes, hyperNode)
		}
	}
	if name, found := findExistingHyperNodeName(matchingLiveHyperNodes, targetName); found {
		return name, nil
	}

	for i := 0; i < loopCount; i++ {
		randomSuffix := rand.String(5)
		hyperNodeName := fmt.Sprintf("hypernode-%s-tier%d-%s", topologyTypeName, tier, randomSuffix)
		_, err := r.hyperNodeLister.Get(hyperNodeName)
		if err == nil {
			continue
		}
		if !apierrors.IsNotFound(err) {
			return "", err
		}
		if _, exists := r.liveHyperNodeNames[hyperNodeName]; exists {
			continue
		}
		_, exists := hyperNodeInfoMap[hyperNodeName]
		if !exists {
			return hyperNodeName, nil
		}
	}
	klog.Errorf("unable to get unique hyperNodeName after %d attempts", loopCount)
	return "", fmt.Errorf("unable to get unique hyperNodeName after %d attempts", loopCount)
}

func (r *hyperNodeNameResolver) getLiveHyperNodes() ([]*topologyv1alpha1.HyperNode, error) {
	if r.liveLoaded {
		return r.liveHyperNodes, r.liveErr
	}
	r.liveLoaded = true
	liveList, err := r.vcClient.TopologyV1alpha1().HyperNodes().List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		r.liveErr = fmt.Errorf("failed to confirm existing HyperNodes from API: %w", err)
		return nil, r.liveErr
	}
	r.liveHyperNodes = make([]*topologyv1alpha1.HyperNode, 0, len(liveList.Items))
	r.liveHyperNodeNames = make(map[string]struct{}, len(liveList.Items))
	for i := range liveList.Items {
		r.liveHyperNodes = append(r.liveHyperNodes, &liveList.Items[i])
		r.liveHyperNodeNames[liveList.Items[i].Name] = struct{}{}
	}
	return r.liveHyperNodes, nil
}

func findExistingHyperNodeName(hyperNodes []*topologyv1alpha1.HyperNode, targetName string) (string, bool) {
	matchingNames := make([]string, 0, len(hyperNodes))
	for _, hyperNode := range hyperNodes {
		if strings.HasPrefix(hyperNode.Name, targetName) {
			matchingNames = append(matchingNames, hyperNode.Name)
		}
	}
	if len(matchingNames) == 0 {
		return "", false
	}
	sort.Strings(matchingNames)
	return matchingNames[0], true
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
