/*
Copyright 2019 The Kubernetes Authors.

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
	"time"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/component-base/config"
	componentbaseoptions "k8s.io/component-base/config/options"
	"k8s.io/component-base/featuregate"

	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/kube"
	commonutil "volcano.sh/volcano/pkg/util"
)

func TestAddFlags(t *testing.T) {
	fs := pflag.NewFlagSet("addflagstest", pflag.ExitOnError)
	s := NewServerOption()
	commonutil.LeaderElectionDefault(&s.LeaderElection)
	componentbaseoptions.BindLeaderElectionFlags(&s.LeaderElection, fs)
	s.AddFlags(fs)
	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	args := []string{
		"--schedule-period=5m",
		"--resync-period=0",
		"--priority-class=false",
		"--cache-dumper=false",
		"--leader-elect-lease-duration=60s",
		"--leader-elect-renew-deadline=20s",
		"--leader-elect-retry-period=10s",
		"--feature-gates=PodDisruptionBudgetsSupport=false,VolcanoJobSupport=true",
	}
	fs.Parse(args)

	// This is a snapshot of expected options parsed by args.
	expected := &ServerOption{
		SchedulerNames: []string{defaultSchedulerName},
		SchedulePeriod: 5 * time.Minute,
		ResyncPeriod:   0,
		LeaderElection: config.LeaderElectionConfiguration{
			LeaderElect:       true,
			LeaseDuration:     metav1.Duration{Duration: 60 * time.Second},
			RenewDeadline:     metav1.Duration{Duration: 20 * time.Second},
			RetryPeriod:       metav1.Duration{Duration: 10 * time.Second},
			ResourceLock:      resourcelock.LeasesResourceLock,
			ResourceNamespace: defaultLockObjectNamespace,
		},
		DefaultQueue:  defaultQueue,
		ListenAddress: defaultListenAddress,
		KubeClientOptions: kube.ClientOptions{
			Master:     "",
			KubeConfig: "",
			QPS:        defaultQPS,
			Burst:      defaultBurst,
		},
		PluginsDir:                 defaultPluginsDir,
		HealthzBindAddress:         ":11251",
		MinNodesToFind:             defaultMinNodesToFind,
		MinPercentageOfNodesToFind: defaultMinPercentageOfNodesToFind,
		PercentageOfNodesToFind:    defaultPercentageOfNodesToFind,
		NodeWorkerThreads:          defaultNodeWorkers,
		CacheDumpFileDir:           "/tmp",
	}
	expectedFeatureGates := map[featuregate.Feature]bool{
		features.PodDisruptionBudgetsSupport: false,
		features.VolcanoJobSupport:           true,
	}

	if !equality.Semantic.DeepEqual(expected, s) {
		t.Errorf("Got different run options than expected.\nGot: %+v\nExpected: %+v\n", s, expected)
	}
	for k, v := range expectedFeatureGates {
		assert.Equal(t, v, utilfeature.DefaultFeatureGate.Enabled(k))
	}
}
