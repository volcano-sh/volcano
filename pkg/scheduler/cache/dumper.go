/*
 Copyright 2023 The Volcano Authors.

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

package cache

import (
	"os"
	"os/signal"
	"strings"
	"syscall"

	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// Dumper writes some information from the scheduler cache to the scheduler logs
// for debugging purposes. Usage: run `kill -s USR2 <pid>` in the shell, where <pid>
// is the process id of the scheduler process.
type Dumper struct {
	Cache Cache
}

func (d *Dumper) dumpAll() {
	snapshot := d.Cache.Snapshot()
	klog.Info("Dump of nodes info in scheduler cache")
	for _, nodeInfo := range snapshot.Nodes {
		klog.Info(d.printNodeInfo(nodeInfo))
	}

	klog.Info("Dump of jobs info in scheduler cache")
	for _, jobInfo := range snapshot.Jobs {
		klog.Info(d.printJobInfo(jobInfo))
	}
}

func (d *Dumper) printNodeInfo(node *api.NodeInfo) string {
	var data strings.Builder
	data.WriteString("\n")
	data.WriteString(node.String())
	data.WriteString("\n")
	return data.String()
}

func (d *Dumper) printJobInfo(jobInfo *api.JobInfo) string {
	var data strings.Builder
	data.WriteString("\n")
	data.WriteString(jobInfo.String())
	data.WriteString("\n")
	return data.String()
}

// ListenForSignal starts a goroutine that will respond when process
// receives SIGUSER2 signal.
func (d *Dumper) ListenForSignal(stopCh <-chan struct{}) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR2)
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case <-ch:
				d.dumpAll()
			}
		}
	}()
}
