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

package util

/*
Occurrences of vendor strings such as huawei.com and hami.io in code
or resource names are used only as chip/vendor prefix identifiers
and do not imply project-level coupling
*/
const (
	HAMiAnnotationsPrefix   = "hami.io"
	AssignedNodeAnnotations = "hami.io/vgpu-node"
	AssignedTimeAnnotations = "hami.io/vgpu-time"
	BindTimeAnnotations     = "hami.io/bind-time"
	DeviceBindPhase         = "hami.io/bind-phase"
)
