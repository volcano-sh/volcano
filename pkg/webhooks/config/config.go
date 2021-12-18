/*
Copyright 2021 The Volcano Authors.

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

package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/filewatcher"
)

// Object the key data for resource group kind
type Object struct {
	Key   string   `yaml:"key"`
	Value []string `yaml:"value"`
}

// ResGroupConfig defines the configuration of admission.
type ResGroupConfig struct {
	ResourceGroup string            `yaml:"resourceGroup"`
	Object        Object            `yaml:"object"`
	SchedulerName string            `yaml:"schedulerName"`
	Tolerations   []v1.Toleration   `yaml:"tolerations"`
	Labels        map[string]string `yaml:"labels"`
}

// AdmissionConfiguration defines the configuration of admission.
type AdmissionConfiguration struct {
	sync.Mutex
	ResGroupsConfig []ResGroupConfig `yaml:"resourceGroups"`
}

var admissionConf AdmissionConfiguration

// LoadAdmissionConf parse the configuration from config path
func LoadAdmissionConf(confPath string) *AdmissionConfiguration {
	if confPath == "" {
		return nil
	}

	configBytes, err := ioutil.ReadFile(confPath)
	if err != nil {
		klog.Errorf("read admission file failed, err=%v", err)
		return nil
	}

	data := AdmissionConfiguration{}
	if err := yaml.Unmarshal(configBytes, &data); err != nil {
		klog.Errorf("Unmarshal admission file failed, err=%v", err)
		return nil
	}

	admissionConf.Lock()
	admissionConf.ResGroupsConfig = data.ResGroupsConfig
	admissionConf.Unlock()
	return &admissionConf
}

// WatchAdmissionConf listen the changes of the configuration file
func WatchAdmissionConf(path string, stopCh chan os.Signal) {
	dirPath := filepath.Dir(path)
	fileWatcher, err := filewatcher.NewFileWatcher(dirPath)
	if err != nil {
		klog.Errorf("failed to create filewatcher for %s: %v", path, err)
		return
	}

	eventCh := fileWatcher.Events()
	errCh := fileWatcher.Errors()
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			klog.V(4).Infof("watch %s event: %v", dirPath, event)
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				LoadAdmissionConf(path)
			}
		case err, ok := <-errCh:
			if !ok {
				return
			}
			klog.Infof("watch %s error: %v", path, err)
		case <-stopCh:
			return
		}
	}
}
