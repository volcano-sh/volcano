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

package simulator

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/hack"
	"volcano.sh/volcano/installer"
	"volcano.sh/volcano/pkg/cli/util"
)

type createClusterFlags struct {
	util.CommonFlags
	Config string
}

var createClusterFlag = &createClusterFlags{}

// InitCreateClusterFlags is used to init all flags during cluster creating.
func InitCreateClusterFlags(cmd *cobra.Command) {
	util.InitFlags(cmd, &createClusterFlag.CommonFlags)
	cmd.Flags().StringVar(&createClusterFlag.Config, "config", "", "Path to the JSON configuration file")
}

// NodeConfig struct definition
type NodeConfig struct {
	Name        string            `json:"name"`
	Count       int               `json:"count,omitempty"`
	Capacity    map[string]string `json:"capacity,omitempty"`
	Allocatable map[string]string `json:"allocatable,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Taints      []TaintConfig     `json:"taints,omitempty"`
}

// TaintConfig struct definition
type TaintConfig struct {
	Key    string             `json:"key"`
	Value  string             `json:"value"`
	Effect corev1.TaintEffect `json:"effect"`
}

// CreateCluster is used to create a cluster.
func CreateCluster(clusterName string, ctx context.Context) error {
	scriptBaseDir, err := loadLocalUpScript(true)
	if err != nil {
		klog.Errorf("Failed to load local up simulator script: %v", err)
		return err
	}
	defer os.RemoveAll(scriptBaseDir)

	// 1. create kind cluster and install volcano and install kwok
	localUpCmd := exec.CommandContext(ctx, filepath.Join(scriptBaseDir, "hack", "local-up-volcano.sh"))
	localUpCmd.Dir = scriptBaseDir
	localUpCmd.Env = append(os.Environ(),
		"INSTALL_SIMULATOR=true",
		"SKIP_BUILD_IMAGE=true",
		"TAG=latest",
		"CLUSTER_NAME="+clusterName)
	localUpCmd.Stdout = os.Stdout
	localUpCmd.Stderr = os.Stderr
	if err := localUpCmd.Run(); err != nil {
		return fmt.Errorf("failed to create kind cluster: %v", err)
	}

	// 2. Parse JSON configuration
	nodeConfigs, err := parseNodeConfigs(createClusterFlag.Config)
	if err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}

	// 3.Convert to Node objects
	if len(nodeConfigs) == 0 {
		klog.Infof("No node config provided, use default fake nodes")
		nodeConfigs = genDefaultFakeNodes()
	}
	nodes := convertToNodes(nodeConfigs)

	// 4. Create kube-client and submit nodes
	client, err := createKubeClient(createClusterFlag.CommonFlags)
	if err != nil {
		return fmt.Errorf("failed to create kube-client: %v", err)
	}

	for _, node := range nodes {
		_, err = client.CoreV1().Nodes().Create(ctx, &node, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create node %s: %v", node.Name, err)
		} else {
			klog.Infof("Node %s created successfully", node.Name)
		}
	}

	return nil
}

func loadLocalUpScript(isCreate bool) (string, error) {
	tmpDir, err := os.MkdirTemp("", "vcctl-scripts-*")
	if err != nil {
		return "", err
	}
	klog.Infof("create tmp dir: %s", tmpDir)

	if err := extractScripts(hack.LocalUpVolcanoScript, tmpDir, "hack"); err != nil {
		return tmpDir, err
	}

	if isCreate {
		if err := extractScripts(installer.VolcanoHelmChart, tmpDir, "installer"); err != nil {
			return tmpDir, err
		}
		volcanoChartDir := filepath.Join(tmpDir, "installer/helm/chart/volcano")
		if err := os.Symlink("bases", filepath.Join(volcanoChartDir, "crd/v1")); err != nil {
			return tmpDir, err
		}
		if err := os.Symlink("bases", filepath.Join(volcanoChartDir, "charts/jobflow/crd/v1")); err != nil {
			return tmpDir, err
		}
	}

	return tmpDir, nil
}

// extractScripts extract scripts from the embedded file system to the target directory
func extractScripts(fsys embed.FS, targetDir, subDir string) error {
	return fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		targetPath := filepath.Join(targetDir, subDir, path)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0700)
		}

		data, err := fsys.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(targetPath, data, 0700)
	})
}

func parseNodeConfigs(filePath string) ([]NodeConfig, error) {
	if filePath == "" {
		return nil, nil
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var nodeConfigs []NodeConfig
	err = json.Unmarshal(data, &nodeConfigs)
	if err != nil {
		return nil, err
	}

	return nodeConfigs, nil
}

func convertToNodes(configs []NodeConfig) []corev1.Node {
	var nodes []corev1.Node

	for _, config := range configs {
		// Handle Count field, default to 1
		count := config.Count
		if count <= 0 {
			count = 1
		}

		// Create count nodes for each configuration
		for i := 1; i <= count; i++ {
			var nodeName string
			if count == 1 {
				nodeName = config.Name
			} else {
				// Generate names with suffix for multiple nodes
				nodeName = fmt.Sprintf("%s-%d", config.Name, i)
			}

			// Convert capacity resources
			capacity := make(corev1.ResourceList)
			for key, value := range config.Capacity {
				quantity, err := resource.ParseQuantity(value)
				if err != nil {
					klog.Errorf("Error parsing capacity quantity for node %s: %v", nodeName, err)
					continue
				}
				capacity[corev1.ResourceName(key)] = quantity
			}

			// Convert allocatable resources (if provided)
			allocatable := make(corev1.ResourceList)
			if config.Allocatable != nil {
				for key, value := range config.Allocatable {
					quantity, err := resource.ParseQuantity(value)
					if err != nil {
						klog.Errorf("Error parsing allocatable quantity for node %s: %v", nodeName, err)
						continue
					}
					allocatable[corev1.ResourceName(key)] = quantity
				}
			} else {
				// If allocatable not specified, use capacity
				allocatable = capacity
			}

			// Merge labels
			labels := map[string]string{
				"beta.kubernetes.io/arch":       "amd64",
				"beta.kubernetes.io/os":         "linux",
				"kubernetes.io/arch":            "amd64",
				"kubernetes.io/hostname":        nodeName,
				"kubernetes.io/os":              "linux",
				"kubernetes.io/role":            "agent",
				"node-role.kubernetes.io/agent": "",
				"type":                          "kwok",
			}
			for k, v := range config.Labels {
				labels[k] = v
			}

			// Merge annotations
			annotations := map[string]string{
				"node.alpha.kubernetes.io/ttl": "0",
				"kwok.x-k8s.io/node":           "fake",
			}
			for k, v := range config.Annotations {
				annotations[k] = v
			}

			// Convert taints
			var taints []corev1.Taint
			// Add default taint
			taints = append(taints, corev1.Taint{
				Key:    "kwok.x-k8s.io/node",
				Value:  "fake",
				Effect: corev1.TaintEffectNoSchedule,
			})
			// Add configured taints
			for _, t := range config.Taints {
				taints = append(taints, corev1.Taint{
					Key:    t.Key,
					Value:  t.Value,
					Effect: t.Effect,
				})
			}

			node := corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:        nodeName,
					Annotations: annotations,
					Labels:      labels,
				},
				Spec: corev1.NodeSpec{
					Taints: taints,
				},
				Status: corev1.NodeStatus{
					Capacity:    capacity,
					Allocatable: allocatable,
					Phase:       corev1.NodeRunning,
					NodeInfo: corev1.NodeSystemInfo{
						Architecture:    "amd64",
						KubeletVersion:  "fake",
						OperatingSystem: "linux",
					},
				},
			}

			// Set default pods capacity if not specified
			if _, exists := capacity[corev1.ResourcePods]; !exists {
				podsQuantity, _ := resource.ParseQuantity("110")
				node.Status.Capacity[corev1.ResourcePods] = podsQuantity
				node.Status.Allocatable[corev1.ResourcePods] = podsQuantity
			}
			// Set default pods capacity if not specified
			if _, exists := capacity[corev1.ResourceCPU]; !exists {
				cpuQuantity, _ := resource.ParseQuantity("32")
				node.Status.Capacity[corev1.ResourceCPU] = cpuQuantity
				node.Status.Allocatable[corev1.ResourceCPU] = cpuQuantity
			}
			// Set default pods capacity if not specified
			if _, exists := capacity[corev1.ResourceMemory]; !exists {
				memoryQuantity, _ := resource.ParseQuantity("256Gi")
				node.Status.Capacity[corev1.ResourceMemory] = memoryQuantity
				node.Status.Allocatable[corev1.ResourceMemory] = memoryQuantity
			}

			nodes = append(nodes, node)
		}
	}

	return nodes
}

func genDefaultFakeNodes() []NodeConfig {
	return []NodeConfig{
		{
			Name:  "kwok-node",
			Count: 3,
		},
	}
}

// createKubeClient is used to create kube-client.
func createKubeClient(commonFlags util.CommonFlags) (kubernetes.Interface, error) {
	master, kubeConfig := "", filepath.Join(os.Getenv("HOME"), ".kube", "config")
	if commonFlags.Master != "" {
		master = commonFlags.Master
	}
	if commonFlags.Kubeconfig != "" {
		kubeConfig = commonFlags.Kubeconfig
	}

	config, err := clientcmd.BuildConfigFromFlags(master, kubeConfig)
	if err != nil {
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}
