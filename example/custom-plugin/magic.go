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

package main // note!!! package must be named main

import (
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/scheduler/framework"
)

const PluginName = "magic"

type magicPlugin struct{}

func (mp *magicPlugin) Name() string {
	return PluginName
}

// New is a PluginBuilder, remove the comment when used.
func New(arguments framework.Arguments) framework.Plugin {
	return &magicPlugin{}
}

func (mp *magicPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Info("Enter magic plugin ...")
}

func (mp *magicPlugin) OnSessionClose(ssn *framework.Session) {}
