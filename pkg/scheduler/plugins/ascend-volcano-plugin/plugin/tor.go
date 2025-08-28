/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.

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

/*
Package plugin is using for HuaWei Ascend pin affinity schedule frame.
*/

package plugin

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

// InitTorNodeInfo initializes Tor node information if the basic Tor node configmap exists
func (sHandle *ScheduleHandler) InitTorNodeInfo(ssn *framework.Session) {
	if err := sHandle.validateInitParams(ssn); err != nil {
		klog.V(util.LogErrorLev).Infof("InitTorNodeInfo validation failed: %v", err)
		return
	}
	sHandle.Tors = nil
	cm, err := sHandle.getTorConfigMap(ssn)
	if err != nil {
		return
	}
	torList, err := sHandle.initTorListFromConfigMap(cm)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("Failed to initialize Tor list: %v", err)
		return
	}

	sHandle.Tors = torList
}

// validateInitParams checks if required parameters are valid
func (sHandle *ScheduleHandler) validateInitParams(ssn *framework.Session) error {
	if sHandle == nil || ssn == nil {
		return fmt.Errorf("invalid parameters: %s", util.ArgumentError)
	}
	return nil
}

// getTorConfigMap retrieves Tor configmap from Kubernetes
func (sHandle *ScheduleHandler) getTorConfigMap(ssn *framework.Session) (*v1.ConfigMap, error) {
	cm, err := k8s.GetTorNodeWithOneMinuteDelay(ssn.KubeClient(), util.DevInfoNameSpace, TorNodeCMName)
	if err != nil {
		if !errors.IsNotFound(err) {
			klog.V(util.LogWarningLev).Infof("Failed to get Tor configmap: %v", err)
		}
		return nil, err
	}
	return cm, nil
}

// initTorListFromConfigMap initializes TorList from configmap data
func (sHandle *ScheduleHandler) initTorListFromConfigMap(cm *v1.ConfigMap) (*TorList, error) {
	torList := &TorList{
		torIpMap:   make(map[string]string),
		torMaps:    make(map[string]*Tor),
		serverMaps: make(map[string]*Server),
	}

	if err := json.Unmarshal([]byte(cm.Data[TorInfoCMKey]), torList); err != nil {
		return nil, fmt.Errorf("unmarshal Tor config failed: %w", err)
	}

	if err := sHandle.configureTorList(torList, cm); err != nil {
		return nil, err
	}

	return torList, nil
}

// configureTorList configures TorList with additional settings
func (sHandle *ScheduleHandler) configureTorList(torList *TorList, cm *v1.ConfigMap) error {
	if len(torList.Tors) == 0 {
		klog.V(util.LogDebugLev).Info("Empty Tor configmap, skipping initialization")
		return fmt.Errorf("empty Tor configuration")
	}

	if level, ok := cm.Data[TorLevelCMKey]; ok {
		torList.TorLevel = level
		klog.V(util.LogInfoLev).Infof("Initialized Tor level: %s", level)
	}

	torList.nslbVersion = sHandle.FrameAttr.NslbVersion
	torList.sharedTorNum = sHandle.FrameAttr.SharedTorNum

	torList.initNodeNameByNodeIp(sHandle.Nodes)
	torList.syncBySsnJobs(sHandle.Jobs)
	torList.initTorMaps()

	if torList.nslbVersion == NSLB2Version {
		torList.initTorShareStatus(sHandle.Jobs)
	}

	return nil
}

// TorList tor info about nodes
type TorList struct {
	sharedTorNum int
	nslbVersion  string
	TorLevel     string `json:"-"`
	Version      string `json:"version"`
	TorCount     int    `json:"tor_count"`
	Tors         []*Tor `json:"server_list"`
	torMaps      map[string]*Tor
	serverMaps   map[string]*Server
	torIpMap     map[string]string
}

// Tor tor info include server
type Tor struct {
	FreeServerCount int
	IsHealthy       int
	IsSharedTor     int
	Id              int       `json:"tor_id"`
	IP              string    `json:"tor_ip"`
	Servers         []*Server `json:"server"`
	Jobs            map[api.JobID]SchedulerJob
}

// Server server info
type Server struct {
	IsUsedByMulJob bool   `json:"-"`
	NodeRank       string `json:"-"`
	IP             string `json:"server_ip"`
	Count          int    `json:"npu_count"`
	SliceId        int    `json:"slice_id"`
	Jobs           sets.String
	CurrentJob     *api.JobID
	Name           string
}

// TorShare tor share info
type TorShare struct {
	IsHealthy   int
	IsSharedTor int
	NodeJobs    []NodeJobInfo `json:"nodes"`
}

// NodeJobInfo node job info
type NodeJobInfo struct {
	NodeIp   string
	NodeName string
	JobName  []string
}

func (tl *TorList) initTorMaps() {
	for _, tor := range tl.Tors {
		tl.torMaps[tor.IP] = tor
		for _, server := range tor.Servers {
			tl.serverMaps[server.Name] = server
			tl.torIpMap[server.Name] = tor.IP
		}
	}
}

// GetTorMaps return tor maps
func (tl *TorList) GetTorMaps() map[string]*Tor {
	if tl == nil {
		return nil
	}
	return tl.torMaps
}

// GetServerMaps return server maps
func (tl *TorList) GetServerMaps() map[string]*Server {
	if tl == nil {
		return nil
	}
	return tl.serverMaps
}

// GetTorIpMap return tor ip map
func (tl *TorList) GetTorIpMap() map[string]string {
	if tl == nil {
		return nil
	}
	return tl.torIpMap
}

func (tl *TorList) initNodeNameByNodeIp(nodes map[string]NPUNode) {
	ipNodeMap := make(map[string]NPUNode, len(nodes))
	for _, node := range nodes {
		ipNodeMap[node.Address] = node
	}
	for _, tor := range tl.Tors {
		for _, tNode := range tor.Servers {
			if node, ok := ipNodeMap[tNode.IP]; ok {
				tNode.Name = node.Name
				continue
			}
			klog.V(util.LogDebugLev).Infof("tor node configmap info error, the ip : %s missing", tNode.IP)
		}
	}
}

func (tl *TorList) syncBySsnJobs(jobs map[api.JobID]SchedulerJob) {
	for _, job := range jobs {
		tl.syncByJob(job)
	}
}

func (tl *TorList) syncByJob(job SchedulerJob) {
	for _, task := range job.Tasks {
		if task.NodeName == "" {
			continue
		}

		tor, server := tl.getTorAndServerByNodeName(task.NodeName)
		if server == nil {
			continue
		}
		if server.Jobs == nil {
			server.Jobs = sets.String{}
		}
		server.Jobs.Insert(job.ReferenceName)
		if tor.Jobs == nil {
			tor.Jobs = map[api.JobID]SchedulerJob{}
		}
		tor.Jobs[job.Name] = job
	}
}

func (tl *TorList) getTorAndServerByNodeName(nodeName string) (*Tor, *Server) {
	for _, tor := range tl.Tors {
		for _, tNode := range tor.Servers {
			if tNode.Name == nodeName {
				return tor, tNode
			}
		}
	}
	return nil, nil
}

func (tl *TorList) initTorShareStatus(jobs map[api.JobID]SchedulerJob) {
	for _, job := range jobs {
		if job.Status != util.PodGroupRunning && job.Status != util.PodGroupUnknown {
			continue
		}
		for _, task := range job.Tasks {
			if tor, ok := tl.torMaps[tl.torIpMap[task.NodeName]]; ok {
				tor.setTorIsSharedTor(task.Annotation[isSharedTor])
				tor.setTorIsHealthy(task.Annotation[isHealthy])
			}
		}
	}
}

// MarkTorListByJobV1 mark the global tor list by node list a job can be scheduled
func (tl *TorList) MarkTorListByJobV1(nodes map[string]*api.NodeInfo, jobUid api.JobID, taskNum int) {
	if tl == nil {
		return
	}
	for _, tor := range tl.Tors {
		count := 0
		tmpName := jobUid
		for _, server := range tor.Servers {
			if nodes[server.Name] != nil {
				count++
				server.CurrentJob = &tmpName
			}
		}
		if tor.HasAcrossJob(false, jobUid) && taskNum > count {
			tmpName = ""
		}
	}
}

// MarkTorListByJobV2 mark the global tor list by node list a job can be scheduled
func (tl *TorList) MarkTorListByJobV2(nodes map[string]*api.NodeInfo, jobUid api.JobID) {
	if tl == nil {
		return
	}
	for _, tor := range tl.Tors {
		count := 0
		tmpName := jobUid
		for _, server := range tor.Servers {
			if nodes[server.Name] != nil {
				count++
				server.CurrentJob = &tmpName
			}
		}
		tor.FreeServerCount = count
	}
}

// SetTorFreeServerCountAndGetFullTor get the num of full tor
func (tl *TorList) SetTorFreeServerCountAndGetFullTor(jobUid api.JobID) int {
	if tl == nil {
		return 0
	}
	var fullTorNum int
	for _, tor := range tl.Tors {
		count := 0
		for _, l := range tor.Servers {
			if l.CurrentJob != nil && *l.CurrentJob == jobUid {
				count++
			}
		}
		if count == tl.TorCount {
			fullTorNum++
		}
		tor.FreeServerCount = count
	}
	return fullTorNum
}

// GetLogicTorsAndFullTorNum get logic tor list by global tor list
func (tl *TorList) GetLogicTorsAndFullTorNum(jobUid api.JobID, taskColumn, taskRow, netSliceNum int) ([]*Tor, int) {
	if netSliceNum > util.MaxSliceNum {
		klog.V(util.LogDebugLev).Infof("GetLogicTorList failed:%s", util.ArgumentError)
		return nil, 0
	}
	torTransposed := make([][]*Server, netSliceNum)
	for _, tor := range tl.Tors {
		for i, server := range tor.Servers {
			if server.CurrentJob == nil || *server.CurrentJob != jobUid {
				continue
			}
			if i >= len(torTransposed) {
				klog.V(util.LogDebugLev).Infof("invalid i: %d, logicTorList length: %d", i, len(torTransposed))
			}
			torTransposed[i] = append(torTransposed[i], server)
		}
	}

	sort.Slice(torTransposed, func(i, j int) bool {
		return len(torTransposed[i]) > len(torTransposed[j])
	})

	// judge the node num in slice x is enough for job request
	// for example: a job has 22 npu task , netSliceNum is 4. taskRow = 5, taskColumn = 1
	// slice 1 must have 5 nodes
	if len(torTransposed[taskColumn]) < taskRow+1 {
		klog.V(util.LogWarningLev).Infof("tor check failed not enough resource by netslice <%d> server num"+
			" <%d> is not enough for job require %d", getNetSliceId(torTransposed[taskColumn]),
			len(torTransposed[taskColumn]), taskRow+1)
		return nil, 0
	}

	if taskRow > 0 && len(torTransposed[netSliceNum-1]) < taskRow {
		klog.V(util.LogWarningLev).Infof("tor check failed not enough resource by logicTor full tor num "+
			"<%d> is not enough for job require <%d>", len(torTransposed[netSliceNum-1]), taskRow)
		return nil, 0
	}

	return getLogicTorsAndFullTorNum(netSliceNum, torTransposed)
}

// GetSharedTorNum get shared tor num
func (tl *TorList) GetSharedTorNum() int {
	if tl == nil {
		klog.V(util.LogDebugLev).Infof("getSharedTorNum %s.", util.ArgumentError)
		return util.ErrorInt
	}
	return tl.sharedTorNum
}

// GetNSLBVersion get nslb version
func (tl *TorList) GetNSLBVersion() string {
	if tl == nil {
		klog.V(util.LogDebugLev).Infof("getNSLBVersion %s.", util.ArgumentError)
		return ""
	}
	return tl.nslbVersion
}

// RecoveryGlobalTor recovery global tor in cache
func (tl *TorList) RecoveryGlobalTor(tors []*Tor) {
	tl.Tors = tors
	tl.initTorMaps()
}

// getLogicTorsListAndFullTorNum transpose the logic tor list
func getLogicTorsAndFullTorNum(torCount int, torTransposed [][]*Server) ([]*Tor, int) {
	tors := make([]*Tor, 0)
	var fullTor int
	for i := 0; i <= len(torTransposed[0]); i++ {
		tmpTor := &Tor{}
		for j := 0; j < torCount; j++ {
			if j >= len(torTransposed) {
				klog.V(util.LogDebugLev).Infof("invalid j: %d, logicList length: %d", j, len(torTransposed))
				return tors, fullTor
			}
			if len(torTransposed[j]) < i+1 {
				break
			}
			tmpTor.Servers = append(tmpTor.Servers, torTransposed[j][i])
			if j == torCount-1 {
				fullTor++
			}
		}
		tmpTor.FreeServerCount = len(tmpTor.Servers)
		tors = append(tors, tmpTor)
	}
	return tors, fullTor
}

func (t *Tor) setTorIsSharedTor(isShared string) {
	if t.IsSharedTor != freeTor {
		return
	}
	isSharedT, err := strconv.Atoi(isShared)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("setTorIsSharedTor err %s", err)
	}
	t.IsSharedTor = isSharedT
}

func (t *Tor) setTorIsHealthy(isHealthy string) {
	if t.IsHealthy == unhealthyTor {
		return
	}
	isHealthyT, err := strconv.Atoi(isHealthy)
	if err != nil {
		klog.V(util.LogErrorLev).Infof("setTorIsHealthy err %s", err)
	}
	t.IsHealthy = isHealthyT
}

// GetNodeByNodeName get node by node name
func (t *Tor) getNodeByNodeName(name string) *Server {
	for _, tNode := range t.Servers {
		if tNode.Name == name {
			return tNode
		}
	}
	return nil
}

// HasAcrossJob whether has across job
func (t *Tor) HasAcrossJob(isNSLBv2 bool, jobName api.JobID) bool {
	if t == nil {
		klog.V(util.LogInfoLev).Infof("HasAcrossJob failed: %s", util.ArgumentError)
		return false
	}
	for _, tNode := range t.Servers {
		if tNode.IsUsedByMulJob {
			return true
		}
	}
	for _, job := range t.Jobs {
		if !job.preCheckForTorHasAcrossJob(isNSLBv2, jobName) {
			continue
		}
		for _, task := range job.Tasks {
			if task.NodeName == "" {
				continue
			}
			if t.getNodeByNodeName(task.NodeName) == nil {
				return true
			}
		}
	}
	return false
}

// IsUsedByAcrossLargeModelJob whether used by across large model job
func (t *Tor) IsUsedByAcrossLargeModelJob() bool {
	if t == nil {
		klog.V(util.LogInfoLev).Infof("IsUsedByAcrossLargeModelJob failed: %s", util.ArgumentError)
		return false
	}
	return t.HasAcrossJob(true, "")
}

// getNetSliceId get net slice num by first server's SliceId
func getNetSliceId(servers []*Server) int {
	for _, server := range servers {
		if server == nil {
			continue
		}
		return server.SliceId
	}
	return -1
}
