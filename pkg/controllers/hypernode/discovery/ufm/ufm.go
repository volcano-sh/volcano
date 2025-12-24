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

package ufm

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/controllers/hypernode/api"
	"volcano.sh/volcano/pkg/controllers/hypernode/utils"
)

func init() {
	api.RegisterDiscoverer("ufm", NewUFMDiscoverer)
}

const (
	// maxBodySize defines the maximum size of response body (100MB)
	maxBodySize = 100 << 20
	// ufmPortsPath defines the API path for UFM ports resources
	ufmPortsPath = "/ufmRest/resources/ports"
)

// UFMInterface represents a single interface between nodes in the UFM topology
type UFMInterface struct {
	Description     string `json:"description"`
	Tier            int    `json:"tier"`
	SystemName      string `json:"system_name"`
	NodeDescription string `json:"node_description"`
	PeerNodeName    string `json:"peer_node_name"`
}

// LeafSwitch represents a single leaf switch in the network topology
type LeafSwitch struct {
	Name      string
	Tier      int
	NodeNames sets.Set[string]
}

// LeafSwitchesGroup represents a group of connected leaf switches
type LeafSwitchesGroup struct {
	Leafs     map[string]LeafSwitch
	NodeNames sets.Set[string]
}

// ufmDiscoverer implements the Discoverer interface for UFM
type ufmDiscoverer struct {
	endpoint          string
	username          string
	password          string
	kubeClient        clientset.Interface
	discoveryInterval time.Duration
	client            *http.Client
	stopCh            chan struct{}
}

// NewUFMDiscoverer creates a new UFM topology discoverer
func NewUFMDiscoverer(cfg api.DiscoveryConfig, kubeClient clientset.Interface, vcClient vcclientset.Interface) api.Discoverer {
	endpoint := cfg.Config["endpoint"].(string)
	insecureSkipVerify, _ := cfg.Config["insecureSkipVerify"].(bool)

	if insecureSkipVerify {
		klog.Warningln("WARNING: TLS certificate verification is disabled which is insecure. This should not be used in production environments")
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecureSkipVerify},
	}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	u := &ufmDiscoverer{
		endpoint:          endpoint,
		discoveryInterval: cfg.Interval,
		client:            client,
		kubeClient:        kubeClient,
		stopCh:            make(chan struct{}),
	}

	var username, password string
	if cfg.Credentials != nil && cfg.Credentials.SecretRef != nil {
		var err error
		username, password, err = u.getCredentialsFromSecret(cfg.Credentials.SecretRef.Name, cfg.Credentials.SecretRef.Namespace)
		if err != nil {
			klog.ErrorS(err, "Failed to get credentials from secret", "secretName", cfg.Credentials.SecretRef.Name)
		}
	} else {
		klog.InfoS("UFM authentication credentials are not configured")
	}
	u.username = username
	u.password = password
	klog.InfoS("UFM discoverer initialized")

	return u
}

// Start begins the topology discovery process and returns the channel for receiving discovered topology
func (u *ufmDiscoverer) Start() (chan []*topologyv1alpha1.HyperNode, error) {
	if u.endpoint == "" {
		return nil, errors.New("UFM endpoint is not configured")
	}

	if u.username == "" || u.password == "" {
		return nil, errors.New("UFM authentication credentials are not configured")
	}

	klog.InfoS("Starting UFM network topology discovery",
		"endpoint", u.endpoint,
		"interval", u.discoveryInterval)

	// Create the output channel that this discoverer will manage
	outputCh := make(chan []*topologyv1alpha1.HyperNode, 10)

	// Start periodic discovery in a separate goroutine
	go u.periodicDiscovery(outputCh)

	return outputCh, nil
}

// Stop halts the discovery process
func (u *ufmDiscoverer) Stop() error {
	close(u.stopCh)
	return nil
}

// Name returns the discoverer name
func (u *ufmDiscoverer) Name() string {
	return "ufm"
}

func (u *ufmDiscoverer) ResultSynced() {
}

// getCredentialsFromSecret retrieves username and password from a Kubernetes Secret
func (u *ufmDiscoverer) getCredentialsFromSecret(name, namespace string) (string, string, error) {
	secret, err := u.kubeClient.CoreV1().Secrets(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get secret %s/%s: %v", namespace, name, err)
	}

	usernameData, ok := secret.Data["username"]
	if !ok {
		return "", "", fmt.Errorf("username not found in secret %s/%s", namespace, name)
	}
	username := string(usernameData)

	passwordData, ok := secret.Data["password"]
	if !ok {
		return "", "", fmt.Errorf("password not found in secret %s/%s", namespace, name)
	}
	password := string(passwordData)

	return username, password, nil
}

// periodicDiscovery periodically discovers network topology
func (u *ufmDiscoverer) periodicDiscovery(outputCh chan []*topologyv1alpha1.HyperNode) {
	// Perform immediate discovery first
	u.discoverAndSend(outputCh)

	// Set up ticker for periodic discovery
	ticker := time.NewTicker(u.discoveryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			u.discoverAndSend(outputCh)
		case <-u.stopCh:
			klog.InfoS("UFM network topology discovery stopped, closing output channel")
			close(outputCh)
			return
		}
	}
}

// discoverAndSend discovers the topology and sends it through the channel
func (u *ufmDiscoverer) discoverAndSend(outputCh chan []*topologyv1alpha1.HyperNode) {
	// Fetch UFM data
	ufmData, err := u.fetchUFMData()
	if err != nil {
		klog.ErrorS(err, "Failed to fetch UFM data")
		return
	}

	// Process UFM data into HyperNodes
	hyperNodes := u.buildHyperNodes(ufmData)

	// Send discovered nodes through the channel
	select {
	case outputCh <- hyperNodes:
		klog.InfoS("Sent network topology data", "hyperNodeCount", len(hyperNodes))
	case <-u.stopCh:
		// Discovery stopped, don't attempt to send
		return
	default:
		klog.InfoS("Failed to send network topology data, channel might be full")
	}
}

// fetchUFMData retrieves network topology data from the UFM API
func (u *ufmDiscoverer) fetchUFMData() ([]UFMInterface, error) {
	var requestURL string
	if strings.HasPrefix(u.endpoint, "http://") || strings.HasPrefix(u.endpoint, "https://") {
		requestURL = strings.TrimRight(u.endpoint, "/") + ufmPortsPath
	} else {
		requestURL = "https://" + strings.TrimRight(u.endpoint, "/") + ufmPortsPath
	}

	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}
	req.SetBasicAuth(u.username, u.password)

	klog.InfoS("Sending request to UFM server", "url", requestURL)
	resp, err := u.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %s", resp.Status)
	}

	resp.Body = http.MaxBytesReader(nil, resp.Body, maxBodySize)

	// Parse response
	var interfaces []UFMInterface
	if err = json.NewDecoder(resp.Body).Decode(&interfaces); err != nil {
		return nil, fmt.Errorf("failed to decode response: %v", err)
	}

	klog.InfoS("Successfully retrieved UFM data", "interfaceCount", len(interfaces))

	return interfaces, nil
}

// buildHyperNodes converts UFM data to HyperNode resources
func (u *ufmDiscoverer) buildHyperNodes(ufmData []UFMInterface) []*topologyv1alpha1.HyperNode {
	// Process UFM data into leaf switch groups
	leafSwitchGroups := u.processLeafSwitchGroups(ufmData)
	hyperNodes := make([]*topologyv1alpha1.HyperNode, 0, len(leafSwitchGroups)+1)
	leafHyperNodeNames := make([]string, 0, len(leafSwitchGroups))

	// Create leaf hypernodes
	for groupID, group := range leafSwitchGroups {
		// Create hypernode name for this leaf group
		hnName := fmt.Sprintf("leaf-hn-%d", groupID)

		nodeList := group.NodeNames.UnsortedList()
		members := utils.BuildMembers(nodeList, topologyv1alpha1.MemberTypeNode)

		// Create the HyperNode object
		leafHyperNode := utils.BuildHyperNode(hnName, 1, members, map[string]string{
			api.NetworkTopologySourceLabelKey: u.Name(),
		})
		hyperNodes = append(hyperNodes, leafHyperNode)

		// Add to the list for the spine hypernode
		leafHyperNodeNames = append(leafHyperNodeNames, hnName)

		klog.InfoS("Created leaf HyperNode", "name", hnName, "nodeCount", len(nodeList))
	}

	// Create spine hypernode that includes all leaf hypernodes
	if len(leafHyperNodeNames) > 0 {
		spineHnName := "spine-hn"
		members := utils.BuildMembers(leafHyperNodeNames, topologyv1alpha1.MemberTypeHyperNode)
		spineHyperNode := utils.BuildHyperNode(spineHnName, 2, members, map[string]string{
			api.NetworkTopologySourceLabelKey: u.Name(),
		})
		hyperNodes = append(hyperNodes, spineHyperNode)

		klog.InfoS("Created spine HyperNode", "name", spineHnName, "leafCount", len(leafHyperNodeNames))
	}

	return hyperNodes
}

// processLeafSwitchGroups processes UFM data into leaf switch groups
func (u *ufmDiscoverer) processLeafSwitchGroups(ufmData []UFMInterface) []LeafSwitchesGroup {
	leafSwitches := u.getLeafSwitches(ufmData)
	return u.classifyLeafs(leafSwitches)
}

// getLeafSwitches extracts leaf switches from UFM data
func (u *ufmDiscoverer) getLeafSwitches(ufmData []UFMInterface) []LeafSwitch {
	leafMap := make(map[string]*LeafSwitch)

	for _, data := range ufmData {
		// Only need to parse computer ufm data because it containers both leaf and computer connection information.
		if !strings.Contains(data.Description, "Computer") {
			continue
		}
		leafName := data.PeerNodeName
		nodeName := data.SystemName

		if _, exists := leafMap[leafName]; !exists {
			leafMap[leafName] = &LeafSwitch{
				Name:      leafName,
				Tier:      data.Tier,
				NodeNames: sets.New[string](nodeName),
			}
		} else {
			leafMap[leafName].NodeNames.Insert(nodeName)
		}
	}

	result := make([]LeafSwitch, 0, len(leafMap))
	for _, leafSwitch := range leafMap {
		result = append(result, *leafSwitch)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}

// classifyLeafs classifies leaf switches into connected groups
func (u *ufmDiscoverer) classifyLeafs(leafSwitches []LeafSwitch) []LeafSwitchesGroup {
	leafMap := make(map[string]LeafSwitch)
	for _, leaf := range leafSwitches {
		leafMap[leaf.Name] = leaf
	}

	leafToNodes := u.buildLeafToNodesMap(leafSwitches)
	nodesToLeafs := u.buildNodeToLeafsMap(leafToNodes)

	leafGroups := make([]LeafSwitchesGroup, 0)
	processedLeafs := sets.New[string]()

	for _, leafSwitch := range leafSwitches {
		if processedLeafs.Has(leafSwitch.Name) {
			continue
		}

		// Use BFS to Find All Connected Leaf Nodes
		queue := []string{leafSwitch.Name}
		leafGroup := LeafSwitchesGroup{
			Leafs:     make(map[string]LeafSwitch),
			NodeNames: sets.New[string](),
		}

		for len(queue) > 0 {
			currentLeaf := queue[0]
			queue = queue[1:]

			if processedLeafs.Has(currentLeaf) {
				continue
			}

			leafGroup.Leafs[currentLeaf] = leafMap[currentLeaf]
			leafGroup.NodeNames.Insert(leafMap[currentLeaf].NodeNames.UnsortedList()...)
			processedLeafs.Insert(currentLeaf)

			// Find all other leafSwitch nodes that have common nodes with the current leafSwitch node
			for node := range leafToNodes[currentLeaf] {
				for relatedLeaf := range nodesToLeafs[node] {
					if !processedLeafs.Has(relatedLeaf) {
						queue = append(queue, relatedLeaf)
					}
				}
			}
		}

		leafGroups = append(leafGroups, leafGroup)
	}

	return leafGroups
}

// buildLeafToNodesMap creates a mapping from leaf switch names to their connected nodes
func (u *ufmDiscoverer) buildLeafToNodesMap(leafSwitches []LeafSwitch) map[string]sets.Set[string] {
	leafToNodes := make(map[string]sets.Set[string])
	for _, leaf := range leafSwitches {
		leafToNodes[leaf.Name] = leaf.NodeNames
	}
	return leafToNodes
}

// buildNodeToLeafsMap creates a mapping from node names to their connected leaf switches
func (u *ufmDiscoverer) buildNodeToLeafsMap(leafToNodes map[string]sets.Set[string]) map[string]sets.Set[string] {
	nodesToLeafs := make(map[string]sets.Set[string])
	for leaf, nodes := range leafToNodes {
		for node := range nodes {
			if _, exist := nodesToLeafs[node]; !exist {
				nodesToLeafs[node] = sets.New[string]()
			}
			nodesToLeafs[node].Insert(leaf)
		}
	}
	return nodesToLeafs
}
