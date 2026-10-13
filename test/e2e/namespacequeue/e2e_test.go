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

package namespacequeue

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	e2eutil "volcano.sh/volcano/test/e2e/util"
)

var namespaceQueueSchedulerConfig *e2eutil.ConfigMapCase

var _ = BeforeSuite(func() {
	namespaceQueueSchedulerConfig = e2eutil.NewConfigMapCase(
		e2eutil.VolcanoNamespace(), "integration-scheduler-configmap",
	)
	err := namespaceQueueSchedulerConfig.ChangeBy(func(data map[string]string) (bool, map[string]string) {
		return e2eutil.ModifySchedulerConfig(data, func(config *e2eutil.SchedulerConfiguration) bool {
			for tierIndex := range config.Tiers {
				for pluginIndex := range config.Tiers[tierIndex].Plugins {
					plugin := &config.Tiers[tierIndex].Plugins[pluginIndex]
					switch plugin.Name {
					case "proportion":
						config.Tiers[tierIndex].Plugins[pluginIndex] = e2eutil.PluginOption{
							Name:             "capacity",
							EnabledHierarchy: boolPtr(true),
						}
						return true
					case "capacity":
						if plugin.EnabledHierarchy == nil || !*plugin.EnabledHierarchy {
							plugin.EnabledHierarchy = boolPtr(true)
							return true
						}
						return false
					}
				}
			}
			return false
		})
	})
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	if namespaceQueueSchedulerConfig != nil {
		Expect(namespaceQueueSchedulerConfig.UndoChanged()).NotTo(HaveOccurred())
	}
})

func boolPtr(value bool) *bool { return &value }

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "NamespaceQueue E2E Test Suite")
}
