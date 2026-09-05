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
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func TestPopulateResourceListV1(t *testing.T) {
	testCases := []struct {
		Name          string
		Spec          string
		ExpectedList  v1.ResourceList
		ExpectedError bool
	}{
		{
			Name:          "EmptySpec",
			Spec:          "",
			ExpectedList:  nil,
			ExpectedError: false,
		},
		{
			Name: "SingleResource",
			Spec: "cpu=1",
			ExpectedList: v1.ResourceList{
				v1.ResourceCPU: resource.MustParse("1"),
			},
			ExpectedError: false,
		},
		{
			Name: "MultipleResources",
			Spec: "cpu=1,memory=2Gi",
			ExpectedList: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("1"),
				v1.ResourceMemory: resource.MustParse("2Gi"),
			},
			ExpectedError: false,
		},
		{
			Name:          "MissingSeparator",
			Spec:          "cpu:1",
			ExpectedList:  nil,
			ExpectedError: true,
		},
		{
			Name:          "InvalidQuantityValue",
			Spec:          "cpu=abc",
			ExpectedList:  nil,
			ExpectedError: true,
		},
		{
			Name:          "ExtraEqualsSigns",
			Spec:          "cpu=1=2",
			ExpectedList:  nil,
			ExpectedError: true,
		},
	}

	for i, testcase := range testCases {
		result, err := PopulateResourceListV1(testcase.Spec)
		if testcase.ExpectedError {
			if err == nil {
				t.Errorf("case %d (%s): expected error, got nil", i, testcase.Name)
			}
		} else {
			if err != nil {
				t.Errorf("case %d (%s): expected no error, got %v", i, testcase.Name, err)
			}
			if !reflect.DeepEqual(result, testcase.ExpectedList) {
				t.Errorf("case %d (%s): expected: %v, got %v", i, testcase.Name, testcase.ExpectedList, result)
			}
		}
	}
}

func TestTranslateTimestampSince(t *testing.T) {
	zeroTime := metav1.Time{}
	if result := TranslateTimestampSince(zeroTime); result != "<unknown>" {
		t.Errorf("zero timestamp: expected \"<unknown>\", got %q", result)
	}


	pastTime := metav1.Time{Time: time.Now().Add(-5 * time.Hour)}
	result := TranslateTimestampSince(pastTime)
	if result == "" || result == "<unknown>" {
		t.Errorf("past timestamp: expected a valid duration string, got %q", result)
	}


	slightFuture := metav1.Time{Time: time.Now().Add(500 * time.Millisecond)}
	result = TranslateTimestampSince(slightFuture)
	if result == "<invalid>" {
		t.Errorf("slight future timestamp: expected \"0s\", got %q", result)
	}
}

func TestRedirectStdoutAndCaptureOutput(t *testing.T) {
	r, oldStdout := RedirectStdout()
	fmt.Fprint(os.Stdout, "hello volcano")
	output := CaptureOutput(r, oldStdout)
	if output != "hello volcano" {
		t.Errorf("simple capture: expected %q, got %q", "hello volcano", output)
	}


	r, oldStdout = RedirectStdout()
	output = CaptureOutput(r, oldStdout)
	if output != "" {
		t.Errorf("empty capture: expected empty string, got %q", output)
	}


	r, oldStdout = RedirectStdout()
	fmt.Fprintln(os.Stdout, "line1")
	fmt.Fprintln(os.Stdout, "line2")
	output = CaptureOutput(r, oldStdout)
	if output != "line1\nline2" {
		t.Errorf("multiline capture: expected %q, got %q", "line1\nline2", output)
	}
}
