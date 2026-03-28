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

package options

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/component-base/config"
	componentbaseconfigvalidation "k8s.io/component-base/config/validation"
)

const (
	defaultQPS            = 50.0
	defaultBurst          = 100
	defaultHealthzAddress = ":11252"
	defaultLockNamespace  = "volcano-system"
)

var (
	defaultElectionLeaseDuration = metav1.Duration{Duration: 15 * time.Second}
	defaultElectionRenewDeadline = metav1.Duration{Duration: 10 * time.Second}
	defaultElectionRetryPeriod   = metav1.Duration{Duration: 2 * time.Second}
)

// KubeClientOptions configures the Kubernetes API client.
type KubeClientOptions struct {
	Master     string
	KubeConfig string
	QPS        float32
	Burst      int
}

// ServerOption configures the standalone HyperNode controller process.
type ServerOption struct {
	KubeClientOptions KubeClientOptions
	CertFile          string
	KeyFile           string
	CaCertFile        string
	CertData          []byte
	KeyData           []byte
	CaCertData        []byte
	LeaderElection    config.LeaderElectionConfiguration
	LockObjectNamespace string
	PrintVersion      bool
	HealthzBindAddress string
	EnableHealthz     bool
}

// NewServerOption returns defaults for the HyperNode controller binary.
func NewServerOption() *ServerOption {
	s := &ServerOption{}
	leaderElectionDefault(&s.LeaderElection)
	s.LeaderElection.ResourceName = "vc-hypernode-controller"
	s.KubeClientOptions.QPS = defaultQPS
	s.KubeClientOptions.Burst = defaultBurst
	s.HealthzBindAddress = defaultHealthzAddress
	return s
}

func leaderElectionDefault(l *config.LeaderElectionConfiguration) {
	l.LeaderElect = true
	l.LeaseDuration = defaultElectionLeaseDuration
	l.RenewDeadline = defaultElectionRenewDeadline
	l.RetryPeriod = defaultElectionRetryPeriod
	l.ResourceLock = resourcelock.LeasesResourceLock
	l.ResourceNamespace = defaultLockNamespace
}

// AddFlags registers CLI flags.
func (s *ServerOption) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeClientOptions.Master, "master", s.KubeClientOptions.Master, "The address of the Kubernetes API server (overrides kubeconfig)")
	fs.StringVar(&s.KubeClientOptions.KubeConfig, "kubeconfig", s.KubeClientOptions.KubeConfig, "Path to kubeconfig file")
	fs.Float32Var(&s.KubeClientOptions.QPS, "kube-api-qps", defaultQPS, "QPS while talking with the apiserver")
	fs.IntVar(&s.KubeClientOptions.Burst, "kube-api-burst", defaultBurst, "Burst while talking with the apiserver")
	fs.StringVar(&s.CaCertFile, "ca-cert-file", s.CaCertFile, "File containing the x509 CA for HTTPS health/metrics")
	fs.StringVar(&s.CertFile, "tls-cert-file", s.CertFile, "TLS certificate file for HTTPS")
	fs.StringVar(&s.KeyFile, "tls-private-key-file", s.KeyFile, "TLS private key file")
	fs.StringVar(&s.LockObjectNamespace, "lock-object-namespace", "", "Deprecated: use leader-elect-resource-namespace")
	fs.BoolVar(&s.PrintVersion, "version", false, "Print version and exit")
	fs.StringVar(&s.HealthzBindAddress, "healthz-address", defaultHealthzAddress, "Health check listen address")
	fs.BoolVar(&s.EnableHealthz, "enable-healthz", false, "Enable the healthz HTTP server")
}

// CheckOptionOrDie validates options.
func (s *ServerOption) CheckOptionOrDie() error {
	var allErrors []error
	if err := componentbaseconfigvalidation.ValidateLeaderElectionConfiguration(
		&s.LeaderElection, field.NewPath("leaderElection")).ToAggregate(); err != nil {
		allErrors = append(allErrors, err)
	}
	return errors.NewAggregate(allErrors)
}

// ReadCAFiles loads TLS material from paths set via flags.
func (s *ServerOption) ReadCAFiles() error {
	if s.CaCertFile == "" && s.CertFile == "" && s.KeyFile == "" {
		return nil
	}
	if s.CaCertFile == "" || s.CertFile == "" || s.KeyFile == "" {
		return fmt.Errorf("ca-cert-file, tls-cert-file, and tls-private-key-file must be set together")
	}
	var err error
	s.CaCertData, err = os.ReadFile(s.CaCertFile)
	if err != nil {
		return fmt.Errorf("read ca-cert-file: %w", err)
	}
	s.CertData, err = os.ReadFile(s.CertFile)
	if err != nil {
		return fmt.Errorf("read tls-cert-file: %w", err)
	}
	s.KeyData, err = os.ReadFile(s.KeyFile)
	if err != nil {
		return fmt.Errorf("read tls-private-key-file: %w", err)
	}
	return nil
}
