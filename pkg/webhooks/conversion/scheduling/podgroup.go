/*
Copyright 2020 The Volcano Authors.

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

package scheduling

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog"

	schedulingv1alpha1 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha1"
	schedulingv1alpha2 "volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
	convrouter "volcano.sh/volcano/pkg/webhooks/conversion/router"
	"volcano.sh/volcano/pkg/webhooks/conversion/util"
)

func init() {
	convrouter.RegisterConversion(service)
}

var service = &convrouter.ConversionService{
	Path: "/conversion/scheduling",
	Func: convertPodGroupCRD,
	Names: []string{
		fmt.Sprintf("%s.%s", "podgroups", schedulingv1alpha1.GroupName),
		fmt.Sprintf("%s.%s", "podgroups", schedulingv1alpha2.GroupName),
		fmt.Sprintf("%s.%s", "queues", schedulingv1alpha1.GroupName),
		fmt.Sprintf("%s.%s", "queues", schedulingv1alpha2.GroupName),
	},

	Config: config,
}

var config = &convrouter.ConversionServiceConfig{}

func convertPodGroupCRD(Object *unstructured.Unstructured, toVersion string) (*unstructured.Unstructured, metav1.Status) {

	convertedObject := Object.DeepCopy()
	fromVersion := Object.GetAPIVersion()

	if toVersion == fromVersion {
		return nil, util.StatusErrorWithMessage("conversion from a version to itself should not call the webhook: %s", toVersion)
	}

	klog.Infof("CRD Conversion: %s --> %s", fromVersion, toVersion)

	return convertedObject, util.StatusSucceed()
}
