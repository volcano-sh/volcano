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

package devices

// DeviceReservation is the common return contract for Devices.Allocate/Release.
// Device implementations only fill the fields they need; predicates consumes the
// result and forwards annotation data into the bind pipeline.
type DeviceReservation struct {
	// DeviceType identifies the device implementation that produced the reservation.
	DeviceType string

	// Annotations contains Pod annotation key/value pairs for this reservation.
	// Allocate callers merge these values into TaskInfo.PodAnnotations for bind carry.
	// Release callers only use the keys to delete corresponding bind annotations.
	Annotations map[string]string

	// Opaque is reserved for device-specific data and is not interpreted by the framework.
	Opaque map[string]string
}

func (r *DeviceReservation) AnnotationKeys() []string {
	if r == nil || len(r.Annotations) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.Annotations))
	for key := range r.Annotations {
		keys = append(keys, key)
	}
	return keys
}

// AnnotationKeyMap builds an annotation map that carries only keys.
// Release implementations use it to tell the scheduler which keys must be
// removed from TaskInfo.PodAnnotations before the bind request is submitted.
func AnnotationKeyMap(keys []string) map[string]string {
	if len(keys) == 0 {
		return nil
	}
	annotations := make(map[string]string, len(keys))
	for _, key := range keys {
		annotations[key] = ""
	}
	return annotations
}
