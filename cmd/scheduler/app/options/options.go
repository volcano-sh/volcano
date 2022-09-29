/*
Copyright 2017 The Kubernetes Authors.

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
	"time"

	"github.com/spf13/pflag"

	"volcano.sh/volcano/pkg/kube"
)

const (
	defaultSchedulerName   = "volcano"
	defaultSchedulerPeriod = time.Second
	defaultQueue           = "default"
	defaultListenAddress   = ":8080"
	defaultHealthzAddress  = ":11251"
	defaultPluginsDir      = ""

	defaultQPS   = 2000.0
	defaultBurst = 2000

	// Default parameters to control the number of feasible nodes to find and score
	defaultMinPercentageOfNodesToFind = 5
	defaultMinNodesToFind             = 100
	defaultPercentageOfNodesToFind    = 100
)

// ServerOption is the main context object for the controller manager.
type ServerOption struct {
	KubeClientOptions    kube.ClientOptions
	SchedulerNames       []string
	SchedulerConf        string
	SchedulePeriod       time.Duration
	EnableLeaderElection bool
	LockObjectNamespace  string
	DefaultQueue         string
	PrintVersion         bool
	EnableMetrics        bool
	ListenAddress        string
	EnablePriorityClass  bool
	EnableCSIStorage     bool
	// vc-scheduler will load (not activate) custom plugins which are in this directory
	PluginsDir    string
	EnableHealthz bool
	// HealthzBindAddress is the IP address and port for the health check server to serve on
	// defaulting to :11251
	HealthzBindAddress string
	// Parameters for scheduling tuning: the number of feasible nodes to find and score
	MinNodesToFind             int32
	MinPercentageOfNodesToFind int32
	PercentageOfNodesToFind    int32

	NodeSelector []string
}

// ServerOpts server options.
var ServerOpts *ServerOption

// NewServerOption creates a new CMServer with a default config.
func NewServerOption() *ServerOption {
	return &ServerOption{}
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet.
func (s *ServerOption) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeClientOptions.Master, "master", s.KubeClientOptions.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.KubeClientOptions.KubeConfig, "kubeconfig", s.KubeClientOptions.KubeConfig, "Path to kubeconfig file with authorization and master location information")
	// volcano scheduler will ignore pods with scheduler names other than specified with the option
	fs.StringArrayVar(&s.SchedulerNames, "scheduler-name", []string{defaultSchedulerName}, "vc-scheduler will handle pods whose .spec.SchedulerName is same as scheduler-name")
	fs.StringVar(&s.SchedulerConf, "scheduler-conf", "", "The absolute path of scheduler configuration file")
	fs.DurationVar(&s.SchedulePeriod, "schedule-period", defaultSchedulerPeriod, "The period between each scheduling cycle")
	fs.StringVar(&s.DefaultQueue, "default-queue", defaultQueue, "The default queue name of the job")
	fs.BoolVar(&s.EnableLeaderElection, "leader-elect", s.EnableLeaderElection,
		"Start a leader election client and gain leadership before "+
			"executing the main loop. Enable this when running replicated vc-scheduler for high availability")
	fs.BoolVar(&s.PrintVersion, "version", false, "Show version and quit")
	fs.StringVar(&s.LockObjectNamespace, "lock-object-namespace", s.LockObjectNamespace, "Define the namespace of the lock object that is used for leader election")
	fs.StringVar(&s.ListenAddress, "listen-address", defaultListenAddress, "The address to listen on for HTTP requests.")
	fs.StringVar(&s.HealthzBindAddress, "healthz-address", defaultHealthzAddress, "The address to listen on for the health check server.")
	fs.BoolVar(&s.EnablePriorityClass, "priority-class", true,
		"Enable PriorityClass to provide the capacity of preemption at pod group level; to disable it, set it false")
	fs.Float32Var(&s.KubeClientOptions.QPS, "kube-api-qps", defaultQPS, "QPS to use while talking with kubernetes apiserver")
	fs.IntVar(&s.KubeClientOptions.Burst, "kube-api-burst", defaultBurst, "Burst to use while talking with kubernetes apiserver")

	// Minimum number of feasible nodes to find and score
	fs.Int32Var(&s.MinNodesToFind, "minimum-feasible-nodes", defaultMinNodesToFind, "The minimum number of feasible nodes to find and score")

	// Minimum percentage of nodes to find and score
	fs.Int32Var(&s.MinPercentageOfNodesToFind, "minimum-percentage-nodes-to-find", defaultMinPercentageOfNodesToFind, "The minimum percentage of nodes to find and score")

	// The percentage of nodes that would be scored in each scheduling cycle; if <= 0, an adpative percentage will be calcuated
	fs.Int32Var(&s.PercentageOfNodesToFind, "percentage-nodes-to-find", defaultPercentageOfNodesToFind, "The percentage of nodes to find and score, if <=0 will be calcuated based on the cluster size")

	fs.StringVar(&s.PluginsDir, "plugins-dir", defaultPluginsDir, "vc-scheduler will load custom plugins which are in this directory")
	fs.BoolVar(&s.EnableCSIStorage, "csi-storage", false,
		"Enable tracking of available storage capacity that CSI drivers provide; it is false by default")
	fs.BoolVar(&s.EnableHealthz, "enable-healthz", false, "Enable the health check; it is false by default")
	fs.BoolVar(&s.EnableMetrics, "enable-metrics", false, "Enable the metrics function; it is false by default")
	fs.StringSliceVar(&s.NodeSelector, "node-selector", nil, "volcano only work with the labeled node, like: --node-selector=volcano.sh/role:train --node-selector=volcano.sh/role:serving")
}

// CheckOptionOrDie check lock-object-namespace when LeaderElection is enabled.
func (s *ServerOption) CheckOptionOrDie() error {
	if s.EnableLeaderElection && s.LockObjectNamespace == "" {
		return fmt.Errorf("lock-object-namespace must not be nil when LeaderElection is enabled")
	}

	return nil
}

// RegisterOptions registers options.
func (s *ServerOption) RegisterOptions() {
	ServerOpts = s
}
