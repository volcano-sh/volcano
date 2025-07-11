/*
Copyright 2024 The Volcano Authors.

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

package options

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"volcano.sh/volcano/pkg/config"
)

const (
	serverPort = 3300

	defaultOverSubscriptionRatio = 60
)

type VolcanoAgentOptions struct {
	// HealthzAddress is the health check server address
	HealthzAddress string

	//HealthzPort is the health check server port
	HealthzPort int

	// KubeCgroupRoot is the root cgroup to use for pods.
	// If CgroupsPerQOS is enabled, this is the root of the QoS cgroup hierarchy.
	KubeCgroupRoot string

	// KubeNodeName is the name of the node which controller is running.
	KubeNodeName string

	// List of supported features, '*' supports all on-by-default features.
	SupportedFeatures []string

	// KubePodName is the name of the pod.
	KubePodName string

	// KubePodName is the namespace of the pod.
	KubePodNamespace string

	// OverSubscriptionPolicy defines overSubscription policy.
	OverSubscriptionPolicy string

	// OverSubscriptionRatio is the over subscription ratio of idle resources, default to 60, which means 60%.
	OverSubscriptionRatio int

	// IncludeSystemUsage determines whether considering system usage when calculate overSubscription resource and evict.
	IncludeSystemUsage bool

	// ExtendResourceCPUName is the extend resource cpu, which is used to calculate overSubscription resources.
	ExtendResourceCPUName string

	// ExtendResourceMemoryName is the extend resource memory, which is used to calculate overSubscription resources.
	ExtendResourceMemoryName string
}

func NewVolcanoAgentOptions() *VolcanoAgentOptions {
	return &VolcanoAgentOptions{}
}

func (options *VolcanoAgentOptions) AddFlags(c *cobra.Command) {
	c.Flags().StringSliceVar(&options.SupportedFeatures, "supported-features", []string{"*"}, "List of supported features. '*' supports all on-by-default features, 'foo' feature named 'foo' is supported"+
		"'-foo' feature named 'foo' is not supported.")
	c.Flags().StringVar(&options.HealthzAddress, "healthz-address", "", "defines the health check address")
	c.Flags().IntVar(&options.HealthzPort, "healthz-port", serverPort, "defines the health check port")
	c.Flags().StringVar(&options.KubeCgroupRoot, "kube-cgroup-root", options.KubeCgroupRoot, "kube cgroup root")
	c.Flags().StringVar(&options.KubeNodeName, "kube-node-name", os.Getenv("KUBE_NODE_NAME"), "the related kube-node name of the host, where the pod run in")
	c.Flags().StringVar(&options.KubePodName, "kube-pod-name", os.Getenv("KUBE_POD_NAME"), "the name of the pod")
	c.Flags().StringVar(&options.KubePodNamespace, "kube-pod-namespace", os.Getenv("KUBE_POD_NAMESPACE"), "the namespace of the pod")
	c.Flags().StringVar(&options.OverSubscriptionPolicy, "oversubscription-policy", "extend", "The oversubscription policy determines where oversubscription resources to report and how to use, default to extend means report to extend resources")
	// TODO: put in configMap.
	c.Flags().IntVar(&options.OverSubscriptionRatio, "oversubscription-ratio", defaultOverSubscriptionRatio, "The oversubscription ratio determines how many idle resources can be oversold")
	c.Flags().BoolVar(&options.IncludeSystemUsage, "include-system-usage", false, "It determines whether considering system usage when calculate overSubscription resource and evict.")
	c.Flags().StringVar(&options.ExtendResourceCPUName, "extend-resource-cpu-name", "", "The extended cpu resource name, which is used to calculate oversubscription resources, default to kubernetes.io/batch-cpu")
	c.Flags().StringVar(&options.ExtendResourceMemoryName, "extend-resource-memory-name", "", "The extended memory resource name, which is used to calculate oversubscription resources, default to kubernetes.io/batch-memory")
}

func (options *VolcanoAgentOptions) Validate() error {
	if options.OverSubscriptionRatio <= 0 {
		return fmt.Errorf("over subscription ratio must be greater than 0")
	}
	return nil
}

func (options *VolcanoAgentOptions) ApplyTo(cfg *config.Configuration) error {
	cfg.GenericConfiguration.HealthzAddress = options.HealthzAddress
	cfg.GenericConfiguration.HealthzPort = options.HealthzPort
	cfg.GenericConfiguration.KubeCgroupRoot = options.KubeCgroupRoot
	cfg.GenericConfiguration.KubeNodeName = options.KubeNodeName
	cfg.GenericConfiguration.SupportedFeatures = options.SupportedFeatures
	cfg.GenericConfiguration.KubePodName = options.KubePodName
	cfg.GenericConfiguration.KubePodNamespace = options.KubePodNamespace
	cfg.GenericConfiguration.OverSubscriptionPolicy = options.OverSubscriptionPolicy
	cfg.GenericConfiguration.OverSubscriptionRatio = options.OverSubscriptionRatio
	cfg.GenericConfiguration.IncludeSystemUsage = options.IncludeSystemUsage
	cfg.GenericConfiguration.ExtendResourceCPUName = options.ExtendResourceCPUName
	cfg.GenericConfiguration.ExtendResourceMemoryName = options.ExtendResourceMemoryName
	return nil
}
