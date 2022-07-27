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
	"reflect"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"volcano.sh/volcano/pkg/kube"
)

func TestAddFlags(t *testing.T) {
	fs := pflag.NewFlagSet("addflagstest", pflag.ContinueOnError)
	s := NewServerOption()
	s.AddFlags(fs)

	args := []string{
		"--schedule-period=5m",
		"--priority-class=false",
	}
	fs.Parse(args)

	// This is a snapshot of expected options parsed by args.
	expected := &ServerOption{
		SchedulerNames: []string{defaultSchedulerName},
		SchedulePeriod: 5 * time.Minute,
		DefaultQueue:   defaultQueue,
		ListenAddress:  defaultListenAddress,
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
	}

	if !reflect.DeepEqual(expected, s) {
		t.Errorf("Got different run options than expected.\nGot: %+v\nExpected: %+v\n", s, expected)
	}
}
