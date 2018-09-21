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

	"github.com/spf13/pflag"
)

// ServerOption is the main context object for the controller manager.
type ServerOption struct {
	Master               string
	Kubeconfig           string
	SchedulerName        string
	SchedulerConf        string
	NamespaceAsQueue     bool
	EnableLeaderElection bool
	LockObjectNamespace  string
}

// NewServerOption creates a new CMServer with a default config.
func NewServerOption() *ServerOption {
	s := ServerOption{}
	return &s
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet
func (s *ServerOption) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information")
	// kube-arbitrator will ignore pods with scheduler names other than specified with the option
	fs.StringVar(&s.SchedulerName, "scheduler-name", "kube-batchd", "kube-batchd will handle pods with the scheduler-name")
	fs.StringVar(&s.SchedulerConf, "scheduler-conf", "", "The namespace and name of ConfigMap for scheduler configuration")
	fs.BoolVar(&s.EnableLeaderElection, "leader-elect", s.EnableLeaderElection, "Start a leader election client and gain leadership before "+
		"executing the main loop. Enable this when running replicated kar-scheduler for high availability")
	fs.BoolVar(&s.NamespaceAsQueue, "enable-namespace-as-queue", true, "Make Namespace as Queue with weight one, "+
		"but kube-batchd will not handle Queue CRD anymore")
	fs.StringVar(&s.LockObjectNamespace, "lock-object-namespace", s.LockObjectNamespace, "Define the namespace of the lock object")
}

func (s *ServerOption) CheckOptionOrDie() error {
	if s.EnableLeaderElection && s.LockObjectNamespace == "" {
		return fmt.Errorf("lock-object-namespace must not be nil when LeaderElection is enabled")
	}
	return nil
}
