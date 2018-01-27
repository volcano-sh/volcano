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
	"os"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/quotalloc/policy/proportion"
	"github.com/spf13/pflag"
)

// ServerOption is the main context object for the controller manager.
type ServerOption struct {
	Master     string
	Kubeconfig string
	Policy     string
}

// NewServerOption creates a new CMServer with a default config.
func NewServerOption() *ServerOption {
	s := ServerOption{}
	return &s
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet
func (s *ServerOption) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	// The default policy is Proportion policy.
	fs.StringVar(&s.Policy, "policy", proportion.PolicyName, "The policy that used to allocate resources")
}

func (s *ServerOption) CheckOptionOrDie() {
	switch s.Policy {
	case proportion.PolicyName:
	default:
		fmt.Fprintf(os.Stderr, "invalid policy name %s\n", s.Policy)
		os.Exit(1)
	}
}
