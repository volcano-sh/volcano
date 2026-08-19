/*
Copyright 2026 The Volcano Authors.

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
	"errors"
	"fmt"
	"net"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/component-base/config"
	componentbaseconfigvalidation "k8s.io/component-base/config/validation"

	"volcano.sh/volcano/pkg/kube"
)

const (
	defaultQPS            = 50.0
	defaultBurst          = 100
	defaultHealthzAddress = ":11251"
	defaultListenAddress  = ":8081"
)

// ServerOption contains standalone HyperNode controller settings.
type ServerOption struct {
	KubeClientOptions kube.ClientOptions
	LeaderElection    config.LeaderElectionConfiguration
	PrintVersion      bool
	HealthzAddress    string
	EnableHealthz     bool
	EnableMetrics     bool
	ListenAddress     string
}

// NewServerOption creates standalone HyperNode controller options.
func NewServerOption() *ServerOption {
	return &ServerOption{}
}

// AddFlags registers standalone HyperNode controller flags.
func (s *ServerOption) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeClientOptions.Master, "master", s.KubeClientOptions.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig).")
	fs.StringVar(&s.KubeClientOptions.KubeConfig, "kubeconfig", s.KubeClientOptions.KubeConfig, "Path to kubeconfig file with authorization and master location information.")
	fs.Float32Var(&s.KubeClientOptions.QPS, "kube-api-qps", defaultQPS, "QPS to use while talking with the Kubernetes API server.")
	fs.IntVar(&s.KubeClientOptions.Burst, "kube-api-burst", defaultBurst, "Burst to use while talking with the Kubernetes API server.")
	fs.BoolVar(&s.PrintVersion, "version", false, "Show version and quit.")
	fs.StringVar(&s.HealthzAddress, "healthz-address", defaultHealthzAddress, "The address on which to serve health checks.")
	fs.BoolVar(&s.EnableHealthz, "enable-healthz", false, "Enable the health check endpoint.")
	fs.BoolVar(&s.EnableMetrics, "enable-metrics", false, "Enable the metrics endpoint.")
	fs.StringVar(&s.ListenAddress, "listen-address", defaultListenAddress, "The address on which to serve metrics.")
}

// Validate validates standalone HyperNode controller options.
func (s *ServerOption) Validate() error {
	var errs []error
	if s.KubeClientOptions.QPS <= 0 {
		errs = append(errs, fmt.Errorf("kube-api-qps must be greater than zero"))
	}
	if s.KubeClientOptions.Burst <= 0 {
		errs = append(errs, fmt.Errorf("kube-api-burst must be greater than zero"))
	}
	var healthzAddress, metricsAddress *net.TCPAddr
	if s.EnableHealthz {
		var err error
		healthzAddress, err = net.ResolveTCPAddr("tcp", s.HealthzAddress)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid healthz-address: %w", err))
		}
	}
	if s.EnableMetrics {
		var err error
		metricsAddress, err = net.ResolveTCPAddr("tcp", s.ListenAddress)
		if err != nil {
			errs = append(errs, fmt.Errorf("invalid listen-address: %w", err))
		}
	}
	if listenersOverlap(healthzAddress, metricsAddress) {
		errs = append(errs, fmt.Errorf("healthz-address and listen-address must be different when both endpoints are enabled"))
	}
	if err := componentbaseconfigvalidation.ValidateLeaderElectionConfiguration(
		&s.LeaderElection, field.NewPath("leaderElection")).ToAggregate(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func listenersOverlap(left, right *net.TCPAddr) bool {
	if left == nil || right == nil || left.Port != right.Port {
		return false
	}
	// An unspecified address binds all local interfaces for the address
	// family, so it overlaps a concrete address on the same port.
	return left.IP == nil || left.IP.IsUnspecified() || right.IP == nil || right.IP.IsUnspecified() || left.IP.Equal(right.IP)
}
