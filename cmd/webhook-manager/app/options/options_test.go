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
	"testing"

	"github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/api/equality"
	utilfeature "k8s.io/apiserver/pkg/util/feature"

	"volcano.sh/volcano/pkg/kube"
)

func TestAddFlags(t *testing.T) {
	fs := pflag.NewFlagSet("addflagstest", pflag.ExitOnError)
	s := NewConfig()
	s.AddFlags(fs)
	utilfeature.DefaultMutableFeatureGate.AddFlag(fs)

	args := []string{
		"--master=127.0.0.1",
		"--kube-api-burst=200",
	}
	fs.Parse(args)

	// This is a snapshot of expected options parsed by args.
	expected := &Config{
		KubeClientOptions: kube.ClientOptions{
			Master:     "127.0.0.1",
			KubeConfig: "",
			QPS:        defaultQPS,
			Burst:      200,
		},
		ListenAddress:        "",
		Port:                 8443,
		PrintVersion:         false,
		WebhookName:          "",
		WebhookNamespace:     "",
		SchedulerNames:       []string{defaultSchedulerName},
		WebhookURL:           "",
		ConfigPath:           "",
		EnabledAdmission:     defaultEnabledAdmission,
		GracefulShutdownTime: defaultGracefulShutdownTime,
		EnableHealthz:        false,
		HealthzBindAddress:   defaultHealthzAddress,
	}

	if !equality.Semantic.DeepEqual(expected, s) {
		t.Errorf("Got different run options than expected.\nGot: %+v\nExpected: %+v\n", s, expected)
	}
}
