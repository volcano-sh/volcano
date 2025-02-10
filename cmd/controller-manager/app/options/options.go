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
	"strings"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/component-base/config"
	componentbaseconfigvalidation "k8s.io/component-base/config/validation"

	"volcano.sh/volcano/pkg/kube"
)

const (
	defaultQPS                 = 50.0
	defaultBurst               = 100
	defaultWorkers             = 3
	defaultMaxRequeueNum       = 15
	defaultSchedulerName       = "volcano"
	defaultHealthzAddress      = ":11251"
	defaultListenAddress       = ":8081"
	defaultLockObjectNamespace = "volcano-system"
	defaultPodGroupWorkers     = 5
	defaultQueueWorkers        = 5
	defaultGCWorkers           = 1
	defaultControllers         = "*"
)

// ServerOption is the main context object for the controllers.
type ServerOption struct {
	KubeClientOptions kube.ClientOptions
	CertFile          string
	KeyFile           string
	CaCertFile        string
	CertData          []byte
	KeyData           []byte
	CaCertData        []byte
	// leaderElection defines the configuration of leader election.
	LeaderElection config.LeaderElectionConfiguration
	// Deprecated: use ResourceNamespace instead.
	LockObjectNamespace string
	PrintVersion        bool
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
	EnableMetrics      bool
	ListenAddress      string
	// To determine whether inherit owner's annotations for pods when create podgroup
	InheritOwnerAnnotations bool
	// WorkerThreadsForPG is the number of threads syncing podgroup operations
	// The larger the number, the faster the podgroup processing, but requires more CPU load.
	WorkerThreadsForPG uint32
	// WorkerThreadsForQueue is the number of threads syncing queue operations
	// The larger the number, the faster the queue processing, but requires more CPU load.
	WorkerThreadsForQueue uint32
	// WorkerThreadsForGC is the number of threads for recycling jobs
	// The larger the number, the faster the job recycling, but requires more CPU load.
	WorkerThreadsForGC uint32
	// Controllers specify controllers to set up.
	// Case1: Use '*' for all controllers,
	// Case2: "+gc-controller,+job-controller,+jobflow-controller,+jobtemplate-controller,+pg-controller,+queue-controller"
	// to enable specific controllers,
	// Case3: "-gc-controller,-job-controller,-jobflow-controller,-jobtemplate-controller,-pg-controller,-queue-controller"
	// to disable specific controllers,
	Controllers []string
}

type DecryptFunc func(c *ServerOption) error

// NewServerOption creates a new CMServer with a default config.
func NewServerOption() *ServerOption {
	return &ServerOption{}
}

// AddFlags adds flags for a specific CMServer to the specified FlagSet.
func (s *ServerOption) AddFlags(fs *pflag.FlagSet, knownControllers []string) {
	fs.StringVar(&s.KubeClientOptions.Master, "master", s.KubeClientOptions.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	fs.StringVar(&s.KubeClientOptions.KubeConfig, "kubeconfig", s.KubeClientOptions.KubeConfig, "Path to kubeconfig file with authorization and master location information.")
	fs.StringVar(&s.CaCertFile, "ca-cert-file", s.CaCertFile, "File containing the x509 Certificate for HTTPS.")
	fs.StringVar(&s.CertFile, "tls-cert-file", s.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	fs.StringVar(&s.KeyFile, "tls-private-key-file", s.KeyFile, "File containing the default x509 private key matching --tls-cert-file.")
	fs.StringVar(&s.LockObjectNamespace, "lock-object-namespace", "", "Define the namespace of the lock object; it is volcano-system by default.")
	fs.MarkDeprecated("lock-object-namespace", "This flag is deprecated and will be removed in a future release. Please use --leader-elect-resource-namespace instead.")
	fs.Float32Var(&s.KubeClientOptions.QPS, "kube-api-qps", defaultQPS, "QPS to use while talking with kubernetes apiserver")
	fs.IntVar(&s.KubeClientOptions.Burst, "kube-api-burst", defaultBurst, "Burst to use while talking with kubernetes apiserver")
	fs.BoolVar(&s.PrintVersion, "version", false, "Show version and quit")
	fs.Uint32Var(&s.WorkerThreads, "worker-threads", defaultWorkers, "The number of threads syncing job operations concurrently. "+
		"Larger number = faster job updating, but more CPU load")
	fs.StringArrayVar(&s.SchedulerNames, "scheduler-name", []string{defaultSchedulerName}, "Volcano will handle pods whose .spec.SchedulerName is same as scheduler-name")
	fs.IntVar(&s.MaxRequeueNum, "max-requeue-num", defaultMaxRequeueNum, "The number of times a job, queue or command will be requeued before it is dropped out of the queue")
	fs.StringVar(&s.HealthzBindAddress, "healthz-address", defaultHealthzAddress, "The address to listen on for the health check server.")
	fs.BoolVar(&s.EnableHealthz, "enable-healthz", false, "Enable the health check; it is false by default")
	fs.BoolVar(&s.EnableMetrics, "enable-metrics", false, "Enable the metrics function; it is false by default")
	fs.StringVar(&s.ListenAddress, "listen-address", defaultListenAddress, "The address to listen on for HTTP requests.")
	fs.BoolVar(&s.InheritOwnerAnnotations, "inherit-owner-annotations", true, "Enable inherit owner annotations for pods when create podgroup; it is enabled by default")
	fs.Uint32Var(&s.WorkerThreadsForPG, "worker-threads-for-podgroup", defaultPodGroupWorkers, "The number of threads syncing podgroup operations. The larger the number, the faster the podgroup processing, but requires more CPU load.")
	fs.Uint32Var(&s.WorkerThreadsForGC, "worker-threads-for-gc", defaultGCWorkers, "The number of threads for recycling jobs. The larger the number, the faster the job recycling, but requires more CPU load.")
	fs.Uint32Var(&s.WorkerThreadsForQueue, "worker-threads-for-queue", defaultQueueWorkers, "The number of threads syncing queue operations. The larger the number, the faster the queue processing, but requires more CPU load.")
	fs.StringSliceVar(&s.Controllers, "controllers", []string{defaultControllers}, fmt.Sprintf("Specify controller gates. Use '*' for all controllers, all knownController: %s ,and we can use "+
		"'-' to disable controllers, e.g. \"-job-controller,-queue-controller\" to disable job and queue controllers.", knownControllers))
}

// CheckOptionOrDie checks all options and returns all errors if they are invalid.
// If there are any invalid options, it aggregates all the errors and returns them.
func (s *ServerOption) CheckOptionOrDie() error {
	var allErrors []error

	// Check controllers option
	if err := s.checkControllers(); err != nil {
		allErrors = append(allErrors, err)
	}

	// Check leader election flag when LeaderElection is enabled.
	leaderElectionErr := componentbaseconfigvalidation.ValidateLeaderElectionConfiguration(
		&s.LeaderElection, field.NewPath("leaderElection")).ToAggregate()
	if leaderElectionErr != nil {
		allErrors = append(allErrors, leaderElectionErr)
	}
	return errors.NewAggregate(allErrors)
}

// checkControllers checks the controllers option and returns error if it's invalid
func (s *ServerOption) checkControllers() error {
	existenceMap := make(map[string]bool)
	for _, c := range s.Controllers {
		if c == "*" {
			// wildcard '*' is not allowed to be combined with other input
			if len(s.Controllers) > 1 {
				return fmt.Errorf("wildcard '*' cannot be combined with other input")
			}
		} else {
			if strings.HasPrefix(c, "-") || strings.HasPrefix(c, "+") {
				if existenceMap[c[1:]] {
					return fmt.Errorf("controllers option %s cannot have both '-' and '+' prefixes", c)
				}
				existenceMap[c[1:]] = true
			} else {
				if existenceMap[c] {
					return fmt.Errorf("controllers option %s cannot have both '-' and '+' prefixes", c)
				}
				existenceMap[c] = true
			}
		}
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
