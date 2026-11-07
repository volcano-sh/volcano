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

package hypernode

import (
	"context"
	"fmt"
	"io"
	"os"

	"golang.org/x/term"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/pkg/cli/util"
)

// TreeOptions holds the connection and output options for a HyperNode tree.
type TreeOptions struct {
	util.CommonFlags
	Charset string
	DRA     bool
}

// Run reads a cluster snapshot and prints its resolved HyperNode forest.
func (o *TreeOptions) Run(ctx context.Context, out io.Writer) error {
	terminal := false
	if file, ok := out.(*os.File); ok {
		terminal = term.IsTerminal(int(file.Fd()))
	}
	unicodeLines, err := selectCharset(o.Charset, terminal, os.Getenv)
	if err != nil {
		return err
	}
	config, err := util.BuildConfig(o.Master, o.Kubeconfig)
	if err != nil {
		return fmt.Errorf("load client configuration: %w", err)
	}
	vcClient, err := versioned.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create Volcano client: %w", err)
	}
	nodeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}
	hyperNodes, err := vcClient.TopologyV1alpha1().HyperNodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list HyperNodes: %w", err)
	}
	nodes, err := nodeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list Nodes: %w", err)
	}
	var resourceSlices []resourcev1.ResourceSlice
	if o.DRA {
		slices, err := nodeClient.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("list ResourceSlices (requires resource.k8s.io/v1 and list permission; use --dra=false to omit DRA inventory): %w", err)
		}
		resourceSlices = slices.Items
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return printTree(out, hyperNodes.Items, nodes.Items, resourceSlices, unicodeLines)
}
