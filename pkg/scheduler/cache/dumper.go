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
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path"
	"runtime"
	"strings"
	"syscall"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
)

// Dumper writes some information from the scheduler cache to the scheduler logs
// for debugging purposes. Usage: run `kill -s USR2 <pid>` in the shell, where <pid>
// is the process id of the scheduler process.
type Dumper struct {
	Cache   Cache
	RootDir string // target directory for the dumped json file
}

// dumpToJSONFile marsh scheduler cache snapshot to json file
func (d *Dumper) dumpToJSONFile() {
	snapshot := d.Cache.Snapshot()
	name := fmt.Sprintf("snapshot-%d.json", time.Now().Unix())
	fName := path.Join(d.RootDir, name)
	file, err := os.OpenFile(fName, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		klog.Errorf("error creating snapshot because of error creating file: %v", err)
		return
	}
	defer file.Close()
	klog.Infoln("Starting to dump info in scheduler cache to file", fName)

	if err := encodeCache(file, snapshot.Nodes, snapshot.HyperNodesSetByTier, snapshot.RealNodesSet, snapshot.Jobs); err != nil {
		klog.Errorf("Failed to dump info in scheduler cache, json encode error: %v", err)
		return
	}

	klog.Infoln("Successfully dump info in scheduler cache to file", fName)
}

func encodeCache(file *os.File, v ...interface{}) error {
	for _, item := range v {
		err := json.NewEncoder(file).Encode(item)
		if err != nil {
			return err
		}
	}
	return nil
}

// dumpAll prints all information to log
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

	klog.Info("Dump of hyperNodes info in scheduler cache")
	d.printHyperNodeInfo(snapshot.HyperNodesSetByTier, snapshot.RealNodesSet)

	d.displaySchedulerMemStats()
}

func (d *Dumper) displaySchedulerMemStats() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	klog.Infof("volcano scheduler memory stat: %+v\n", m)
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

func (d *Dumper) printHyperNodeInfo(HyperNodesSetByTier map[int]sets.Set[string], HyperNodes map[string]sets.Set[string]) {
	var data strings.Builder
	data.WriteString("\n")
	for tier, hyperNodes := range HyperNodesSetByTier {
		for hyperNode := range hyperNodes {
			data.WriteString(fmt.Sprintf("tier: %d, HyperNodeName: %s, Nodes: %s\n", tier, hyperNode, HyperNodes[hyperNode]))
		}
	}
	data.WriteString("\n")
}

// ListenForSignal starts a goroutine that will respond when process
// receives SIGUSER1/SIGUSER2 signal.
func (d *Dumper) ListenForSignal(stopCh <-chan struct{}) {
	ch1 := make(chan os.Signal, 1)
	ch2 := make(chan os.Signal, 1)
	signal.Notify(ch1, syscall.SIGUSR1)
	signal.Notify(ch2, syscall.SIGUSR2)
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case <-ch1:
				d.dumpToJSONFile()
			case <-ch2:
				d.dumpAll()
			}
		}
	}()
}
