/*
Copyright 2022 The Volcano Authors.

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

package jobtemplate

import "testing"

func TestGetConnectionOfJobAndJobTemplate(t *testing.T) {
	type args struct {
		namespace string
		name      string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "TestGetJobTemplateString",
			args: args{
				namespace: "default",
				name:      "flow",
			},
			want: "default.flow",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetTemplateString(tt.args.namespace, tt.args.name); got != tt.want {
				t.Errorf("GetJobTemplateString() = %v, want %v", got, tt.want)
			}
		})
	}
}
