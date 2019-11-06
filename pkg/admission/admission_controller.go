/*
Copyright 2018 The Volcano Authors.

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

	"github.com/golang/glog"
	"github.com/hashicorp/go-multierror"

	"k8s.io/api/admission/v1beta1"
	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"k8s.io/kubernetes/pkg/apis/core/validation"
	batchv1alpha1 "volcano.sh/volcano/pkg/apis/batch/v1alpha1"
	vcclientset "volcano.sh/volcano/pkg/client/clientset/versioned"
)

const (
	// AdmitJobPath is the pattern for the jobs admission
	AdmitJobPath = "/jobs"
	// MutateJobPath is the pattern for the mutating jobs
	MutateJobPath = "/mutating-jobs"
	// AdmitPodPath is the pattern for the pods admission
	AdmitPodPath = "/pods"
	// CONTENTTYPE http content-type
	CONTENTTYPE = "Content-Type"
	// APPLICATIONJSON json content
	APPLICATIONJSON = "application/json"
)

//The AdmitFunc returns response
type AdmitFunc func(v1beta1.AdmissionReview) *v1beta1.AdmissionResponse

// Controller the Admission Controller type
type Controller struct {
	VcClients     vcclientset.Interface
	SchedulerName string
}

var scheme = runtime.NewScheme()

//Codecs is for retrieving serializers for the supported wire formats
//and conversion wrappers to define preferred internal and external versions.
var Codecs = serializer.NewCodecFactory(scheme)

// policyEventMap defines all policy events and whether to allow external use
var policyEventMap = map[batchv1alpha1.Event]bool{
	batchv1alpha1.AnyEvent:           true,
	batchv1alpha1.PodFailedEvent:     true,
	batchv1alpha1.PodEvictedEvent:    true,
	batchv1alpha1.JobUnknownEvent:    true,
	batchv1alpha1.TaskCompletedEvent: true,
	batchv1alpha1.OutOfSyncEvent:     false,
	batchv1alpha1.CommandIssuedEvent: false,
}

// policyActionMap defines all policy actions and whether to allow external use
var policyActionMap = map[batchv1alpha1.Action]bool{
	batchv1alpha1.AbortJobAction:     true,
	batchv1alpha1.RestartJobAction:   true,
	batchv1alpha1.RestartTaskAction:  true,
	batchv1alpha1.TerminateJobAction: true,
	batchv1alpha1.CompleteJobAction:  true,
	batchv1alpha1.ResumeJobAction:    true,
	batchv1alpha1.SyncJobAction:      false,
	batchv1alpha1.EnqueueAction:      false,
}

func init() {
	addToScheme(scheme)
}

func addToScheme(scheme *runtime.Scheme) {
	corev1.AddToScheme(scheme)
	admissionregistrationv1beta1.AddToScheme(scheme)
}

//ToAdmissionResponse updates the admission response with the input error
func ToAdmissionResponse(err error) *v1beta1.AdmissionResponse {
	glog.Error(err)
	return &v1beta1.AdmissionResponse{
		Result: &metav1.Status{
			Message: err.Error(),
		},
	}
}

//DecodeJob decodes the job using deserializer from the raw object
func DecodeJob(object runtime.RawExtension, resource metav1.GroupVersionResource) (batchv1alpha1.Job, error) {
	jobResource := metav1.GroupVersionResource{Group: batchv1alpha1.SchemeGroupVersion.Group, Version: batchv1alpha1.SchemeGroupVersion.Version, Resource: "jobs"}
	raw := object.Raw
	job := batchv1alpha1.Job{}

	if resource != jobResource {
		err := fmt.Errorf("expect resource to be %s", jobResource)
		return job, err
	}

	deserializer := Codecs.UniversalDeserializer()
	if _, _, err := deserializer.Decode(raw, nil, &job); err != nil {
		return job, err
	}
	glog.V(3).Infof("the job struct is %+v", job)

	return job, nil
}

func validatePolicies(policies []batchv1alpha1.LifecyclePolicy, fldPath *field.Path) error {
	var err error
	policyEvents := map[batchv1alpha1.Event]struct{}{}
	exitCodes := map[int32]struct{}{}

	for _, policy := range policies {
		if (policy.Event != "" || len(policy.Events) != 0) && policy.ExitCode != nil {
			err = multierror.Append(err, fmt.Errorf("must not specify event and exitCode simultaneously"))
			break
		}

		if policy.Event == "" && len(policy.Events) == 0 && policy.ExitCode == nil {
			err = multierror.Append(err, fmt.Errorf("either event and exitCode should be specified"))
			break
		}

		if len(policy.Event) != 0 || len(policy.Events) != 0 {
			bFlag := false
			policyEventsList := getEventList(policy)
			for _, event := range policyEventsList {
				if allow, ok := policyEventMap[event]; !ok || !allow {
					err = multierror.Append(err, field.Invalid(fldPath, event, fmt.Sprintf("invalid policy event")))
					bFlag = true
					break
				}

				if allow, ok := policyActionMap[policy.Action]; !ok || !allow {
					err = multierror.Append(err, field.Invalid(fldPath, policy.Action, fmt.Sprintf("invalid policy action")))
					bFlag = true
					break
				}
				if _, found := policyEvents[event]; found {
					err = multierror.Append(err, fmt.Errorf("duplicate event %v  across different policy", event))
					bFlag = true
					break
				} else {
					policyEvents[event] = struct{}{}
				}
			}
			if bFlag == true {
				break
			}

		} else {
			if *policy.ExitCode == 0 {
				err = multierror.Append(err, fmt.Errorf("0 is not a valid error code"))
				break
			}
			if _, found := exitCodes[*policy.ExitCode]; found {
				err = multierror.Append(err, fmt.Errorf("duplicate exitCode %v", *policy.ExitCode))
				break
			} else {
				exitCodes[*policy.ExitCode] = struct{}{}
			}
		}
	}

	if _, found := policyEvents[batchv1alpha1.AnyEvent]; found && len(policyEvents) > 1 {
		err = multierror.Append(err, fmt.Errorf("if there's * here, no other policy should be here"))
	}

	return err
}

func getEventList(policy batchv1alpha1.LifecyclePolicy) []batchv1alpha1.Event {
	policyEventsList := policy.Events
	if len(policy.Event) > 0 {
		policyEventsList = append(policyEventsList, policy.Event)
	}
	uniquePolicyEventlist := removeDuplicates(policyEventsList)
	return uniquePolicyEventlist
}

func removeDuplicates(EventList []batchv1alpha1.Event) []batchv1alpha1.Event {
	keys := make(map[batchv1alpha1.Event]bool)
	list := []batchv1alpha1.Event{}
	for _, val := range EventList {
		if _, value := keys[val]; !value {
			keys[val] = true
			list = append(list, val)
		}
	}
	return list
}

func getValidEvents() []batchv1alpha1.Event {
	var events []batchv1alpha1.Event
	for e, allow := range policyEventMap {
		if allow {
			events = append(events, e)
		}
	}

	return events
}

func getValidActions() []batchv1alpha1.Action {
	var actions []batchv1alpha1.Action
	for a, allow := range policyActionMap {
		if allow {
			actions = append(actions, a)
		}
	}

	return actions
}

// ValidateIO validate IO configuration
func ValidateIO(volumes []batchv1alpha1.VolumeSpec) (string, bool) {
	volumeMap := map[string]bool{}
	for _, volume := range volumes {
		if len(volume.MountPath) == 0 {
			return " mountPath is required;", true
		}
		if _, found := volumeMap[volume.MountPath]; found {
			return fmt.Sprintf(" duplicated mountPath: %s;", volume.MountPath), true
		}
		if len(volume.VolumeClaimName) != 0 {
			if volume.VolumeClaim != nil {
				return fmt.Sprintf("Confilct: If you want to use an existing PVC, just specify VolumeClaimName. If you want to create a new PVC, you do not need to specify VolumeClaimName."), true
			}
			if errMsgs := validation.ValidatePersistentVolumeName(volume.VolumeClaimName, false); len(errMsgs) > 0 {
				return fmt.Sprintf("Illegal VolumeClaimName %s : %v", volume.VolumeClaimName, errMsgs), true
			}
		}
		volumeMap[volume.MountPath] = true
	}
	return "", false
}
