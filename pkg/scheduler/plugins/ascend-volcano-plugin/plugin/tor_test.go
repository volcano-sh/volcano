/*
Copyright(C)2025. Huawei Technologies Co.,Ltd. All rights reserved.

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
	"reflect"
	"testing"

	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
)

const (
	fakeServerNum     = 32
	fakeSliceNum      = 4
	fakeJobId         = " test"
	fakeEnableNodeNum = 18
)

func fakeNormalTorList(enableNodeNum int, jobUid api.JobID) *TorList {
	var tmpTors = []*Tor{}
	taskNodeNum := 0
	for i := 0; i < fakeServerNum; i++ {
		tmpTor := &Tor{}
		for j := 0; j < fakeSliceNum; j++ {
			if taskNodeNum < enableNodeNum {
				tmpTor.Servers = append(tmpTor.Servers, &Server{Name: "node", CurrentJob: &jobUid, SliceId: j})
				taskNodeNum++
				continue
			}
			tmpTor.Servers = append(tmpTor.Servers, &Server{SliceId: j})
		}
		tmpTors = append(tmpTors, tmpTor)
	}
	return &TorList{Tors: tmpTors}
}

type getLogicTorsAndFullTorNumTest struct {
	name           string
	taskRow        int
	taskColumn     int
	sliceNum       int
	wantFullTorNum int
}

func buildGetLogicTorsAndFullTorNumTestCase() []getLogicTorsAndFullTorNumTest {
	return []getLogicTorsAndFullTorNumTest{
		{
			name:           "01 will return nil 0 when SliceId is over 128",
			sliceNum:       util.MaxSliceNum + 1,
			wantFullTorNum: 0,
		},
		{
			name:           "02 will return nil 0 when taskRow is too large",
			sliceNum:       fakeSliceNum,
			taskRow:        util.NPUIndex5,
			taskColumn:     util.NPUIndex1,
			wantFullTorNum: 0,
		},
		{
			name:           "03 will return nil 0 when not enough logic tor",
			sliceNum:       fakeSliceNum,
			taskRow:        util.NPUIndex4,
			taskColumn:     util.NPUIndex1,
			wantFullTorNum: 4,
		},
	}
}

func TestTorListGetLogicTorsAndFullTorNum(t *testing.T) {
	tl := fakeNormalTorList(fakeEnableNodeNum, fakeJobId)
	for _, tt := range buildGetLogicTorsAndFullTorNumTestCase() {
		t.Run(tt.name, func(t *testing.T) {
			_, got1 := tl.GetLogicTorsAndFullTorNum(fakeJobId, tt.taskColumn, tt.taskRow, tt.sliceNum)
			if got1 != tt.wantFullTorNum {
				t.Errorf("GetLogicTorsAndFullTorNum() got1 = %v, want %v", got1, tt.wantFullTorNum)
			}
		})
	}
}

func fakeNormalJob() SchedulerJob {
	fakeJob := SchedulerJob{}
	fakeJob.Name = "test"
	fakeJob.NPUJob = &util.NPUJob{}
	fakeJob.Tasks = map[api.TaskID]util.NPUTask{
		"test-pod":  {NodeName: ""},
		"test-pod2": {NodeName: "node1"},
	}
	return fakeJob
}

type torHasAcrossJobTest struct {
	name     string
	tor      *Tor
	isNSLBv2 bool
	jobName  api.JobID
	want     bool
}

func buildTorHasAcrossJobTestCase01() torHasAcrossJobTest {
	return torHasAcrossJobTest{
		name: "01 will return true when tor is IsUsedByMulJob",
		tor:  &Tor{Servers: []*Server{{IsUsedByMulJob: true}}},
		want: true,
	}
}

func buildTorHasAcrossJobTestCase02() torHasAcrossJobTest {
	return torHasAcrossJobTest{
		name: "02 will return false when tor is empty",
		tor:  &Tor{Servers: []*Server{{}}},
		want: false,
	}
}

func buildTorHasAcrossJobTestCase03() torHasAcrossJobTest {
	return torHasAcrossJobTest{
		name: "03 will return false when job name is meet",
		tor:  &Tor{Jobs: map[api.JobID]SchedulerJob{"test": {}}},
		want: false,
	}
}

func buildTorHasAcrossJobTestCase04() torHasAcrossJobTest {
	return torHasAcrossJobTest{
		name: "04 will return false when job status is not running",
		tor:  &Tor{Jobs: map[api.JobID]SchedulerJob{"test": fakeNormalJob()}},
		want: false,
	}
}

func buildTorHasAcrossJobTestCase05() torHasAcrossJobTest {
	fakeJob := fakeNormalJob()
	fakeJob.Status = util.PodGroupRunning
	return torHasAcrossJobTest{
		name: "05 will return true when job status is running",
		tor:  &Tor{Jobs: map[api.JobID]SchedulerJob{"test": fakeJob}},
		want: true,
	}
}

func buildTorHasAcrossJobTestCase06() torHasAcrossJobTest {
	fakeJob := fakeNormalJob()
	fakeJob.Status = util.PodGroupRunning
	return torHasAcrossJobTest{
		name: "06 will return false when job is across job and meet tor node",
		tor:  &Tor{Jobs: map[api.JobID]SchedulerJob{"test": fakeJob}, Servers: []*Server{{Name: "node1"}}},
		want: false,
	}
}

func buildTorHasAcrossJobTestCases() []torHasAcrossJobTest {
	return []torHasAcrossJobTest{
		buildTorHasAcrossJobTestCase01(),
		buildTorHasAcrossJobTestCase02(),
		buildTorHasAcrossJobTestCase03(),
		buildTorHasAcrossJobTestCase04(),
		buildTorHasAcrossJobTestCase05(),
		buildTorHasAcrossJobTestCase06(),
	}
}

func TestTorHasAcrossJob(t *testing.T) {
	for _, tt := range buildTorHasAcrossJobTestCases() {
		t.Run(tt.name, func(t1 *testing.T) {
			if got := tt.tor.HasAcrossJob(tt.isNSLBv2, tt.jobName); got != tt.want {
				t1.Errorf("HasAcrossJob() = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("tor nil test", func(t1 *testing.T) {
		var tor *Tor
		if got := tor.HasAcrossJob(false, ""); got != false {
			t1.Errorf("HasAcrossJob() = %v, want %v", got, false)
		}
	})
}

func TestSetTorFreeServerCountAndGetFullTor(t *testing.T) {
	tl := fakeNormalTorList(fakeEnableNodeNum, fakeJobId)
	const fullTorNum = 27
	t.Run("get full tor num test will return 27", func(t *testing.T) {
		if got := tl.SetTorFreeServerCountAndGetFullTor(fakeJobId); got != fullTorNum {
			t.Errorf("SetTorFreeServerCountAndGetFullTor() = %v, want %v", got, fullTorNum)
		}
	})
	var tl2 *TorList
	t.Run("tor nil test", func(t *testing.T) {
		if got := tl2.SetTorFreeServerCountAndGetFullTor(fakeJobId); got != 0 {
			t.Errorf("SetTorFreeServerCountAndGetFullTor() = %v, want %v", got, 0)
		}
	})
}

func TestTorListMarkTorListByJobV2(t *testing.T) {
	tl := fakeNormalTorList(fakeEnableNodeNum, fakeJobId)
	t.Run("mark tor list by job v2 node is not empty test", func(t *testing.T) {
		tl.MarkTorListByJobV2(map[string]*api.NodeInfo{"node": {Name: "node"}}, fakeJobId)
	})
	var tl2 *TorList
	t.Run("tor nil test", func(t *testing.T) {
		tl2.MarkTorListByJobV2(nil, fakeJobId)
	})
}

func TestTorListMarkTorListByJobV1(t *testing.T) {
	tl := fakeNormalTorList(fakeEnableNodeNum, fakeJobId)
	t.Run("mark tor list by job v1 node is not empty test", func(t *testing.T) {
		tl.MarkTorListByJobV1(map[string]*api.NodeInfo{"node": {Name: "node"}}, fakeJobId, fakeSliceNum)
	})
	var tl2 *TorList
	t.Run("tor nil test", func(t *testing.T) {
		tl2.MarkTorListByJobV1(nil, fakeJobId, 0)
	})
}

func TestTorISNil(t *testing.T) {
	var tl *TorList
	var tor *Tor
	t.Run("IsUsedByAcrossLargeModelJob tor nil test", func(t *testing.T) {
		if got := tor.IsUsedByAcrossLargeModelJob(); got != false {
			t.Errorf("IsUsedByAcrossLargeModelJob() = %v, want %v", got, false)
		}
	})
	t.Run("GetNSLBVersion tor nil test", func(t *testing.T) {
		if got := tl.GetNSLBVersion(); got != "" {
			t.Errorf("GetNSLBVersion() = %v, want %v", got, "")
		}
	})
	t.Run("GetSharedTorNum nil test", func(t *testing.T) {
		if got := tl.GetSharedTorNum(); got != util.ErrorInt {
			t.Errorf("GetSharedTorNum() = %v, want %v", got, util.ErrorInt)
		}
	})
	var torIpMap map[string]string
	t.Run("GetSharedTorNum nil test", func(t *testing.T) {
		if got := tl.GetTorIpMap(); !reflect.DeepEqual(got, torIpMap) {
			t.Errorf("GetTorIpMap() = %v, want %v", got, torIpMap)
		}
	})
	var serverMap map[string]*Server
	t.Run("GetServerMaps nil test", func(t *testing.T) {
		if got := tl.GetServerMaps(); !reflect.DeepEqual(got, serverMap) {
			t.Errorf("GetServerMaps() = %v, want %v", got, serverMap)
		}
	})
	var torMaps map[string]*Tor
	t.Run("GetTorMaps nil test", func(t *testing.T) {
		if got := tl.GetTorMaps(); !reflect.DeepEqual(got, torMaps) {
			t.Errorf("GetTorMaps() = %v, want %v", got, torMaps)
		}
	})
}
