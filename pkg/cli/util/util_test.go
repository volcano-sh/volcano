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

package util

import (
	"path/filepath"
	"testing"
	"time"
)

func TestJobUtil(t *testing.T) {
	testCases := []struct {
		Name        string
		Duration    time.Duration
		ExpectValue string
	}{
		{
			Name:        "InvalidTime",
			Duration:    -time.Minute,
			ExpectValue: "<invalid>",
		},
		{
			Name:        "SmallInvalieTime",
			Duration:    -time.Millisecond,
			ExpectValue: "0s",
		},
		{
			Name:        "NormalSeconds",
			Duration:    62 * time.Second,
			ExpectValue: "62s",
		},
		{
			Name:        "NormalMinutes",
			Duration:    180 * time.Second,
			ExpectValue: "3m",
		},
		{
			Name:        "NormalMinutesWithSecond",
			Duration:    190 * time.Second,
			ExpectValue: "3m10s",
		},
		{
			Name:        "BiggerMinutesWithoutSecond",
			Duration:    121*time.Minute + 56*time.Second,
			ExpectValue: "121m",
		},
		{
			Name:        "NormalHours",
			Duration:    5*time.Hour + 9*time.Second,
			ExpectValue: "5h",
		},
		{
			Name:        "NormalHoursWithMinute",
			Duration:    5*time.Hour + 7*time.Minute + 9*time.Second,
			ExpectValue: "5h7m",
		},
		{
			Name:        "BiggerHoursWithoutMinute",
			Duration:    12*time.Hour + 7*time.Minute + 9*time.Second,
			ExpectValue: "12h",
		},
		{
			Name:        "NormalDays",
			Duration:    5*24*time.Hour + 7*time.Minute + 9*time.Second,
			ExpectValue: "5d",
		},
		{
			Name:        "NormalDaysWithHours",
			Duration:    5*24*time.Hour + 9*time.Hour,
			ExpectValue: "5d9h",
		},
		{
			Name:        "BiggerDayWithoutHours",
			Duration:    531*24*time.Hour + 7*time.Minute + 9*time.Second,
			ExpectValue: "531d",
		},
		{
			Name:        "NormalYears",
			Duration:    (365*5+89)*24*time.Hour + 7*time.Minute + 9*time.Second,
			ExpectValue: "5y89d",
		},
		{
			Name:        "BiggerYears",
			Duration:    (365*12+15)*24*time.Hour + 7*time.Minute + 9*time.Second,
			ExpectValue: "12y",
		},
	}

	for i, testcase := range testCases {
		answer := HumanDuration(testcase.Duration)
		if answer != testcase.ExpectValue {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, answer)
		}
	}
}

func TestBuildConfigAndNamespace(t *testing.T) {
	const (
		contextServer = "https://context.example.com"
		masterServer  = "https://master.example.com"
	)
	withNamespace, err := CreateTestKubeconfig(t.TempDir(), contextServer, "volcano-system")
	if err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}
	withoutNamespace, err := CreateTestKubeconfig(t.TempDir(), contextServer, "")
	if err != nil {
		t.Fatalf("failed to write kubeconfig: %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing")

	testCases := []struct {
		name          string
		envKubeconfig string
		master        string
		kubeconfig    string
		namespace     string
		wantErr       bool
		wantHost      string
		wantNamespace string
	}{
		{
			name:          "namespace of the current context",
			kubeconfig:    withNamespace,
			wantHost:      contextServer,
			wantNamespace: "volcano-system",
		},
		{
			name:          "namespace flag overrides the context",
			kubeconfig:    withNamespace,
			namespace:     "flag-namespace",
			wantHost:      contextServer,
			wantNamespace: "flag-namespace",
		},
		{
			name:          "context without a namespace",
			kubeconfig:    withoutNamespace,
			wantHost:      contextServer,
			wantNamespace: "default",
		},
		{
			name:          "kubeconfig from the KUBECONFIG environment variable",
			envKubeconfig: withNamespace,
			wantHost:      contextServer,
			wantNamespace: "volcano-system",
		},
		{
			name:          "kubeconfig flag takes precedence over KUBECONFIG",
			envKubeconfig: withNamespace,
			kubeconfig:    withoutNamespace,
			wantHost:      contextServer,
			wantNamespace: "default",
		},
		{
			name:          "master flag keeps the context namespace",
			kubeconfig:    withNamespace,
			master:        masterServer,
			wantHost:      masterServer,
			wantNamespace: "volcano-system",
		},
		{
			name:          "master flag without a kubeconfig",
			master:        masterServer,
			wantHost:      masterServer,
			wantNamespace: "default",
		},
		{
			name:       "missing kubeconfig file",
			kubeconfig: missing,
			wantErr:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			envKubeconfig := tc.envKubeconfig
			if envKubeconfig == "" {
				// Keep the test away from the kubeconfig of the machine it runs on.
				envKubeconfig = missing
			}
			t.Setenv("KUBECONFIG", envKubeconfig)
			// Make sure the in-cluster namespace is never used.
			t.Setenv("KUBERNETES_SERVICE_HOST", "")

			config, namespace, err := BuildConfigAndNamespace(tc.master, tc.kubeconfig, tc.namespace)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got namespace %q", namespace)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if config.Host != tc.wantHost {
				t.Errorf("host: got %q, want %q", config.Host, tc.wantHost)
			}
			if namespace != tc.wantNamespace {
				t.Errorf("namespace: got %q, want %q", namespace, tc.wantNamespace)
			}
		})
	}
}
