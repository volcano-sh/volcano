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

package admission

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/golang/glog"

	"k8s.io/api/admission/v1beta1"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"volcano.sh/volcano/pkg/apis/helpers"
	"volcano.sh/volcano/pkg/apis/scheduling/v1alpha2"
)

// ServerPods is to server pods
func (c *Controller) ServerPods(w http.ResponseWriter, r *http.Request) {
	Serve(w, r, c.AdmitPods)
}

// AdmitPods is to admit pods and return response
func (c *Controller) AdmitPods(ar v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {

	glog.V(3).Infof("admitting pods -- %s", ar.Request.Operation)

	pod, err := decodePod(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return ToAdmissionResponse(err)
	}

	var msg string
	reviewResponse := v1beta1.AdmissionResponse{}
	reviewResponse.Allowed = true

	switch ar.Request.Operation {
	case v1beta1.Create:
		msg = c.validatePod(pod, &reviewResponse)
		break
	default:
		err := fmt.Errorf("expect operation to be 'CREATE'")
		return ToAdmissionResponse(err)
	}

	if !reviewResponse.Allowed {
		reviewResponse.Result = &metav1.Status{Message: strings.TrimSpace(msg)}
	}
	return &reviewResponse
}

func decodePod(object runtime.RawExtension, resource metav1.GroupVersionResource) (v1.Pod, error) {
	podResource := metav1.GroupVersionResource{Group: "", Version: "v1", Resource: "pods"}
	raw := object.Raw
	pod := v1.Pod{}

	if resource != podResource {
		err := fmt.Errorf("expect resource to be %s", podResource)
		return pod, err
	}

	deserializer := Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &pod); err != nil {
		return pod, err
	}
	glog.V(3).Infof("the pod struct is %+v", pod)

	return pod, nil
}

// allow pods to create when
// 1. schedulerName of pod isn't volcano
// 2. pod has Podgroup whose phase isn't Pending
// 3. normal pods whose schedulerName is volcano don't have podgroup
func (c *Controller) validatePod(pod v1.Pod, reviewResponse *v1beta1.AdmissionResponse) string {
	if pod.Spec.SchedulerName != c.SchedulerName {
		return ""
	}

	pgName := ""
	msg := ""

	// vc-job, SN == volcano
	if pod.Annotations != nil {
		pgName = pod.Annotations[v1alpha2.GroupNameAnnotationKey]
	}
	if pgName != "" {
		if err := c.checkPGPhase(pod, pgName, true); err != nil {
			msg = err.Error()
			reviewResponse.Allowed = false
		}
		return msg
	}

	// normal pod, SN == volcano
	pgName = helpers.GeneratePodgroupName(&pod)
	if err := c.checkPGPhase(pod, pgName, false); err != nil {
		msg = err.Error()
		reviewResponse.Allowed = false
	}

	return msg
}

func (c *Controller) checkPGPhase(pod v1.Pod, pgName string, isVCJob bool) error {
	pg, err := c.VcClients.SchedulingV1alpha2().PodGroups(pod.Namespace).Get(pgName, metav1.GetOptions{})
	if err != nil {
		if isVCJob || (!isVCJob && !apierrors.IsNotFound(err)) {
			return fmt.Errorf("Failed to get PodGroup for pod <%s/%s>: %v", pod.Namespace, pod.Name, err)
		}
		return nil
	}
	if pg.Status.Phase != v1alpha2.PodGroupPending {
		return nil
	}
	return fmt.Errorf("Failed to create pod <%s/%s>, because the podgroup phase is Pending",
		pod.Namespace, pod.Name)
}
