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

package util

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// DecodeDeviceTopologyAnnotation decodes the volcano.sh/device-topology
// annotation of a normal Pod. It returns nil for an empty value. The value must
// be exactly one DeviceTopologySpec with no unknown fields. The policy rules,
// such as the allowed modes, are checked by admission and by the scheduler.
func DecodeDeviceTopologyAnnotation(rawPolicy string) (*schedulingv1beta1.DeviceTopologySpec, error) {
	if rawPolicy == "" {
		return nil, nil
	}

	decoder := json.NewDecoder(strings.NewReader(rawPolicy))
	decoder.DisallowUnknownFields()
	policy := &schedulingv1beta1.DeviceTopologySpec{}
	if err := decoder.Decode(policy); err != nil {
		return nil, fmt.Errorf("must contain a valid device topology policy: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return nil, errors.New("must contain exactly one device topology policy")
		}
		return nil, fmt.Errorf("must contain exactly one device topology policy: %w", err)
	}

	return policy, nil
}
