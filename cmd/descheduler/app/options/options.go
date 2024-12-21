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

Copyright 2024 The Volcano Authors.

Modifications made by Volcano authors:
- [2023]Add `DeschedulingIntervalCronExpression` flag
*/

// Package options provides the descheduler flags
package options

import (
	"time"

	"github.com/spf13/pflag"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiserveroptions "k8s.io/apiserver/pkg/server/options"
	clientset "k8s.io/client-go/kubernetes"
	componentbaseconfig "k8s.io/component-base/config"
	componentbaseoptions "k8s.io/component-base/config/options"

	"volcano.sh/volcano/pkg/descheduler/apis/componentconfig"
)

const (
	DefaultDeschedulerPort = 10258
)

// DeschedulerServer configuration
type DeschedulerServer struct {
	componentconfig.DeschedulerConfiguration

	Client         clientset.Interface
	EventClient    clientset.Interface
	SecureServing  *apiserveroptions.SecureServingOptionsWithLoopback
	DisableMetrics bool
}

// NewDeschedulerServer creates a new DeschedulerServer with default parameters
func NewDeschedulerServer() (*DeschedulerServer, error) {
	cfg, err := newDefaultComponentConfig()
	if err != nil {
		return nil, err
	}

	secureServing := apiserveroptions.NewSecureServingOptions().WithLoopback()
	secureServing.BindPort = DefaultDeschedulerPort

	return &DeschedulerServer{
		DeschedulerConfiguration: *cfg,
		SecureServing:            secureServing,
	}, nil
}

func newDefaultComponentConfig() (*componentconfig.DeschedulerConfiguration, error) {
	versionedCfg := componentconfig.DeschedulerConfiguration{
		LeaderElection: componentbaseconfig.LeaderElectionConfiguration{
			LeaderElect:       false,
			LeaseDuration:     metav1.Duration{Duration: 137 * time.Second},
			RenewDeadline:     metav1.Duration{Duration: 107 * time.Second},
			RetryPeriod:       metav1.Duration{Duration: 26 * time.Second},
			ResourceLock:      "leases",
			ResourceName:      "vc-descheduler",
			ResourceNamespace: "volcano-system",
		},
	}
	return &versionedCfg, nil
}

// AddFlags adds flags for a specific SchedulerServer to the specified FlagSet
func (rs *DeschedulerServer) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&rs.Logging.Format, "logging-format", "text", `Sets the log format. Permitted formats: "text", "json". Non-default formats don't honor these flags: --add-dir-header, --alsologtostderr, --log-backtrace-at, --log_dir, --log_file, --log_file_max_size, --logtostderr, --skip-headers, --skip-log-headers, --stderrthreshold, --log-flush-frequency.\nNon-default choices are currently alpha and subject to change without warning.`)
	fs.DurationVar(&rs.DeschedulingInterval, "descheduling-interval", rs.DeschedulingInterval, "Time interval between two consecutive descheduler executions. Setting this value instructs the descheduler to run in a continuous loop at the interval specified.")
	fs.StringVar(&rs.ClientConnection.Kubeconfig, "kubeconfig", rs.ClientConnection.Kubeconfig, "File with kube configuration. Deprecated, use client-connection-kubeconfig instead.")
	fs.StringVar(&rs.ClientConnection.Kubeconfig, "client-connection-kubeconfig", rs.ClientConnection.Kubeconfig, "File path to kube configuration for interacting with kubernetes apiserver.")
	fs.Float32Var(&rs.ClientConnection.QPS, "client-connection-qps", rs.ClientConnection.QPS, "QPS to use for interacting with kubernetes apiserver.")
	fs.Int32Var(&rs.ClientConnection.Burst, "client-connection-burst", rs.ClientConnection.Burst, "Burst to use for interacting with kubernetes apiserver.")
	fs.StringVar(&rs.PolicyConfigFile, "policy-config-file", rs.PolicyConfigFile, "File with descheduler policy configuration.")
	fs.BoolVar(&rs.DryRun, "dry-run", rs.DryRun, "Execute descheduler in dry run mode.")
	fs.BoolVar(&rs.DisableMetrics, "disable-metrics", rs.DisableMetrics, "Disables metrics. The metrics are by default served through https://localhost:10258/metrics. Secure address, resp. port can be changed through --bind-address, resp. --secure-port flags.")
	fs.StringVar(&rs.DeschedulingIntervalCronExpression, "descheduling-interval-cron-expression", rs.DeschedulingIntervalCronExpression, "Time interval between two consecutive descheduler executions. Cron expression is allowed to use to configure this parameter.")

	componentbaseoptions.BindLeaderElectionFlags(&rs.LeaderElection, fs)

	rs.SecureServing.AddFlags(fs)
}
