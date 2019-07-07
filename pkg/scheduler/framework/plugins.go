/*
Copyright 2018 The Kubernetes Authors.

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

package framework

import "sync"

var pluginMutex sync.Mutex

// PluginBuilder plugin management
type PluginBuilder func(Arguments) Plugin

// ActionBuilder action management
type ActionBuilder func(Arguments) Action

// Plugin management
var pluginBuilders = map[string]PluginBuilder{}

// RegisterPluginBuilder register the plugin
func RegisterPluginBuilder(name string, pc PluginBuilder) {
	pluginMutex.Lock()
	defer pluginMutex.Unlock()

	pluginBuilders[name] = pc
}

// CleanupPluginBuilders cleans up all the plugin
func CleanupPluginBuilders() {
	pluginMutex.Lock()
	defer pluginMutex.Unlock()

	pluginBuilders = map[string]PluginBuilder{}
}

// GetPluginBuilder get the pluginbuilder by name
func GetPluginBuilder(name string) (PluginBuilder, bool) {
	pluginMutex.Lock()
	defer pluginMutex.Unlock()

	pb, found := pluginBuilders[name]
	return pb, found
}

// Action management
var actionMap = map[string]ActionBuilder{}

// RegisterAction register action
func RegisterAction(name string, act ActionBuilder) {
	pluginMutex.Lock()
	defer pluginMutex.Unlock()

	actionMap[name] = act
}

// GetAction get the action by name
func GetAction(name string) (ActionBuilder, bool) {
	pluginMutex.Lock()
	defer pluginMutex.Unlock()

	act, found := actionMap[name]
	return act, found
}
