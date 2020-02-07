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

package router

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/kubernetes"
	"volcano.sh/volcano/pkg/webhooks/util"

	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

// ConversionFunc is the user defined function for any conversion. The code in this file is a
// template that can be use for any CR conversion given this function.
type ConversionFunc func(Object *unstructured.Unstructured, version string) (*unstructured.Unstructured, metav1.Status)

type ConversionService struct {
	Path    string
	Func    ConversionFunc
	Handler util.Handler
	Names   []string
	Config  *ConversionServiceConfig
}

type ConversionServiceConfig struct {
	SchedulerName string
	KubeClient    kubernetes.Interface
	VolcanoClient versioned.Interface
}
