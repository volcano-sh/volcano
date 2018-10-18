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
	qjobv1 "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/apis/controller/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
)

//RefManager : Define reference manager commont interface
type RefManager interface {

	//Tag the owner
	AddTag(owner *qjobv1.XQueueJobResource, getTag func() string) error

	//check whether ownee is a member of owner
	BelongTo(owner *qjobv1.XQueueJobResource, ownee runtime.Object) bool

	//mark the ownee to be a member of owner
	AddReference(owner *qjobv1.XQueueJobResource, ownee runtime.Object) error
}

//RefByLabel : A reference manager by resource vector index
type RefByLabel struct {
	qjobResLabel string
}

//NewLabelRefManager : new ref manager
func NewLabelRefManager() RefManager {
	return &RefByLabel{qjobResLabel: "qjobResLabel"}
}

//AddTag : add tag
func (rm *RefByLabel) AddTag(owner *qjobv1.XQueueJobResource, getTag func() string) error {

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

//BelongTo : belong to QJ
func (rm *RefByLabel) BelongTo(owner *qjobv1.XQueueJobResource, ownee runtime.Object) bool {

	accessor, err := meta.Accessor(ownee)
	if err != nil {
		return false
	}

	accessorOwner, err := meta.Accessor(owner)
	if err != nil {
		return false
	}

	labels := accessor.GetLabels()
	labelsOwner := accessorOwner.GetLabels()

	return labels != nil && labelsOwner != nil && labels[rm.qjobResLabel] == labelsOwner[rm.qjobResLabel]

}

//AddReference : add ref
func (rm *RefByLabel) AddReference(owner *qjobv1.XQueueJobResource, ownee runtime.Object) error {

	accessor, err := meta.Accessor(ownee)
	if err != nil {
		return fmt.Errorf("Cannot get object meta")
	}

	accessorOwner, err := meta.Accessor(owner)
	if err != nil {
		return fmt.Errorf("Cannot get object meta")
	}

	labelsOwner := accessorOwner.GetLabels()

	tag, found := labelsOwner[rm.qjobResLabel]
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
