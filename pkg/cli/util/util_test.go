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

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestPopulateResourceListV1(t *testing.T) {
	testCases := []struct {
		Name      string
		Spec      string
		Expect    v1.ResourceList
		ExpectErr bool
	}{
		{
			Name:   "EmptySpec",
			Spec:   "",
			Expect: nil,
		},
		{
			Name: "SingleResource",
			Spec: "cpu=1",
			Expect: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("1"),
			},
		},
		{
			Name: "MultipleResources",
			Spec: "cpu=1,memory=1Gi",
			Expect: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		{
			Name:      "MissingEquals",
			Spec:      "cpu",
			ExpectErr: true,
		},
		{
			Name:      "TooManyEquals",
			Spec:      "cpu=1=2",
			ExpectErr: true,
		},
		{
			Name:      "InvalidQuantity",
			Spec:      "cpu=notaquantity",
			ExpectErr: true,
		},
	}

	for _, testcase := range testCases {
		t.Run(testcase.Name, func(t *testing.T) {
			got, err := PopulateResourceListV1(testcase.Spec)
			if testcase.ExpectErr {
				if err == nil {
					t.Fatalf("expected an error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(testcase.Expect) {
				t.Fatalf("expected %v, got %v", testcase.Expect, got)
			}
			for name, want := range testcase.Expect {
				gotQuantity, ok := got[name]
				if !ok || gotQuantity.Cmp(want) != 0 {
					t.Errorf("resource %s: expected %v, got %v", name, want, got[name])
				}
			}
		})
	}
}

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
