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
	"crypto/sha256"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mitchellh/mapstructure"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/validation"
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
)

func init() {
	api.RegisterDiscovererWithOptions("label", NewLabelDiscovererWithOptions)
}

// NodeLabel represents a label associated with a node in the network topology.
type NodeLabel struct {
	NodeLabel string `mapstructure:"nodeLabel"`
	TierName  string `mapstructure:"tierName"`
}

// NetworkTopologyType defines the structure that holds different types of network topologies.
type NetworkTopologyType struct {
	NetworkTopologyTypes map[string]interface{} `mapstructure:"networkTopologyTypes"`
}

// NetworkTopologyProfile is the extended topology configuration. Levels use
// node labels to identify domains and tierName to expose a shared semantic
// boundary to the scheduler.
type NetworkTopologyProfile struct {
	NodeSelector *metav1.LabelSelector `mapstructure:"nodeSelector"`
	Levels       []NodeLabel           `mapstructure:"levels"`
}

type topologyProfile struct {
	name               string
	labelValue         string
	nodeSelector       labels.Selector
	selectorConfigured bool
	levels             []NodeLabel
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
	informerFactory      informers.SharedInformerFactory
	vcInformerFactory    vcinformer.SharedInformerFactory
	nodeInformer         infov1.NodeInformer
	hyperNodeInformer    topologyinformerv1alpha1.HyperNodeInformer
	topologyProfiles     []topologyProfile
	watchedNodeLabelKeys map[string]struct{}
	configErr            error
	outputCh             chan []*topologyv1alpha1.HyperNode
	stopCh               chan struct{}
	completedCh          chan struct{}
	queue                workqueue.TypedRateLimitingInterface[string]
	hyperNodeLister      topologylisterv1alpha1.HyperNodeLister
	vcClient             vcclientset.Interface
	nodeHandler          cache.ResourceEventHandlerRegistration
	hyperNodeHandler     cache.ResourceEventHandlerRegistration
	workerDone           chan struct{}
	started              bool
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
	if l.configErr != nil {
		return nil, l.configErr
	}

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
	discoverer, err := newLabelDiscoverer(cfg, api.DiscovererOptions{
		KubeClient: kubeClient, VolcanoClient: vcClient,
		NodeInformer:      informerFactory.Core().V1().Nodes(),
		HyperNodeInformer: vcInformerFactory.Topology().V1alpha1().HyperNodes(),
	}, informerFactory, vcInformerFactory)
	if err != nil {
		return &labelDiscoverer{configErr: err}
	}
	return discoverer
}

// NewLabelDiscovererWithOptions creates a label discoverer that reuses process-provided informers.
func NewLabelDiscovererWithOptions(cfg api.DiscoveryConfig, options api.DiscovererOptions) (api.Discoverer, error) {
	if options.NodeInformer == nil || options.HyperNodeInformer == nil {
		if options.KubeClient == nil || options.VolcanoClient == nil {
			return nil, errors.New("label discoverer requires Node and HyperNode informers or Kubernetes and Volcano clients")
		}
		informerFactory := informers.NewSharedInformerFactory(options.KubeClient, 0)
		vcInformerFactory := vcinformer.NewSharedInformerFactory(options.VolcanoClient, 0)
		options.NodeInformer = informerFactory.Core().V1().Nodes()
		options.HyperNodeInformer = vcInformerFactory.Topology().V1alpha1().HyperNodes()
		return newLabelDiscoverer(cfg, options, informerFactory, vcInformerFactory)
	}
	return newLabelDiscoverer(cfg, options, nil, nil)
}

func newLabelDiscoverer(cfg api.DiscoveryConfig, options api.DiscovererOptions, informerFactory informers.SharedInformerFactory,
	vcInformerFactory vcinformer.SharedInformerFactory) (*labelDiscoverer, error) {
	if options.NodeInformer == nil || options.HyperNodeInformer == nil {
		return nil, errors.New("label discoverer requires Node and HyperNode informers")
	}
	if options.VolcanoClient == nil {
		return nil, errors.New("label discoverer requires a Volcano client")
	}
	// parse config
	topologyProfiles, watchedNodeLabelKeys, err := parseCfg(cfg)
	if err != nil {
		return nil, err
	}

	// Create the output channel that this discoverer will manage
	outputCh := make(chan []*topologyv1alpha1.HyperNode)

	stopCh := make(chan struct{})
	completedCh := make(chan struct{})
	nodeInformer := options.NodeInformer
	hyperNodeInformer := options.HyperNodeInformer
	queue := workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[string]())

	return &labelDiscoverer{
		informerFactory:      informerFactory,
		nodeInformer:         nodeInformer,
		vcInformerFactory:    vcInformerFactory,
		hyperNodeInformer:    hyperNodeInformer,
		topologyProfiles:     topologyProfiles,
		watchedNodeLabelKeys: watchedNodeLabelKeys,
		outputCh:             outputCh,
		stopCh:               stopCh,
		completedCh:          completedCh,
		workerDone:           make(chan struct{}),
		queue:                queue,
		hyperNodeLister:      hyperNodeInformer.Lister(),
		vcClient:             options.VolcanoClient,
	}, nil
}

// parseCfg parses legacy topology lists and extended topology profiles.
func parseCfg(cfg api.DiscoveryConfig) ([]topologyProfile, map[string]struct{}, error) {
	klog.InfoS("Start parse label based hyperNode auto discovery config")

	var config NetworkTopologyType
	if err := strictWeakDecode(cfg.Config, &config); err != nil {
		return nil, nil, fmt.Errorf("decode networkTopologyTypes: %w", err)
	}
	if len(config.NetworkTopologyTypes) == 0 {
		return nil, nil, errors.New("networkTopologyTypes must contain at least one topology profile")
	}

	names := make([]string, 0, len(config.NetworkTopologyTypes))
	for name := range config.NetworkTopologyTypes {
		names = append(names, name)
	}
	sort.Strings(names)

	profiles := make([]topologyProfile, 0, len(names))
	watchedNodeLabelKeys := make(map[string]struct{})
	profileByLabelValue := make(map[string]string)
	for _, name := range names {
		if len(name) > networkTopologyTypeLength {
			return nil, nil, fmt.Errorf("topology profile name %q exceeds %d characters", name, networkTopologyTypeLength)
		}

		rawProfile := config.NetworkTopologyTypes[name]
		profileConfig := NetworkTopologyProfile{}
		legacyConfig := reflect.ValueOf(rawProfile).IsValid() && reflect.ValueOf(rawProfile).Kind() == reflect.Slice
		if legacyConfig {
			if err := strictWeakDecode(rawProfile, &profileConfig.Levels); err != nil {
				return nil, nil, fmt.Errorf("decode legacy topology profile %q: %w", name, err)
			}
		} else if err := strictWeakDecode(rawProfile, &profileConfig); err != nil {
			return nil, nil, fmt.Errorf("decode topology profile %q: %w", name, err)
		}

		if err := checkLabels(profileConfig.Levels); err != nil {
			return nil, nil, fmt.Errorf("invalid topology profile %q: %w", name, err)
		}
		if len(profileConfig.Levels) < 2 {
			return nil, nil, fmt.Errorf("invalid topology profile %q: at least one topology level and one node level are required", name)
		}
		leafLevel := profileConfig.Levels[len(profileConfig.Levels)-1]
		if leafLevel.NodeLabel != v1.LabelHostname {
			return nil, nil, fmt.Errorf("invalid topology profile %q: last level must use nodeLabel %q", name, v1.LabelHostname)
		}
		if leafLevel.TierName != "" {
			return nil, nil, fmt.Errorf("invalid topology profile %q: node leaf level must not set tierName", name)
		}

		selector := labels.Everything()
		if profileConfig.NodeSelector != nil {
			var err error
			selector, err = metav1.LabelSelectorAsSelector(profileConfig.NodeSelector)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid nodeSelector for topology profile %q: %w", name, err)
			}
			for key := range profileConfig.NodeSelector.MatchLabels {
				watchedNodeLabelKeys[key] = struct{}{}
			}
			for _, expression := range profileConfig.NodeSelector.MatchExpressions {
				watchedNodeLabelKeys[expression.Key] = struct{}{}
			}
		}

		levels := make([]NodeLabel, 0, len(profileConfig.Levels)-1)
		seenTierNames := make(map[string]struct{})
		// kubernetes.io/hostname refers to a node label, not a hypernode label. This level of label needs to be removed during traversal.
		for i := len(profileConfig.Levels) - 2; i >= 0; i-- {
			level := profileConfig.Levels[i]
			if level.TierName == "" {
				level.TierName = level.NodeLabel
			}
			if _, exists := seenTierNames[level.TierName]; exists {
				return nil, nil, fmt.Errorf("invalid topology profile %q: duplicate tierName %q", name, level.TierName)
			}
			seenTierNames[level.TierName] = struct{}{}
			levels = append(levels, level)
			watchedNodeLabelKeys[level.NodeLabel] = struct{}{}
		}

		labelValue := strings.Trim(cleanString(name), ".-")
		if labelValue == "" {
			return nil, nil, fmt.Errorf("topology profile name %q cannot be normalized to a label value", name)
		}
		if validationErrors := validation.IsValidLabelValue(labelValue); len(validationErrors) > 0 {
			return nil, nil, fmt.Errorf("topology profile name %q has invalid normalized label value %q: %s", name, labelValue, strings.Join(validationErrors, "; "))
		}
		if existingName, exists := profileByLabelValue[labelValue]; exists {
			return nil, nil, fmt.Errorf("topology profile names %q and %q normalize to the same label value %q", existingName, name, labelValue)
		}
		profileByLabelValue[labelValue] = name
		profiles = append(profiles, topologyProfile{
			name:               name,
			labelValue:         labelValue,
			nodeSelector:       selector,
			selectorConfigured: profileConfig.NodeSelector != nil,
			levels:             levels,
		})
	}
	klog.InfoS("Successfully parsed label based hyperNode auto discovery config", "profileCount", len(profiles))

	return profiles, watchedNodeLabelKeys, nil
}

func strictWeakDecode(input, output interface{}) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		ErrorUnused:      true,
		WeaklyTypedInput: true,
		ZeroFields:       true,
		Result:           output,
	})
	if err != nil {
		return err
	}
	return decoder.Decode(input)
}

func checkLabels(labels []NodeLabel) error {
	seen := make(map[string]bool)
	for _, label := range labels {
		if label.NodeLabel == "" {
			return errors.New("nodeLabel cannot be empty")
		}
		if validationErrors := validation.IsQualifiedName(label.NodeLabel); len(validationErrors) > 0 {
			return fmt.Errorf("nodeLabel %q is not a valid qualified name: %s", label.NodeLabel, strings.Join(validationErrors, "; "))
		}
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

		discoverySucceeded := false
		func() {
			defer l.queue.Done(key)
			if err := l.discovery(); err != nil {
				klog.ErrorS(err, "Error discover HyperNode")
				l.queue.AddRateLimited(key)
				return
			}

			l.queue.Forget(key)
			discoverySucceeded = true
		}()
		if !discoverySucceeded {
			continue
		}
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

	// Send discovered nodes through the channel unless shutdown has started.
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
	for key := range l.watchedNodeLabelKeys {
		value, exist := labelMap[key]
		if exist {
			tempMap[key] = value
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

	// Domain identity is profile-scoped. Two profiles may intentionally use
	// the same node label and value while representing independent networks.
	type domainKey struct {
		profile   string
		nodeLabel string
		value     string
	}
	domainHyperNodeMap := make(map[domainKey]string)
	parentByHyperNode := make(map[string]string)
	nameResolver := newHyperNodeNameResolver(l.hyperNodeLister, l.vcClient)

	// Get all node
	list, err := l.nodeInformer.Lister().List(labels.Everything())
	if err != nil {
		klog.Errorf("Failed to list existing Node resources, error is %v", err.Error())
		return hyperNodeInfoMap, err
	}

	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	for _, node := range list {
		labelMap := node.Labels
		profiles, err := l.profilesForNode(node)
		if err != nil {
			return hyperNodeInfoMap, err
		}
		if len(profiles) == 0 {
			continue
		}

		for _, profile := range profiles {
			memberName := node.Name
			for i, level := range profile.levels {
				value, exists := labelMap[level.NodeLabel]
				if !exists {
					if profile.selectorConfigured {
						return hyperNodeInfoMap, fmt.Errorf("node %s selected by topology profile %s is missing level label %s", node.Name, profile.name, level.NodeLabel)
					}
					klog.V(5).InfoS("Topology level label does not exist on node", "node", node.Name, "profile", profile.name, "label", level.NodeLabel)
					break
				}

				tier := i + 1
				cacheKey := domainKey{profile: profile.name, nodeLabel: level.NodeLabel, value: value}
				hyperNodeName, exists := domainHyperNodeMap[cacheKey]
				if !exists {
					hyperNodeName, err = nameResolver.buildHyperNodeName(*profile, level.NodeLabel, value, tier, hyperNodeInfoMap)
					if err != nil {
						return hyperNodeInfoMap, fmt.Errorf("build HyperNode for profile %s: %w", profile.name, err)
					}
					domainHyperNodeMap[cacheKey] = hyperNodeName
				}

				hyperNodeInfo, exists := hyperNodeInfoMap[hyperNodeName]
				if !exists {
					hyperNodeInfo = HyperNodeInfo{
						tier:     tier,
						tierName: level.TierName,
						members:  make([]string, 0),
						labels: map[string]string{
							api.NetworkTopologyProfileLabelKey: profile.labelValue,
							level.NodeLabel:                    value,
						},
					}
				}
				if tier > 1 {
					if existingParent, exists := parentByHyperNode[memberName]; exists && existingParent != hyperNodeName {
						return hyperNodeInfoMap, fmt.Errorf("HyperNode %s in profile %s belongs to multiple parents: %s and %s", memberName, profile.name, existingParent, hyperNodeName)
					}
					parentByHyperNode[memberName] = hyperNodeName
				}
				hyperNodeInfo.members = append(hyperNodeInfo.members, memberName)
				hyperNodeInfoMap[hyperNodeName] = hyperNodeInfo
				memberName = hyperNodeName
			}
		}
	}
	return hyperNodeInfoMap, nil
}

// profilesForNode gives explicit selectors ownership of a Node. If no
// explicit selector matches, every matching selector-less legacy topology is
// returned so the pre-profile multi-topology traversal remains compatible.
func (l *labelDiscoverer) profilesForNode(node *v1.Node) ([]*topologyProfile, error) {
	var explicitMatch *topologyProfile
	legacyMatches := make([]*topologyProfile, 0)
	for i := range l.topologyProfiles {
		profile := &l.topologyProfiles[i]
		if !profile.nodeSelector.Matches(labels.Set(node.Labels)) {
			continue
		}
		if profile.selectorConfigured {
			if explicitMatch != nil {
				return nil, fmt.Errorf("node %s matches multiple topology profiles: %s and %s", node.Name, explicitMatch.name, profile.name)
			}
			explicitMatch = profile
			continue
		}

		// Legacy profiles have no selector. Preserve their partial-level
		// behavior while requiring at least the first topology label before
		// treating the node as a profile member.
		if len(profile.levels) == 0 {
			continue
		}
		if _, exists := node.Labels[profile.levels[0].NodeLabel]; !exists {
			continue
		}
		legacyMatches = append(legacyMatches, profile)
	}
	if explicitMatch != nil {
		return []*topologyProfile{explicitMatch}, nil
	}
	return legacyMatches, nil
}

func (l *labelDiscoverer) buildHyperNodeName(profile topologyProfile, key, value string, tier int,
	hyperNodeInfoMap map[string]HyperNodeInfo) (string, error) {
	return newHyperNodeNameResolver(l.hyperNodeLister, l.vcClient).
		buildHyperNodeName(profile, key, value, tier, hyperNodeInfoMap)
}

func (r *hyperNodeNameResolver) buildHyperNodeName(profile topologyProfile, key, value string, tier int,
	hyperNodeInfoMap map[string]HyperNodeInfo) (string, error) {
	selector := labels.SelectorFromSet(labels.Set{
		key:                               value,
		api.NetworkTopologySourceLabelKey: "label",
	})
	list, err := r.hyperNodeLister.List(selector)
	if err != nil {
		klog.Errorf("Failed to list existing hyperNode resources, error is %v", err.Error())
		return "", err
	}
	topologyTypeName := cleanString(profile.name)
	targetName := fmt.Sprintf("hypernode-%s-tier%d", topologyTypeName, tier)
	if name, found := findExistingHyperNodeName(list, profile, targetName); found {
		return name, nil
	}

	// An API write can complete before the shared informer observes it. Confirm
	// the cache miss against the API before generating the deterministic name.
	// This is also required to reuse a legacy random-suffix object during an
	// upgrade instead of publishing a second object for the same Profile domain.
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
	if name, found := findExistingHyperNodeName(matchingLiveHyperNodes, profile, targetName); found {
		return name, nil
	}

	domainHash := sha256.Sum256([]byte(profile.name + "\x00" + key + "\x00" + value))
	hyperNodeName := fmt.Sprintf("hypernode-%s-tier%d-%x", topologyTypeName, tier, domainHash[:6])
	_, err = r.hyperNodeLister.Get(hyperNodeName)
	if err == nil {
		return "", fmt.Errorf("deterministic HyperNode name %s is already used by a different topology domain", hyperNodeName)
	}
	if !apierrors.IsNotFound(err) {
		return "", err
	}
	if _, exists := r.liveHyperNodeNames[hyperNodeName]; exists {
		return "", fmt.Errorf("deterministic HyperNode name %s is already used by a different topology domain", hyperNodeName)
	}
	if _, exists := hyperNodeInfoMap[hyperNodeName]; exists {
		return "", fmt.Errorf("deterministic HyperNode name collision for %s", hyperNodeName)
	}
	return hyperNodeName, nil
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

func findExistingHyperNodeName(hyperNodes []*topologyv1alpha1.HyperNode, profile topologyProfile,
	targetName string) (string, bool) {
	matchingNames := make([]string, 0, len(hyperNodes))
	for _, hyperNode := range hyperNodes {
		if existingProfile, exists := hyperNode.Labels[api.NetworkTopologyProfileLabelKey]; exists && existingProfile != profile.labelValue {
			continue
		}
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
