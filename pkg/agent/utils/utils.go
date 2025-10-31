/*
Copyright 2024 The Volcano Authors.

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

package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	v1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/kubelet/cm/cpumanager/state"

	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/config/api"
	"volcano.sh/volcano/pkg/agent/utils/file"
)

var (
	SysFsPathEnv     = "SYS_FS_PATH"
	DefaultSysFsPath = "/sys/fs"
)

const (
	// Component is volcano agent component name
	Component = "volcano-agent"

	kubeletRootDirEnv = "KUBELET_ROOT_DIR"

	defaultKubeletRootDir = "/var/lib/kubelet"

	cpuManagerState = "cpu_manager_state"

	evictionKind            = "Eviction"
	evictionSubResourceName = "pods/eviction"

	defaultCPUThrottlingThreshold = 80
	defaultCPUProtectionWatermark = 30
)

func UpdateFile(path string, content []byte) error {
	oldValue, err := file.ReadByteFromFile(path)
	if err != nil {
		return fmt.Errorf("failed to get value from file(%s): %w", path, err)
	}

	if strings.Compare(strings.TrimRight(string(oldValue), "\n"), string(content)) == 0 {
		return nil
	}

	err = file.WriteByteToFile(path, content)
	if err != nil {
		return fmt.Errorf("failed to write content(%s) to file(%s): %w", content, path, err)
	}

	klog.InfoS("Successfully update content to file", "content", content, "file", path)
	return nil
}

func UpdatePodCgroup(cgroupFile string, content []byte) error {
	dir, file := path.Split(cgroupFile)
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, iErr error) error {
		if iErr != nil {
			return iErr
		}
		if d != nil && !d.IsDir() {
			return nil
		}
		if updateErr := UpdateFile(path.Join(p, file), content); updateErr != nil {
			return fmt.Errorf("failed to update file(%s): %w", path.Join(dir, file), updateErr)
		}
		return nil
	})
	return err
}

type OSRelease struct {
	Name      string
	Version   string
	ID        string
	VersionID string
}

func GetOSReleaseFromFile(releaseFile string) (*OSRelease, error) {
	var release OSRelease
	content, err := file.ReadByteFromFile(releaseFile)
	if err != nil {
		klog.ErrorS(err, "Failed to read release path", "path", releaseFile)
		return nil, err
	}

	lineContents := strings.Split(string(content), "\n")
	for index := range lineContents {
		pairs := strings.Split(lineContents[index], "=")
		if len(pairs) != 2 {
			continue
		}
		switch strings.Trim(pairs[0], "\"") {
		case "NAME":
			release.Name = strings.Trim(pairs[1], "\"")
		case "VERSION":
			release.Version = strings.Trim(pairs[1], "\"")
		case "ID":
			release.ID = strings.Trim(pairs[1], "\"")
		case "VERSION_ID":
			release.VersionID = strings.Trim(pairs[1], "\"")
		default:
		}
	}
	return &release, nil
}

// SetEvictionWatermark set eviction watermark.
func SetEvictionWatermark(cfg *api.ColocationConfig, lowWatermark apis.Watermark, highWatermark apis.Watermark) {
	lowWatermark[v1.ResourceCPU] = *cfg.EvictingConfig.EvictingCPULowWatermark
	lowWatermark[v1.ResourceMemory] = *cfg.EvictingConfig.EvictingMemoryLowWatermark
	highWatermark[v1.ResourceCPU] = *cfg.EvictingConfig.EvictingCPUHighWatermark
	highWatermark[v1.ResourceMemory] = *cfg.EvictingConfig.EvictingMemoryHighWatermark
	klog.InfoS("Successfully set watermark",
		"cpuLowWatermark", *cfg.EvictingConfig.EvictingCPULowWatermark,
		"cpuHighWatermark", *cfg.EvictingConfig.EvictingCPUHighWatermark,
		"memoryLowWatermark", *cfg.EvictingConfig.EvictingMemoryLowWatermark,
		"memoryHighWatermark", *cfg.EvictingConfig.EvictingMemoryHighWatermark)
}

// SetCPUThrottlingConfig set CPU QoS configuration from colocation config.
func SetCPUThrottlingConfig(cfg *api.ColocationConfig) (throttlingThreshold, protectionWatermark int) {
	if cfg.CPUThrottlingConfig.CPUThrottlingThreshold != nil {
		throttlingThreshold = *cfg.CPUThrottlingConfig.CPUThrottlingThreshold
	} else {
		throttlingThreshold = defaultCPUThrottlingThreshold
	}

	if cfg.CPUThrottlingConfig.CPUProtectionWatermark != nil {
		protectionWatermark = *cfg.CPUThrottlingConfig.CPUProtectionWatermark
	} else {
		protectionWatermark = defaultCPUProtectionWatermark
	}

	klog.InfoS("Successfully set CPU QoS config",
		"throttlingThreshold", throttlingThreshold,
		"protectionWatermark", protectionWatermark)
	return throttlingThreshold, protectionWatermark
}

func GetCPUManagerPolicy() string {
	kubeletDir := strings.TrimSpace(os.Getenv(kubeletRootDirEnv))
	if kubeletDir == "" {
		kubeletDir = defaultKubeletRootDir
	}

	b, err := os.ReadFile(path.Join(kubeletDir, cpuManagerState))
	if err != nil {
		klog.ErrorS(err, "Failed to read cpu manager state file")
		return ""
	}
	s := &state.CPUManagerCheckpoint{}
	if err = json.Unmarshal(b, s); err != nil {
		klog.ErrorS(err, "Failed to unmarshal cpu manager state")
		return ""
	}
	return s.PolicyName
}

func GetEvictionVersion(kubeClient clientset.Interface) (string, error) {
	resourceList, err := kubeClient.Discovery().ServerResourcesForGroupVersion("v1")
	if err != nil {
		return "", err
	}

	for _, apiResource := range resourceList.APIResources {
		if apiResource.Name == evictionSubResourceName && apiResource.Kind == evictionKind {
			return apiResource.Version, nil
		}
	}
	return "", fmt.Errorf("eviction version not found")
}
