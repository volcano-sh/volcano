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

package options

import (
	"github.com/spf13/pflag"

	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
)

// Options holds configuration for podgroup controller inherit behavior.
type Options struct {
	// InheritOwnerAnnotations enables inheriting owner's annotations for pods when creating podgroup.
	InheritOwnerAnnotations bool
	// InheritOwnerAnnotationPrefixes defines owner's annotation prefixes to inherit to PodGroup.
	InheritOwnerAnnotationPrefixes []string

	// InheritPodAnnotations enables inheriting pod annotations when creating podgroup.
	InheritPodAnnotations bool
	// InheritPodAnnotationPrefixes defines pod's annotation prefixes to inherit to PodGroup.
	InheritPodAnnotationPrefixes []string

	// InheritOwnerLabels enables inheriting owner's labels when creating podgroup.
	InheritOwnerLabels bool
	// InheritOwnerLabelPrefixes defines owner's label prefixes to inherit to PodGroup.
	InheritOwnerLabelPrefixes []string

	// InheritPodLabels enables inheriting pod labels when creating podgroup.
	InheritPodLabels bool
	// InheritPodLabelPrefixes defines pod's label prefixes to inherit to PodGroup.
	InheritPodLabelPrefixes []string
}

// AddFlags adds podgroup controller flags to the specified FlagSet.
func AddFlags(fs *pflag.FlagSet, o *Options) {
	fs.BoolVar(&o.InheritOwnerAnnotations, "inherit-owner-annotations", true, "Enable inherit owner annotations for pods when create podgroup; it is enabled by default")
	fs.StringArrayVar(&o.InheritOwnerAnnotationPrefixes, "inherit-owner-annotation-prefixes", []string{"volcano.sh/"}, "Owner's annotations prefixes which will be inherited to PodGroup; by default, with 'volcano.sh/' prefix will be inherited to PodGroup")
	fs.BoolVar(&o.InheritOwnerLabels, "inherit-owner-labels", false, "Enable inherit labels of owner when create podgroup; it is disabled by default")
	fs.StringArrayVar(&o.InheritOwnerLabelPrefixes, "inherit-owner-label-prefixes", []string{"volcano.sh/"}, "Owner's labels prefixes which will be inherited to PodGroup; by default, with 'volcano.sh/' prefix will be inherited to PodGroup")

	fs.BoolVar(&o.InheritPodAnnotations, "inherit-pod-annotations", true, "Enable inherit annotations of pods when create podgroup; it is enabled by default")
	fs.StringArrayVar(&o.InheritPodAnnotationPrefixes, "inherit-pod-annotation-prefixes", []string{scheduling.PodPreemptable, scheduling.CooldownTime, scheduling.RevocableZone, scheduling.JDBMinAvailable, scheduling.JDBMaxUnavailable}, "Pod's annotation prefixes which will be inherited to PodGroup; by default, selected volcano.sh/* annotation keys (e.g. pod-preemptable, cooldown-time, revocable-zone, jdb-min-available, jdb-max-unavailable) are inherited")
	fs.BoolVar(&o.InheritPodLabels, "inherit-pod-labels", true, "Enable inherit labels of pods when create podgroup; it is enabled by default")
	fs.StringArrayVar(&o.InheritPodLabelPrefixes, "inherit-pod-label-prefixes", []string{scheduling.PodPreemptable, scheduling.CooldownTime}, "Pod's label prefixes which will be inherited to PodGroup; by default, selected volcano.sh/* label keys (e.g. pod-preemptable, cooldown-time) are inherited")
}
