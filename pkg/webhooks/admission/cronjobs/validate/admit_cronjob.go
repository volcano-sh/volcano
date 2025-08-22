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

package validate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
	admissionv1 "k8s.io/api/admission/v1"
	whv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/capabilities"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"volcano.sh/volcano/pkg/webhooks/router"
	"volcano.sh/volcano/pkg/webhooks/schema"
	"volcano.sh/volcano/pkg/webhooks/util"
)

func init() {
	capabilities.Initialize(capabilities.Capabilities{
		AllowPrivileged: true,
		PrivilegedSources: capabilities.PrivilegedSources{
			HostNetworkSources: []string{},
			HostPIDSources:     []string{},
			HostIPCSources:     []string{},
		},
	})
	router.RegisterAdmission(service)
}

var service = &router.AdmissionService{
	Path: "/cronjobs/validate",
	Func: AdmitCronjobs,

	Config: config,

	ValidatingConfig: &whv1.ValidatingWebhookConfiguration{
		Webhooks: []whv1.ValidatingWebhook{{
			Name: "validatecronjob.volcano.sh",
			Rules: []whv1.RuleWithOperations{
				{
					Operations: []whv1.OperationType{whv1.Create, whv1.Update},
					Rule: whv1.Rule{
						APIGroups:   []string{"batch.volcano.sh"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"cronjobs"},
					},
				},
			},
		}},
	},
}
var config = &router.AdmissionServiceConfig{}

func AdmitCronjobs(ar admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	klog.V(3).Infof("admitting cronjobs -- %s", ar.Request.Operation)

	cronjob, err := schema.DecodeCronJob(ar.Request.Object, ar.Request.Resource)
	if err != nil {
		return util.ToAdmissionResponse(err)
	}
	var msg string
	reviewResponse := admissionv1.AdmissionResponse{}
	reviewResponse.Allowed = true

	switch ar.Request.Operation {
	case admissionv1.Create:
		msg = validateCronJobCreate(cronjob, &reviewResponse)
	case admissionv1.Update:
		err = validateCronJobUpdate(cronjob)
		if err != nil {
			return util.ToAdmissionResponse(err)
		}
	default:
		err := fmt.Errorf("expect operation to be 'CREATE' or 'UPDATE'")
		return util.ToAdmissionResponse(err)
	}

	if !reviewResponse.Allowed {
		reviewResponse.Result = &metav1.Status{Message: strings.TrimSpace(msg)}
	}
	return &reviewResponse
}
func validateCronJobCreate(cronjob *v1alpha1.CronJob, reviewResponse *admissionv1.AdmissionResponse) string {
	msg := validateCronjobSpec(&cronjob.Spec, cronjob.Namespace)
	msg += validateCronJobName(cronjob.Name)
	if msg != "" {
		reviewResponse.Allowed = false
	}
	return msg
}
func validateCronJobUpdate(new *v1alpha1.CronJob) error {
	msg := validateCronjobSpec(&new.Spec, new.Namespace)
	msg += validateCronJobName(new.Name)

	if msg != "" {
		return errors.New(msg)
	}
	return nil
}
func validateCronjobSpec(spec *v1alpha1.CronJobSpec, nameSpace string) string {
	var msg string
	if len(spec.Schedule) == 0 {
		msg += " schedule is required, but got empty"
	} else {
		if strings.Contains(spec.Schedule, "TZ") {
			msg += " schedule should not contain TZ or CRON_TZ, TZ should only be set in the timeZone field"
		} else {
			if _, err := cron.ParseStandard(spec.Schedule); err != nil {
				msg += "schedule is not a valid cron expression"
			}
		}
	}

	var validTimeZoneCharacters = regexp.MustCompile(`^[A-Za-z\.\-_0-9+]{1,14}$`)
	if spec.TimeZone != nil {
		if len(*spec.TimeZone) == 0 {
			msg += " timeZone must be nil or non-empty string, got empty string"
		} else {
			for _, part := range strings.Split(*spec.TimeZone, "/") {
				if part == "." || part == ".." || strings.HasPrefix(part, "-") || !validTimeZoneCharacters.MatchString(part) {
					msg += " unknown timeZone"
				}
			}
			if strings.EqualFold(*spec.TimeZone, "Local") {
				msg += " timeZone can't be Local, and must be defined in https://www.iana.org/time-zones"
			} else {
				if _, err := time.LoadLocation(*spec.TimeZone); err != nil {
					msg += " invalid timeZone"
				}
			}
		}
	}
	return msg
}
func validateCronJobName(name string) string {
	const (
		maxTotalLength    = 63
		maxSuffixLength   = 11
		maxBaseNameLength = maxTotalLength - maxSuffixLength
	)

	if len(name) > maxBaseNameLength {
		return fmt.Sprintf("cronJob name must be no more than %d characters to accommodate job name suffix (max %d total)",
			maxBaseNameLength, maxTotalLength)
	}

	if errs := validation.IsQualifiedName(name); len(errs) > 0 {
		return fmt.Sprintf("invalid cronJob name %q: %v", name, strings.Join(errs, "; "))
	}

	return ""
}
