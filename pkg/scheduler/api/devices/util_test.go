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

package devices

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheckUUID(t *testing.T) {
	GPUUseUUID := "hami.io/gpu-use-uuid"
	GPUNoUseUUID := "hami.io/gpu-no-use-uuid"
	tests := []struct {
		name  string
		annos map[string]string
		id    string
		want  bool
	}{
		{
			name:  "don't set GPUUseUUID and GPUNoUseUUID annotation",
			annos: make(map[string]string),
			id:    "abc",
			want:  true,
		},
		{
			name: "use set GPUUseUUID don't set GPUNoUseUUID annotation,device match",
			annos: map[string]string{
				GPUUseUUID: "abc,123",
			},
			id:   "abc",
			want: true,
		},
		{
			name: "use set GPUUseUUID don't set GPUNoUseUUID annotation,device don't match",
			annos: map[string]string{
				GPUUseUUID: "abc,123",
			},
			id:   "1abc",
			want: false,
		},
		{
			name: "use don't set GPUUseUUID set GPUNoUseUUID annotation,device match",
			annos: map[string]string{
				GPUNoUseUUID: "abc,123",
			},
			id:   "abc",
			want: false,
		},
		{
			name: "use don't set GPUUseUUID set GPUNoUseUUID annotation,device  don't match",
			annos: map[string]string{
				GPUNoUseUUID: "abc,123",
			},
			id:   "1abc",
			want: true,
		},
		{
			name: "both GPUUseUUID and GPUNoUseUUID set, device in use list but also in nouse list",
			annos: map[string]string{
				GPUUseUUID:   "abc,123",
				GPUNoUseUUID: "abc",
			},
			id:   "abc",
			want: false,
		},
		{
			name: "both GPUUseUUID and GPUNoUseUUID set, device in use list and not in nouse list",
			annos: map[string]string{
				GPUUseUUID:   "abc,123",
				GPUNoUseUUID: "456",
			},
			id:   "abc",
			want: true,
		},
		{
			name: "use list with spaces around uuids, device matches after trim",
			annos: map[string]string{
				GPUUseUUID: " abc , 123 ",
			},
			id:   "abc",
			want: true,
		},
		{
			name: "empty GPUUseUUID annotation should not filter out any device",
			annos: map[string]string{
				GPUUseUUID: "",
			},
			id:   "abc",
			want: true,
		},
		{
			name: "whitespace-only GPUUseUUID annotation should not filter out any device",
			annos: map[string]string{
				GPUUseUUID: "   ",
			},
			id:   "abc",
			want: true,
		},
		{
			name: "empty GPUNoUseUUID annotation should not exclude any device",
			annos: map[string]string{
				GPUNoUseUUID: "",
			},
			id:   "abc",
			want: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CheckUUID(test.annos, test.id, GPUUseUUID, GPUNoUseUUID, "NVIDIA")
			assert.Equal(t, test.want, got)
		})
	}
}
