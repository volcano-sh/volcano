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
	defaultPercentageOfNodesToFind    = 0
	defaultLockObjectNamespace        = "volcano-system"
	defaultNodeWorkers                = 20
)

// ServerOption is the main context object for the controller manager.
type ServerOption struct {
	KubeClientOptions    kube.ClientOptions
	CertFile             string
	KeyFile              string
	CaCertFile           string
	CertData             []byte
	KeyData              []byte
	CaCertData           []byte
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

	NodeSelector      []string
	CacheDumpFileDir  string
	EnableCacheDumper bool
	NodeWorkerThreads uint32

	// IgnoredCSIProvisioners contains a list of provisioners, and pod request pvc with these provisioners will
	// not be counted in pod pvc resource request and node.Allocatable, because the spec.drivers of csinode resource
	// is always null, these provisioners usually are host path csi controllers like rancher.io/local-path and hostpath.csi.k8s.io.
	IgnoredCSIProvisioners []string

	// CustomResourceGVR is a list of custom resource gvr, user can specify custom resource gvr and then volcano will cache them.
	CustomResourceGVR []string
}

// DecryptFunc is custom function to parse ca file
type DecryptFunc func(c *ServerOption) error

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
	fs.StringVar(&s.CaCertFile, "ca-cert-file", s.CaCertFile, "File containing the x509 Certificate for HTTPS.")
	fs.StringVar(&s.CertFile, "tls-cert-file", s.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	fs.StringVar(&s.KeyFile, "tls-private-key-file", s.KeyFile, "File containing the default x509 private key matching --tls-cert-file.")
	// volcano scheduler will ignore pods with scheduler names other than specified with the option
	fs.StringArrayVar(&s.SchedulerNames, "scheduler-name", []string{defaultSchedulerName}, "vc-scheduler will handle pods whose .spec.SchedulerName is same as scheduler-name")
	fs.StringVar(&s.SchedulerConf, "scheduler-conf", "", "The absolute path of scheduler configuration file")
	fs.DurationVar(&s.SchedulePeriod, "schedule-period", defaultSchedulerPeriod, "The period between each scheduling cycle")
	fs.StringVar(&s.DefaultQueue, "default-queue", defaultQueue, "The default queue name of the job")
	fs.BoolVar(&s.EnableLeaderElection, "leader-elect", true,
		"Start a leader election client and gain leadership before "+
			"executing the main loop. Enable this when running replicated vc-scheduler for high availability; it is enabled by default")
	fs.BoolVar(&s.PrintVersion, "version", false, "Show version and quit")
	fs.StringVar(&s.LockObjectNamespace, "lock-object-namespace", defaultLockObjectNamespace, "Define the namespace of the lock object that is used for leader election; it is volcano-system by default")
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
	fs.BoolVar(&s.EnableCacheDumper, "cache-dumper", true, "Enable the cache dumper, it's true by default")
	fs.StringVar(&s.CacheDumpFileDir, "cache-dump-dir", "/tmp", "The target dir where the json file put at when dump cache info to json file")
	fs.Uint32Var(&s.NodeWorkerThreads, "node-worker-threads", defaultNodeWorkers, "The number of threads syncing node operations.")
	fs.StringSliceVar(&s.IgnoredCSIProvisioners, "ignored-provisioners", nil, "The provisioners that will be ignored during pod pvc request computation and preemption.")
	fs.StringSliceVar(&s.CustomResourceGVR, "custom-resource-gvr", nil, "The custom resource gvr, scheduler framework will cache those custom resources and are accessible."+
		"The format is {resource}.{version}.{group}, eg test.v1.example.io")
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

// Default new and registry a default one
func Default() *ServerOption {
	s := NewServerOption()
	s.AddFlags(pflag.CommandLine)
	s.RegisterOptions()
	return s
}
