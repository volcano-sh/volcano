// +build !ignore_autogenerated

/*
Copyright 2017 The Kubernetes Authors.

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

// This file was autogenerated by deepcopy-gen. Do not edit it manually!

package v1

import (
	core_v1 "k8s.io/api/core/v1"
	conversion "k8s.io/apimachinery/pkg/conversion"
	runtime "k8s.io/apimachinery/pkg/runtime"
	reflect "reflect"
)

// GetGeneratedDeepCopyFuncs returns the generated funcs, since we aren't registering them.
//
// Deprecated: deepcopy registration will go away when static deepcopy is fully implemented.
func GetGeneratedDeepCopyFuncs() []conversion.GeneratedDeepCopyFunc {
	return []conversion.GeneratedDeepCopyFunc{
		{Fn: func(in interface{}, out interface{}, c *conversion.Cloner) error {
			in.(*Consumer).DeepCopyInto(out.(*Consumer))
			return nil
		}, InType: reflect.TypeOf(&Consumer{})},
		{Fn: func(in interface{}, out interface{}, c *conversion.Cloner) error {
			in.(*ConsumerList).DeepCopyInto(out.(*ConsumerList))
			return nil
		}, InType: reflect.TypeOf(&ConsumerList{})},
		{Fn: func(in interface{}, out interface{}, c *conversion.Cloner) error {
			in.(*ConsumerSpec).DeepCopyInto(out.(*ConsumerSpec))
			return nil
		}, InType: reflect.TypeOf(&ConsumerSpec{})},
		{Fn: func(in interface{}, out interface{}, c *conversion.Cloner) error {
			in.(*ConsumerStatus).DeepCopyInto(out.(*ConsumerStatus))
			return nil
		}, InType: reflect.TypeOf(&ConsumerStatus{})},
		{Fn: func(in interface{}, out interface{}, c *conversion.Cloner) error {
			in.(*QueueJob).DeepCopyInto(out.(*QueueJob))
			return nil
		}, InType: reflect.TypeOf(&QueueJob{})},
		{Fn: func(in interface{}, out interface{}, c *conversion.Cloner) error {
			in.(*QueueJobList).DeepCopyInto(out.(*QueueJobList))
			return nil
		}, InType: reflect.TypeOf(&QueueJobList{})},
		{Fn: func(in interface{}, out interface{}, c *conversion.Cloner) error {
			in.(*QueueJobSpec).DeepCopyInto(out.(*QueueJobSpec))
			return nil
		}, InType: reflect.TypeOf(&QueueJobSpec{})},
		{Fn: func(in interface{}, out interface{}, c *conversion.Cloner) error {
			in.(*QueueJobStatus).DeepCopyInto(out.(*QueueJobStatus))
			return nil
		}, InType: reflect.TypeOf(&QueueJobStatus{})},
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Consumer) DeepCopyInto(out *Consumer) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Consumer.
func (in *Consumer) DeepCopy() *Consumer {
	if in == nil {
		return nil
	}
	out := new(Consumer)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Consumer) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	} else {
		return nil
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConsumerList) DeepCopyInto(out *ConsumerList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Consumer, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConsumerList.
func (in *ConsumerList) DeepCopy() *ConsumerList {
	if in == nil {
		return nil
	}
	out := new(ConsumerList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ConsumerList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	} else {
		return nil
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConsumerSpec) DeepCopyInto(out *ConsumerSpec) {
	*out = *in
	if in.Reserved != nil {
		in, out := &in.Reserved, &out.Reserved
		*out = make(core_v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConsumerSpec.
func (in *ConsumerSpec) DeepCopy() *ConsumerSpec {
	if in == nil {
		return nil
	}
	out := new(ConsumerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConsumerStatus) DeepCopyInto(out *ConsumerStatus) {
	*out = *in
	if in.Deserved != nil {
		in, out := &in.Deserved, &out.Deserved
		*out = make(core_v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	if in.Allocated != nil {
		in, out := &in.Allocated, &out.Allocated
		*out = make(core_v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	if in.Used != nil {
		in, out := &in.Used, &out.Used
		*out = make(core_v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	if in.Preempting != nil {
		in, out := &in.Preempting, &out.Preempting
		*out = make(core_v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConsumerStatus.
func (in *ConsumerStatus) DeepCopy() *ConsumerStatus {
	if in == nil {
		return nil
	}
	out := new(ConsumerStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *QueueJob) DeepCopyInto(out *QueueJob) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new QueueJob.
func (in *QueueJob) DeepCopy() *QueueJob {
	if in == nil {
		return nil
	}
	out := new(QueueJob)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *QueueJob) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	} else {
		return nil
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *QueueJobList) DeepCopyInto(out *QueueJobList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	out.ListMeta = in.ListMeta
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]QueueJob, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new QueueJobList.
func (in *QueueJobList) DeepCopy() *QueueJobList {
	if in == nil {
		return nil
	}
	out := new(QueueJobList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *QueueJobList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	} else {
		return nil
	}
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *QueueJobSpec) DeepCopyInto(out *QueueJobSpec) {
	*out = *in
	if in.ResourceUnit != nil {
		in, out := &in.ResourceUnit, &out.ResourceUnit
		*out = make(core_v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new QueueJobSpec.
func (in *QueueJobSpec) DeepCopy() *QueueJobSpec {
	if in == nil {
		return nil
	}
	out := new(QueueJobSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *QueueJobStatus) DeepCopyInto(out *QueueJobStatus) {
	*out = *in
	if in.Allocated != nil {
		in, out := &in.Allocated, &out.Allocated
		*out = make(core_v1.ResourceList, len(*in))
		for key, val := range *in {
			(*out)[key] = val.DeepCopy()
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new QueueJobStatus.
func (in *QueueJobStatus) DeepCopy() *QueueJobStatus {
	if in == nil {
		return nil
	}
	out := new(QueueJobStatus)
	in.DeepCopyInto(out)
	return out
}
