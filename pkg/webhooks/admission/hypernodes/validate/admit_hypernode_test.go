/*
Copyright 2024 The Volcano Authors.

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

package validate

import (
	"testing"

	hypernodev1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

func TestValidateHyperNodeMembers(t *testing.T) {
	testCases := []struct {
		Name      string
		HyperNode *hypernodev1alpha1.HyperNode
		ExpectErr string
	}{
		{
			Name: "validate valid hypernode",
			HyperNode: &hypernodev1alpha1.HyperNode{
				Spec: hypernodev1alpha1.HyperNodeSpec{
					Members: []hypernodev1alpha1.MemberSpec{
						{
							Selector: hypernodev1alpha1.MemberSelector{
								ExactMatch: &hypernodev1alpha1.ExactMatch{
									Name: "node-1",
								},
							},
						},
					},
				},
			},
			ExpectErr: "",
		},
		{
			Name: "validate invalid hypernode with empty selector",
			HyperNode: &hypernodev1alpha1.HyperNode{
				Spec: hypernodev1alpha1.HyperNodeSpec{
					Members: []hypernodev1alpha1.MemberSpec{
						{
							Selector: hypernodev1alpha1.MemberSelector{},
						},
					},
				},
			},
			ExpectErr: "either exactMatch or regexMatch must be specified",
		},
		{
			Name: "validate invalid hypernode with both exactMatch and regexMatch",
			HyperNode: &hypernodev1alpha1.HyperNode{
				Spec: hypernodev1alpha1.HyperNodeSpec{
					Members: []hypernodev1alpha1.MemberSpec{
						{
							Selector: hypernodev1alpha1.MemberSelector{
								ExactMatch: &hypernodev1alpha1.ExactMatch{
									Name: "node-1",
								},
								RegexMatch: &hypernodev1alpha1.RegexMatch{
									Pattern: "node-1",
								},
							},
						},
					},
				},
			},
			ExpectErr: "exactMatch and regexMatch cannot be specified together",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			err := validateHyperNode(testCase.HyperNode)
			if err != nil && err.Error() != testCase.ExpectErr {
				t.Errorf("validateHyperNodeLabels failed: %v", err)
			}
		})
	}
}
