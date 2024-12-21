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

Copyright 2024 The Volcano Authors.

Modifications made by Volcano authors:
- [2024]Register LoadAwareUtilizationPluginName plugin
*/

package main

import (
	"os"

	"k8s.io/component-base/cli"
	"sigs.k8s.io/descheduler/pkg/descheduler"
	"sigs.k8s.io/descheduler/pkg/framework/pluginregistry"

	"volcano.sh/volcano/cmd/descheduler/app"
	"volcano.sh/volcano/pkg/descheduler/framework/plugins/loadaware"
)

func init() {
	descheduler.SetupPlugins()
	pluginregistry.Register(loadaware.LoadAwareUtilizationPluginName, loadaware.NewLoadAwareUtilization, &loadaware.LoadAwareUtilization{}, &loadaware.LoadAwareUtilizationArgs{}, loadaware.ValidateLoadAwareUtilizationArgs, loadaware.SetDefaults_LoadAwareUtilizationArgs, pluginregistry.PluginRegistry)
}

func main() {
	out := os.Stdout
	cmd := app.NewDeschedulerCommand(out)
	cmd.AddCommand(app.NewVersionCommand())

	code := cli.Run(cmd)
	os.Exit(code)
}
