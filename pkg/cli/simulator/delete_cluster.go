/*
Copyright 2025 The Volcano Authors.

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

package simulator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

type deleteClusterFlags struct {
}

var deleteClusterFlag = &deleteClusterFlags{}

// InitDeleteClusterFlags is used to init all flags during cluster deleting.
func InitDeleteClusterFlags(cmd *cobra.Command) {
}

// DeleteCluster is used to delete a cluster.
func DeleteCluster(clusterName string, ctx context.Context) error {
	scriptBaseDir, err := loadLocalUpScript(false)
	if err != nil {
		klog.Errorf("Failed to load local up simulator script: %v", err)
		return err
	}
	defer os.RemoveAll(scriptBaseDir)

	localUpCmd := exec.CommandContext(ctx, filepath.Join(scriptBaseDir, "hack", "local-up-volcano.sh"), "-q")
	localUpCmd.Dir = scriptBaseDir
	localUpCmd.Env = append(os.Environ(),
		"INSTALL_SIMULATOR=true",
		"SKIP_BUILD_IMAGE=true",
		"TAG=latest",
		"CLUSTER_NAME="+clusterName)
	localUpCmd.Stdout = os.Stdout
	localUpCmd.Stderr = os.Stderr
	if err := localUpCmd.Run(); err != nil {
		return fmt.Errorf("failed to delete kind cluster: %v", err)
	}

	return nil
}
