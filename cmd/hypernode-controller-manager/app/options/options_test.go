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
	"testing"

	"github.com/spf13/pflag"
	componentbaseoptions "k8s.io/component-base/config/options"

	commonutil "volcano.sh/volcano/pkg/util"
)

func TestAddFlags(t *testing.T) {
	options := NewServerOption()
	commonutil.LeaderElectionDefault(&options.LeaderElection)
	options.LeaderElection.ResourceName = "vc-hypernode-controller-manager"
	flags := pflag.NewFlagSet("hypernode-controller-manager", pflag.ContinueOnError)
	options.AddFlags(flags)
	componentbaseoptions.BindLeaderElectionFlags(&options.LeaderElection, flags)
	if err := flags.Parse([]string{"--kube-api-qps=75", "--kube-api-burst=150", "--enable-healthz=true", "--enable-metrics=true"}); err != nil {
		t.Fatal(err)
	}
	if options.KubeClientOptions.QPS != 75 || options.KubeClientOptions.Burst != 150 {
		t.Fatalf("unexpected client limits: QPS=%v Burst=%v", options.KubeClientOptions.QPS, options.KubeClientOptions.Burst)
	}
	if !options.EnableHealthz || !options.EnableMetrics {
		t.Fatal("health and metrics flags were not applied")
	}
	if err := options.Validate(); err != nil {
		t.Fatalf("valid options were rejected: %v", err)
	}
}

func TestValidateRejectsInvalidClientAndListenerSettings(t *testing.T) {
	options := NewServerOption()
	options.KubeClientOptions.QPS = -1
	options.KubeClientOptions.Burst = 0
	options.EnableHealthz = true
	options.HealthzAddress = "bad-address:port"
	options.EnableMetrics = true
	options.ListenAddress = "bad-address:port"
	if err := options.Validate(); err == nil {
		t.Fatal("Validate() accepted invalid client and listener settings")
	}
}

func TestValidateRejectsConflictingListeners(t *testing.T) {
	for _, test := range []struct {
		name           string
		health, metric string
	}{
		{name: "identical", health: ":8080", metric: ":8080"},
		{name: "wildcard and concrete", health: ":8080", metric: "127.0.0.1:8080"},
	} {
		t.Run(test.name, func(t *testing.T) {
			options := NewServerOption()
			options.KubeClientOptions.QPS = 1
			options.KubeClientOptions.Burst = 1
			options.EnableHealthz = true
			options.EnableMetrics = true
			options.HealthzAddress = test.health
			options.ListenAddress = test.metric
			if err := options.Validate(); err == nil {
				t.Fatal("Validate() accepted conflicting health and metrics listeners")
			}
		})
	}
}
