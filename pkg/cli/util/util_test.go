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
	"testing"
	"time"

	version "k8s.io/apimachinery/pkg/version"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/kubernetes"
	fake "k8s.io/client-go/kubernetes/fake"
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

func TestSupportsCustomResourceFieldSelectors(t *testing.T) {
	// mock kubernetes client
	fakeClient_v131 := fake.NewSimpleClientset()
	fakeClient_v131.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{
		Major: "1",
		Minor: "31",
	}

	fakeClient_v130 := fake.NewSimpleClientset()
	fakeClient_v130.Discovery().(*fakediscovery.FakeDiscovery).FakedServerVersion = &version.Info{
		Major: "1",
		Minor: "30",
	}

	testCases := []struct {
		Name        string
		clientset   kubernetes.Interface
		ExpectValue bool
	}{
		{
			Name:        "v1.31",
			clientset:   fakeClient_v131,
			ExpectValue: true,
		},
		{
			Name:        "v1.30",
			clientset:   fakeClient_v130,
			ExpectValue: false,
		},
	}

	for i, testcase := range testCases {
		answer, err := SupportsCustomResourceFieldSelectors(testcase.clientset)
		if err != nil {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, answer)
		}
		if answer != testcase.ExpectValue {
			t.Errorf("case %d (%s): expected: %v, got %v ", i, testcase.Name, testcase.ExpectValue, answer)
		}
	}
}
