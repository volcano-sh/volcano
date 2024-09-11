/*
Copyright 2019 The Volcano Authors.

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
	"sort"
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

	"volcano.sh/volcano/pkg/controllers/framework"
	_ "volcano.sh/volcano/pkg/controllers/garbagecollector"
	_ "volcano.sh/volcano/pkg/controllers/job"
	_ "volcano.sh/volcano/pkg/controllers/jobflow"
	_ "volcano.sh/volcano/pkg/controllers/jobtemplate"
	_ "volcano.sh/volcano/pkg/controllers/podgroup"
	_ "volcano.sh/volcano/pkg/controllers/queue"
	"volcano.sh/volcano/pkg/features"
	"volcano.sh/volcano/pkg/kube"
	commonutil "volcano.sh/volcano/pkg/util"
)

func TestAddFlags(t *testing.T) {
	fs := pflag.NewFlagSet("addflagstest", pflag.ExitOnError)
	s := NewServerOption()

	commonutil.LeaderElectionDefault(&s.LeaderElection)
	s.LeaderElection.ResourceName = "vc-controller-manager"
	componentbaseoptions.BindLeaderElectionFlags(&s.LeaderElection, fs)
	// knownControllers is a list of all known controllers.
	var knownControllers = func() []string {
		controllerNames := []string{}
		fn := func(controller framework.Controller) {
			controllerNames = append(controllerNames, controller.Name())
		}
		framework.ForeachController(fn)
		sort.Strings(controllerNames)
		return controllerNames
	}
	s.AddFlags(fs, knownControllers())
	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	args := []string{
		"--master=127.0.0.1",
		"--kube-api-burst=200",
		"--scheduler-name=volcano",
		"--scheduler-name=volcano2",
		"--leader-elect-lease-duration=60s",
		"--leader-elect-renew-deadline=20s",
		"--leader-elect-retry-period=10s",
		"--feature-gates=ResourceTopology=false",
	}
	fs.Parse(args)

	// This is a snapshot of expected options parsed by args.
	expected := &ServerOption{
		KubeClientOptions: kube.ClientOptions{
			Master:     "127.0.0.1",
			KubeConfig: "",
			QPS:        defaultQPS,
			Burst:      200,
		},
		PrintVersion:            false,
		WorkerThreads:           defaultWorkers,
		SchedulerNames:          []string{"volcano", "volcano2"},
		MaxRequeueNum:           defaultMaxRequeueNum,
		HealthzBindAddress:      ":11251",
		InheritOwnerAnnotations: true,
		LeaderElection: config.LeaderElectionConfiguration{
			LeaderElect:       true,
			LeaseDuration:     metav1.Duration{60 * time.Second},
			RenewDeadline:     metav1.Duration{20 * time.Second},
			RetryPeriod:       metav1.Duration{10 * time.Second},
			ResourceLock:      resourcelock.LeasesResourceLock,
			ResourceNamespace: defaultLockObjectNamespace,
			ResourceName:      "vc-controller-manager",
		},
		LockObjectNamespace: defaultLockObjectNamespace,
		WorkerThreadsForPG:  5,
		WorkerThreadsForGC:  1,
		Controllers:         []string{"*"},
	}
	expectedFeatureGates := map[featuregate.Feature]bool{features.ResourceTopology: false}

	if !equality.Semantic.DeepEqual(expected, s) {
		t.Errorf("Got different run options than expected.\nGot: %+v\nExpected: %+v\n", s, expected)
	}

	for k, v := range expectedFeatureGates {
		assert.Equal(t, v, utilfeature.DefaultFeatureGate.Enabled(k))
	}

	err := s.CheckOptionOrDie()
	if err != nil {
		t.Errorf("expected nil but got %v\n", err)
	}
}

func TestCheckControllers(t *testing.T) {
	testCases := []struct {
		name         string
		serverOption *ServerOption
		expectErr    error
	}{
		{
			name: "normal case: use *",
			serverOption: &ServerOption{
				Controllers: []string{"*"},
			},
			expectErr: nil,
		},
		{
			name: "normal case: use specific controller",
			serverOption: &ServerOption{
				Controllers: []string{"+gc-controller", "+jobtemplate-controller", "+jobflow-controller"},
			},
			expectErr: nil,
		},
		{
			name: "fail case: use duplicate job-controller",
			serverOption: &ServerOption{
				Controllers: []string{"+gc-controller", "+job-controller", "-job-controller"},
			},
			expectErr: fmt.Errorf("controllers option %s cannot have both '-' and '+' prefixes", "-job-controller"),
		},
		{
			name: "fail case: use * but combined with other input",
			serverOption: &ServerOption{
				Controllers: []string{"*", "+job-controller"},
			},
			expectErr: fmt.Errorf("wildcard '*' cannot be combined with other input"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.serverOption.checkControllers()
			if err != nil {
				if err.Error() != tc.expectErr.Error() {
					t.Errorf("test case %s failed: expected: %v, but got: %v", tc.name, tc.expectErr, err)
				}
			}
		})
	}
}
