/*
Copyright 2017 The Volcano Authors.

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

	"github.com/spf13/pflag"

	"volcano.sh/volcano/pkg/kube"
)

const (
	defaultQPS           = 50.0
	defaultBurst         = 100
	defaultWorkers       = 3
	defaultSchedulerName = "volcano"
)

// ServerOption is the main context object for the controllers.
type ServerOption struct {
	KubeClientOptions    kube.ClientOptions
	EnableLeaderElection bool
	LockObjectNamespace  string
	PrintVersion         bool
	// WorkerThreads is the number of threads syncing job operations
	// concurrently. Larger number = faster job updating, but more CPU load.
	WorkerThreads uint32
	SchedulerName string
	// HealthzBindAddress is the IP address and port for the health check server to serve on,
	// defaulting to 0.0.0.0:11252
	HealthzBindAddress string
}

// NewServerOption creates a new CMServer with a default config.
func NewServerOption() *ServerOption {
	s := ServerOption{
		HealthzBindAddress: ":11252",
	}
	return &s
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet.
func (s *ServerOption) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeClientOptions.Master, "master", s.KubeClientOptions.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.KubeClientOptions.KubeConfig, "kubeconfig", s.KubeClientOptions.KubeConfig, "Path to kubeconfig file with authorization and master location information.")
	fs.BoolVar(&s.EnableLeaderElection, "leader-elect", s.EnableLeaderElection, "Start a leader election client and gain leadership before "+
		"executing the main loop. Enable this when running replicated vc-controller-manager for high availability.")
	fs.StringVar(&s.LockObjectNamespace, "lock-object-namespace", s.LockObjectNamespace, "Define the namespace of the lock object.")
	fs.Float32Var(&s.KubeClientOptions.QPS, "kube-api-qps", defaultQPS, "QPS to use while talking with kubernetes apiserver")
	fs.IntVar(&s.KubeClientOptions.Burst, "kube-api-burst", defaultBurst, "Burst to use while talking with kubernetes apiserver")
	fs.BoolVar(&s.PrintVersion, "version", false, "Show version and quit")
	fs.Uint32Var(&s.WorkerThreads, "worker-threads", defaultWorkers, "The number of threads syncing job operations concurrently. "+
		"Larger number = faster job updating, but more CPU load")
	fs.StringVar(&s.SchedulerName, "scheduler-name", defaultSchedulerName, "Volcano will handle pods whose .spec.SchedulerName is same as scheduler-name")
}

// CheckOptionOrDie checks the LockObjectNamespace.
func (s *ServerOption) CheckOptionOrDie() error {
	if s.EnableLeaderElection && s.LockObjectNamespace == "" {
		return fmt.Errorf("lock-object-namespace must not be nil when LeaderElection is enabled")
	}
	return nil
}
