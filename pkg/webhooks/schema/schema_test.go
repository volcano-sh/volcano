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

package schema

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	batchv1alpha1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	flowv1alpha1 "volcano.sh/apis/pkg/apis/flow/v1alpha1"
	schedulingv1beta1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	hypernodev1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
)

const decodedName = "decoded-object"

// namedObject is implemented by every object the decoders return.
type namedObject interface {
	GetName() string
}

// rawObject returns the JSON of an object of the given API version and kind,
// named decodedName.
func rawObject(apiVersion, kind string) string {
	return fmt.Sprintf(`{"apiVersion":%q,"kind":%q,"metadata":{"name":%q}}`, apiVersion, kind, decodedName)
}

// testDecode checks that decode returns the object of the expected resource,
// and returns an error for another resource or for malformed data.
func testDecode[T namedObject](t *testing.T, decode func(runtime.RawExtension, metav1.GroupVersionResource) (T, error),
	resource metav1.GroupVersionResource, raw string) {
	t.Helper()

	otherResource := resource
	otherResource.Resource = "other" + resource.Resource

	tests := []struct {
		name     string
		raw      string
		resource metav1.GroupVersionResource
		wantErr  string
	}{
		{
			name:     "decodes the object of the expected resource",
			raw:      raw,
			resource: resource,
		},
		{
			name:     "rejects another resource",
			raw:      raw,
			resource: otherResource,
			wantErr:  "expect resource to be",
		},
		{
			name:     "returns an error for malformed data",
			raw:      "{",
			resource: resource,
			wantErr:  "unexpected end of JSON input",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decode(runtime.RawExtension{Raw: []byte(tt.raw)}, tt.resource)
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, decodedName, got.GetName())
		})
	}
}

func TestDecodeJob(t *testing.T) {
	testDecode(t, DecodeJob,
		metav1.GroupVersionResource{Group: batchv1alpha1.SchemeGroupVersion.Group, Version: batchv1alpha1.SchemeGroupVersion.Version, Resource: "jobs"},
		rawObject(batchv1alpha1.SchemeGroupVersion.String(), "Job"))
}

func TestDecodeCronJob(t *testing.T) {
	testDecode(t, DecodeCronJob,
		metav1.GroupVersionResource{Group: batchv1alpha1.SchemeGroupVersion.Group, Version: batchv1alpha1.SchemeGroupVersion.Version, Resource: "cronjobs"},
		rawObject(batchv1alpha1.SchemeGroupVersion.String(), "CronJob"))
}

func TestDecodePod(t *testing.T) {
	testDecode(t, DecodePod,
		metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"},
		rawObject("v1", "Pod"))
}

func TestDecodeQueue(t *testing.T) {
	testDecode(t, DecodeQueue,
		metav1.GroupVersionResource{Group: schedulingv1beta1.SchemeGroupVersion.Group, Version: schedulingv1beta1.SchemeGroupVersion.Version, Resource: "queues"},
		rawObject(schedulingv1beta1.SchemeGroupVersion.String(), "Queue"))
}

func TestDecodeNamespaceQueue(t *testing.T) {
	testDecode(t, DecodeNamespaceQueue,
		metav1.GroupVersionResource{Group: schedulingv1beta1.SchemeGroupVersion.Group, Version: schedulingv1beta1.SchemeGroupVersion.Version, Resource: "namespacequeues"},
		rawObject(schedulingv1beta1.SchemeGroupVersion.String(), "NamespaceQueue"))
}

func TestDecodePodGroup(t *testing.T) {
	testDecode(t, DecodePodGroup,
		metav1.GroupVersionResource{Group: schedulingv1beta1.SchemeGroupVersion.Group, Version: schedulingv1beta1.SchemeGroupVersion.Version, Resource: "podgroups"},
		rawObject(schedulingv1beta1.SchemeGroupVersion.String(), "PodGroup"))
}

func TestDecodeHyperNode(t *testing.T) {
	testDecode(t, DecodeHyperNode,
		metav1.GroupVersionResource{Group: hypernodev1alpha1.SchemeGroupVersion.Group, Version: hypernodev1alpha1.SchemeGroupVersion.Version, Resource: "hypernodes"},
		rawObject(hypernodev1alpha1.SchemeGroupVersion.String(), "HyperNode"))
}

func TestDecodeJobFlow(t *testing.T) {
	testDecode(t, DecodeJobFlow,
		metav1.GroupVersionResource{Group: flowv1alpha1.SchemeGroupVersion.Group, Version: flowv1alpha1.SchemeGroupVersion.Version, Resource: "jobflows"},
		rawObject(flowv1alpha1.SchemeGroupVersion.String(), "JobFlow"))
}
