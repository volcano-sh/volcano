/*
Copyright 2014 The Kubernetes Authors.

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

package queuejobresources

import (
	"fmt"

	qjobv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

//Define reference manager commont interface
type RefManager interface {

	//Tag the owner
	AddTag(owner *qjobv1.QueueJobResource, getTag func() string) error

	//check whether ownee is a member of owner
	BelongTo(owner *qjobv1.QueueJobResource, ownee runtime.Object) bool

	//mark the ownee to be a member of owner
	AddReference(owner *qjobv1.QueueJobResource, ownee runtime.Object) error
}

//A reference manager by resource vector index
type RefByLabel struct {
	qjobResLabel string
}

func NewLabelRefManager() RefManager {
	return &RefByLabel{qjobResLabel: "qjobResLabel"}
}

func (rm *RefByLabel) AddTag(owner *qjobv1.QueueJobResource, getTag func() string) error {

	accessor, err := meta.Accessor(owner)
	if err != nil {
		return fmt.Errorf("Cannot get object meta")
	}

	labels := accessor.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[rm.qjobResLabel] = getTag()

	accessor.SetLabels(labels)

	return nil
}

func (rm *RefByLabel) BelongTo(owner *qjobv1.QueueJobResource, ownee runtime.Object) bool {

	accessor, err := meta.Accessor(ownee)
	if err != nil {
		return false
	}

	accessor_owner, err := meta.Accessor(owner)
	if err != nil {
		return false
	}

	labels := accessor.GetLabels()
	labels_owner := accessor_owner.GetLabels()

	return labels != nil && labels_owner != nil && labels[rm.qjobResLabel] == labels_owner[rm.qjobResLabel]

}

func (rm *RefByLabel) AddReference(owner *qjobv1.QueueJobResource, ownee runtime.Object) error {

	accessor, err := meta.Accessor(ownee)
	if err != nil {
		return fmt.Errorf("Cannot get object meta")
	}

	accessor_owner, err := meta.Accessor(owner)
	if err != nil {
		return fmt.Errorf("Cannot get object meta")
	}

	labels_owner := accessor_owner.GetLabels()

	tag, found := labels_owner[rm.qjobResLabel]
	if !found {
		return fmt.Errorf("The identification label not found")
	}

	labels := accessor.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[rm.qjobResLabel] = tag

	accessor.SetLabels(labels)

	return nil

}
