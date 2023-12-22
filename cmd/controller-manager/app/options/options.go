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
	"os"

	"github.com/spf13/pflag"

	"volcano.sh/volcano/pkg/kube"
)

const (
	defaultQPS                 = 50.0
	defaultBurst               = 100
	defaultWorkers             = 3
	defaultMaxRequeueNum       = 15
	defaultSchedulerName       = "volcano"
	defaultHealthzAddress      = ":11251"
	defaultLockObjectNamespace = "volcano-system"
	defaultPodGroupWorkers     = 5
)

// ServerOption is the main context object for the controllers.
type ServerOption struct {
	KubeClientOptions    kube.ClientOptions
	CertFile             string
	KeyFile              string
	CaCertFile           string
	CertData             []byte
	KeyData              []byte
	CaCertData           []byte
	EnableLeaderElection bool
	LockObjectNamespace  string
	PrintVersion         bool
	// WorkerThreads is the number of threads syncing job operations
	// concurrently. Larger number = faster job updating, but more CPU load.
	WorkerThreads uint32
	// MaxRequeueNum is the number of times a job, queue or command will be requeued before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the times
	// a job, queue or command is going to be requeued:
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	MaxRequeueNum  int
	SchedulerNames []string
	// HealthzBindAddress is the IP address and port for the health check server to serve on,
	// defaulting to 0.0.0.0:11251
	HealthzBindAddress string
	EnableHealthz      bool
	// To determine whether inherit owner's annotations for pods when create podgroup
	InheritOwnerAnnotations bool
	// WorkerThreadsForPG is the number of threads syncing podgroup operations
	// The larger the number, the faster the podgroup processing, but requires more CPU load.
	WorkerThreadsForPG uint32
}

type DecryptFunc func(c *ServerOption) error

// NewServerOption creates a new CMServer with a default config.
func NewServerOption() *ServerOption {
	return &ServerOption{}
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet.
func (s *ServerOption) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&s.KubeClientOptions.Master, "master", s.KubeClientOptions.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.KubeClientOptions.KubeConfig, "kubeconfig", s.KubeClientOptions.KubeConfig, "Path to kubeconfig file with authorization and master location information.")
	fs.StringVar(&s.CaCertFile, "ca-cert-file", s.CaCertFile, "File containing the x509 Certificate for HTTPS.")
	fs.StringVar(&s.CertFile, "tls-cert-file", s.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	fs.StringVar(&s.KeyFile, "tls-private-key-file", s.KeyFile, "File containing the default x509 private key matching --tls-cert-file.")
	fs.BoolVar(&s.EnableLeaderElection, "leader-elect", true, "Start a leader election client and gain leadership before "+
		"executing the main loop. Enable this when running replicated vc-controller-manager for high availability; it is enabled by default")
	fs.StringVar(&s.LockObjectNamespace, "lock-object-namespace", defaultLockObjectNamespace, "Define the namespace of the lock object; it is volcano-system by default")
	fs.Float32Var(&s.KubeClientOptions.QPS, "kube-api-qps", defaultQPS, "QPS to use while talking with kubernetes apiserver")
	fs.IntVar(&s.KubeClientOptions.Burst, "kube-api-burst", defaultBurst, "Burst to use while talking with kubernetes apiserver")
	fs.BoolVar(&s.PrintVersion, "version", false, "Show version and quit")
	fs.Uint32Var(&s.WorkerThreads, "worker-threads", defaultWorkers, "The number of threads syncing job operations concurrently. "+
		"Larger number = faster job updating, but more CPU load")
	fs.StringArrayVar(&s.SchedulerNames, "scheduler-name", []string{defaultSchedulerName}, "Volcano will handle pods whose .spec.SchedulerName is same as scheduler-name")
	fs.IntVar(&s.MaxRequeueNum, "max-requeue-num", defaultMaxRequeueNum, "The number of times a job, queue or command will be requeued before it is dropped out of the queue")
	fs.StringVar(&s.HealthzBindAddress, "healthz-address", defaultHealthzAddress, "The address to listen on for the health check server.")
	fs.BoolVar(&s.EnableHealthz, "enable-healthz", false, "Enable the health check; it is false by default")
	fs.BoolVar(&s.InheritOwnerAnnotations, "inherit-owner-annotations", true, "Enable inherit owner annotations for pods when create podgroup; it is enabled by default")
	fs.Uint32Var(&s.WorkerThreadsForPG, "worker-threads-for-podgroup", defaultPodGroupWorkers, "The number of threads syncing podgroup operations. The larger the number, the faster the podgroup processing, but requires more CPU load.")
}

// CheckOptionOrDie checks the LockObjectNamespace.
func (s *ServerOption) CheckOptionOrDie() error {
	if s.EnableLeaderElection && s.LockObjectNamespace == "" {
		return fmt.Errorf("lock-object-namespace must not be nil when LeaderElection is enabled")
	}
	return nil
}

// readCAFiles read data from ca file path
func (s *ServerOption) readCAFiles() error {
	var err error

	s.CaCertData, err = os.ReadFile(s.CaCertFile)
	if err != nil {
		return fmt.Errorf("failed to read cacert file (%s): %v", s.CaCertFile, err)
	}

	s.CertData, err = os.ReadFile(s.CertFile)
	if err != nil {
		return fmt.Errorf("failed to read cert file (%s): %v", s.CertFile, err)
	}

	s.KeyData, err = os.ReadFile(s.KeyFile)
	if err != nil {
		return fmt.Errorf("failed to read key file (%s): %v", s.KeyFile, err)
	}

	return nil
}

// ParseCAFiles parse ca file by decryptFunc
func (s *ServerOption) ParseCAFiles(decryptFunc DecryptFunc) error {
	if err := s.readCAFiles(); err != nil {
		return err
	}

	// users can add one function to decrypt tha data by their own way if CA data is encrypted
	if decryptFunc != nil {
		return decryptFunc(s)
	}

	return nil
}
