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
*/

package scheduler

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"google.golang.org/grpc"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	"volcano.sh/volcano/pkg/filewatcher"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedcache "volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/metrics"
	"volcano.sh/volcano/pkg/scheduler/plugins/vgpupredicates"
	vgpuproto "volcano.sh/volcano/pkg/scheduler/plugins/vgpupredicates/protos"
)

// Scheduler watches for new unscheduled pods for volcano. It attempts to find
// nodes that they fit on and writes bindings back to the api server.
type Scheduler struct {
	cache          schedcache.Cache
	schedulerConf  string
	fileWatcher    filewatcher.FileWatcher
	schedulePeriod time.Duration
	once           sync.Once

	mutex          sync.Mutex
	actions        []framework.Action
	plugins        []conf.Tier
	configurations []conf.Configuration

	vgpuServerStarted bool
}

// NewScheduler returns a scheduler
func NewScheduler(
	config *rest.Config,
	schedulerName string,
	schedulerConf string,
	period time.Duration,
	defaultQueue string,
	nodeSelectors []string,
) (*Scheduler, error) {
	var watcher filewatcher.FileWatcher
	if schedulerConf != "" {
		var err error
		path := filepath.Dir(schedulerConf)
		watcher, err = filewatcher.NewFileWatcher(path)
		if err != nil {
			return nil, fmt.Errorf("failed creating filewatcher for %s: %v", schedulerConf, err)
		}
	}

	scheduler := &Scheduler{
		schedulerConf:     schedulerConf,
		fileWatcher:       watcher,
		cache:             schedcache.New(config, schedulerName, defaultQueue, nodeSelectors),
		schedulePeriod:    period,
		vgpuServerStarted: false,
	}

	return scheduler, nil
}

// GetCache is used by cmd.scheduler to get schedcache.Cache for grpc communication
func (pc *Scheduler) GetCache() schedcache.Cache {
	return pc.cache
}

// Run runs the Scheduler
func (pc *Scheduler) Run(stopCh <-chan struct{}) {
	pc.loadSchedulerConf()
	go pc.watchSchedulerConf(stopCh)
	// Start cache for policy.
	pc.cache.Run(stopCh)
	pc.cache.WaitForCacheSync(stopCh)
	klog.V(2).Infof("scheduler completes Initialization and start to run")
	go pc.startVgpuServer()
	go wait.Until(pc.runOnce, pc.schedulePeriod, stopCh)
}

func (pc *Scheduler) startVgpuServer() {
	klog.V(4).Infof("Starting vgpuserver...")

	pc.mutex.Lock()
	plugins := pc.plugins
	configurations := pc.configurations
	pc.mutex.Unlock()

	startvgpuserver := false
	ssn := framework.OpenSession(pc.cache, plugins, configurations)
	defer framework.CloseSession(ssn)
	if !pc.vgpuServerStarted {
		for _, val := range ssn.Tiers {
			for _, plugins := range val.Plugins {
				if strings.Contains(plugins.Name, "vgpupredicates") {
					for idx, arguments := range plugins.Arguments {
						klog.Infof(idx, ":", arguments)
						if strings.Contains(idx, "GPUSharingEnable") && strings.Contains(arguments, "true") {
							startvgpuserver = true
						}
					}
				}
			}
		}
	}
	if startvgpuserver {
		klog.Infof("Need to startvgpuserver here")
		pc.vgpuServerStarted = true
		// start grpc server

		lisGrpc, _ := net.Listen("tcp", vgpupredicates.GrpcBind)
		defer lisGrpc.Close()
		s := grpc.NewServer()
		vgpuproto.RegisterDeviceServiceServer(s, pc)
		err := s.Serve(lisGrpc)
		if err != nil {
			klog.Fatal(err)
		}
	}
}

func (pc *Scheduler) runOnce() {
	klog.V(4).Infof("Start scheduling ...")
	scheduleStartTime := time.Now()
	defer klog.V(4).Infof("End scheduling ...")

	pc.mutex.Lock()
	actions := pc.actions
	plugins := pc.plugins
	configurations := pc.configurations
	pc.mutex.Unlock()

	//startvgpuserver := false
	ssn := framework.OpenSession(pc.cache, plugins, configurations)
	defer framework.CloseSession(ssn)

	for _, action := range actions {
		actionStartTime := time.Now()
		action.Execute(ssn)
		metrics.UpdateActionDuration(action.Name(), metrics.Duration(actionStartTime))
	}
	metrics.UpdateE2eDuration(metrics.Duration(scheduleStartTime))
}

func (pc *Scheduler) loadSchedulerConf() {
	var err error
	pc.once.Do(func() {
		pc.actions, pc.plugins, pc.configurations, err = unmarshalSchedulerConf(defaultSchedulerConf)
		if err != nil {
			klog.Errorf("unmarshal scheduler config %s failed: %v", defaultSchedulerConf, err)
			panic("invalid default configuration")
		}
	})

	var config string
	if len(pc.schedulerConf) != 0 {
		if config, err = readSchedulerConf(pc.schedulerConf); err != nil {
			klog.Errorf("Failed to read scheduler configuration '%s', using previous configuration: %v",
				pc.schedulerConf, err)
			return
		}
	}

	actions, plugins, configurations, err := unmarshalSchedulerConf(config)
	if err != nil {
		klog.Errorf("scheduler config %s is invalid: %v", config, err)
		return
	}

	pc.mutex.Lock()
	// If it is valid, use the new configuration
	pc.actions = actions
	pc.plugins = plugins
	pc.configurations = configurations
	pc.mutex.Unlock()
}

func (pc *Scheduler) watchSchedulerConf(stopCh <-chan struct{}) {
	if pc.fileWatcher == nil {
		return
	}
	eventCh := pc.fileWatcher.Events()
	errCh := pc.fileWatcher.Errors()
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			klog.V(4).Infof("watch %s event: %v", pc.schedulerConf, event)
			if event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create {
				pc.loadSchedulerConf()
			}
		case err, ok := <-errCh:
			if !ok {
				return
			}
			klog.Infof("watch %s error: %v", pc.schedulerConf, err)
		case <-stopCh:
			return
		}
	}
}

func (pc *Scheduler) Register(stream vgpuproto.DeviceService_RegisterServer) error {
	var nodeID string
	for {
		req, err := stream.Recv()
		if err != nil {
			klog.Infof("Register Error err=", err.Error())
		}
		if err != nil {
			//s.delNode(nodeID)
			//klog.Infof("node %v leave, %v", nodeID, err)
			_ = stream.SendAndClose(&vgpuproto.RegisterReply{})
			return err
		}
		klog.V(3).Infof("device register %v", req.String())
		nodeID = req.GetNode()
		nodes := pc.cache.(*schedcache.SchedulerCache).Nodes
		nodeinfo, ok := nodes[nodeID]
		if !ok {
			nodes[nodeID] = api.NewNodeInfo(nil)
		}
		nodes[nodeID].VGPUDevices = make([]api.VGPUDevice, len(req.Devices))
		for i := 0; i < len(req.Devices); i++ {
			nodeinfo.VGPUDevices[i] = api.VGPUDevice{
				ID:           i,
				UUID:         req.Devices[i].GetId(),
				Count:        req.Devices[i].GetCount(),
				Memory:       uint(req.Devices[i].GetDevmem()),
				Health:       req.Devices[i].GetHealth(),
				PodMap:       make(map[string]*v1.Pod),
				ContainerMap: make(map[string]*v1.Container),
			}
		}
		//s.addNode(nodeID, nodeInfo)
		klog.Infof("node %v come node info=", nodeID, nodeinfo.VGPUDevices)
	}
}

func (pc *Scheduler) GetContainer(_ context.Context, req *vgpuproto.GetContainerRequest) (*vgpuproto.GetContainerReply, error) {
	klog.Infof("into vGPU GetContainer")
	nodes := pc.cache.(*schedcache.SchedulerCache).Nodes
	podstr := strings.Split(req.Uuid, "/")[0]
	ctrName := strings.Split(req.Uuid, "/")[1]
	client := pc.cache.(*schedcache.SchedulerCache).Client()
	podnamespace := strings.Split(podstr, "_")[0]
	podname := strings.Split(podstr, "_")[1]
	pod, err := client.CoreV1().Pods(podnamespace).Get(context.TODO(), podname, metav1.GetOptions{})
	if err != nil {
		klog.Info("GetContainer Not Found", err.Error())
	}

	var devarray []*vgpuproto.DeviceUsage

	for _, node := range nodes {
		for _, vgpu := range node.VGPUDevices {
			podt, ok := vgpu.PodMap[string(pod.UID)]
			pod = podt
			if !ok {
				continue
			}
			ctr, ok := vgpu.ContainerMap[ctrName]
			if !ok {
				continue
			}
			memory := api.GetvGPUResourceOfContainer(ctr)
			temp := vgpuproto.DeviceUsage{
				Id:     vgpu.UUID,
				Devmem: int32(memory),
			}
			devarray = append(devarray, &temp)
		}
	}
	klog.Infoln("GetContainer response:devarray=", devarray)
	rep := vgpuproto.GetContainerReply{
		DevList:      devarray,
		PodUID:       string(pod.UID),
		CtrName:      ctrName,
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}
	return &rep, nil
}
