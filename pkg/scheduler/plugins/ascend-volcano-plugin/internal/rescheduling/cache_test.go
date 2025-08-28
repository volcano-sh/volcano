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
Package rescheduling is using for HuaWei Ascend pin fault rescheduling.
*/
package rescheduling

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes"
	"volcano.sh/volcano/pkg/scheduler/api"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

const (
	validLengthOfInfo = 2000
	maxGenRecordLoop  = 100
	perLoopJobNum     = 50
)

func dealMarshal(data interface{}) string {
	dataString, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(dataString)
}

type ReSchedulerCacheWriteReSchedulerCacheToEnvCacheFields struct {
	FaultNodes map[string]*FaultNode
	FaultJobs  map[api.JobID]*FaultJob
}

type ReSchedulerCacheWriteReSchedulerCacheToEnvCacheArgs struct {
	env     *plugin.ScheduleEnv
	jobType string
}

type ReSchedulerCacheWriteReSchedulerCacheToEnvCacheTests struct {
	name    string
	fields  ReSchedulerCacheWriteReSchedulerCacheToEnvCacheFields
	args    ReSchedulerCacheWriteReSchedulerCacheToEnvCacheArgs
	wantErr bool
}

func buildReSchedulerCacheWriteReSchedulerCacheToEnvCache() []ReSchedulerCacheWriteReSchedulerCacheToEnvCacheTests {
	test1 := ReSchedulerCacheWriteReSchedulerCacheToEnvCacheTests{
		name: "01-ReSchedulerCache_WriteReSchedulerCacheToEnvCache()-nothing to write",
		fields: ReSchedulerCacheWriteReSchedulerCacheToEnvCacheFields{
			FaultNodes: map[string]*FaultNode{},
			FaultJobs:  map[api.JobID]*FaultJob{},
		},
		args: ReSchedulerCacheWriteReSchedulerCacheToEnvCacheArgs{
			env: &plugin.ScheduleEnv{
				OutputCache: plugin.ScheduleCache{
					Names:      map[string]string{RePropertyName: CmName},
					Namespaces: map[string]string{RePropertyName: CmNameSpace},
					Data:       map[string]map[string]string{RePropertyName: make(map[string]string, util.MapInitNum)},
				},
			},
			jobType: CmFaultJob910x8Kind,
		},
		wantErr: false,
	}
	faultJob := fakeTestFaultJob([]string{"node0"}, []string{"0", "1"}, nil, "job0", "vcjob")
	test2 := ReSchedulerCacheWriteReSchedulerCacheToEnvCacheTests{
		name: "02-ReSchedulerCache_WriteReSchedulerCacheToEnvCache()-with faultJob",
		fields: ReSchedulerCacheWriteReSchedulerCacheToEnvCacheFields{
			FaultNodes: map[string]*FaultNode{},
			FaultJobs:  map[api.JobID]*FaultJob{faultJob.JobUID: faultJob},
		},
		args: ReSchedulerCacheWriteReSchedulerCacheToEnvCacheArgs{
			env: &plugin.ScheduleEnv{
				OutputCache: plugin.ScheduleCache{
					Names:      map[string]string{RePropertyName: CmName},
					Namespaces: map[string]string{RePropertyName: CmNameSpace},
					Data: map[string]map[string]string{RePropertyName: make(map[string]string, util.MapInitNum),
						JobRecovery: make(map[string]string, util.MapInitNum)},
				},
			},
			jobType: CmFaultJob910x8Kind,
		},
		wantErr: false,
	}
	tests := []ReSchedulerCacheWriteReSchedulerCacheToEnvCacheTests{test1, test2}
	return tests
}

// TestDealReSchedulerCacheWriteReSchedulerCacheToEnvCache test for re-scheduler writing
func TestDealReSchedulerCacheWriteReSchedulerCacheToEnvCache(t *testing.T) {
	tests := buildReSchedulerCacheWriteReSchedulerCacheToEnvCache()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reCache := &DealReSchedulerCache{
				FaultNodes: tt.fields.FaultNodes,
				FaultJobs:  tt.fields.FaultJobs,
			}
			if err := reCache.WriteReSchedulerCacheToEnvCache(
				tt.args.env, tt.args.jobType); (err != nil) != tt.wantErr {
				t.Errorf("WriteReSchedulerCacheToEnvCache() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func buildTestCaseForMaxLengthOfRescheduleReason() ReSchedulerCacheWriteReSchedulerCacheToEnvCacheTests {
	test1 := ReSchedulerCacheWriteReSchedulerCacheToEnvCacheTests{
		name: "01-ReSchedulerCache_WriteReSchedulerCacheToEnvCache()-max reschedule reason to write",
		fields: ReSchedulerCacheWriteReSchedulerCacheToEnvCacheFields{
			FaultNodes: map[string]*FaultNode{},
			FaultJobs:  map[api.JobID]*FaultJob{},
		},
		args: ReSchedulerCacheWriteReSchedulerCacheToEnvCacheArgs{
			env: &plugin.ScheduleEnv{
				OutputCache: plugin.ScheduleCache{
					Names:      map[string]string{ReschedulingReasonKey: RescheduleReasonCmName},
					Namespaces: map[string]string{ReschedulingReasonKey: RescheduleReasonCmNamespace},
					Data: map[string]map[string]string{ReschedulingReasonKey: make(map[string]string,
						util.MapInitNum)},
				},
			},
			jobType: CmFaultJob910x8Kind,
		},
		wantErr: false,
	}
	return test1
}

func gen950KbRecords() map[api.JobID]*RescheduleReason {
	records := make(map[api.JobID]*RescheduleReason, util.MapInitNum)
	singleRecord := RescheduleRecord{
		LogFileFormatTime:   time.Now().Format("I0102 15:04:05"),
		RescheduleTimeStamp: time.Now().Unix(),
		ReasonOfTask: []RescheduleTaskReason{{
			RescheduleReason: "pod-failed",
			PodName:          "scheduler-0",
			NodeName:         "node0",
			NodeRankIndex:    "0",
		}},
	}

	length, maxLoop := 0, maxGenRecordLoop
	for i := 0; length < MaxKbOfRescheduleRecords && i < maxLoop; i++ {
		// every 50 job, marshal once to judge length
		for j := 0; j < perLoopJobNum; j++ {
			id := uuid.NewUUID()
			jobId := api.JobID(id)
			singleJobWith10Record := RescheduleReason{
				JobID:                jobId,
				TotalRescheduleTimes: 0,
				RescheduleRecords: []RescheduleRecord{
					singleRecord, singleRecord, singleRecord, singleRecord, singleRecord,
					singleRecord, singleRecord, singleRecord, singleRecord, singleRecord,
				},
				AdditionalInfo: "",
			}
			records[jobId] = &singleJobWith10Record
		}
		bytes, err := json.Marshal(records)
		if err != nil {
			fmt.Printf("failed to marshal, err: %s", err.Error())
			continue
		}
		length = len(bytes)
	}
	return records
}

func TestMaxLengthOfRescheduleReason(t *testing.T) {
	test := buildTestCaseForMaxLengthOfRescheduleReason()
	records := gen950KbRecords()
	reCache := &DealReSchedulerCache{
		FaultNodes:                 test.fields.FaultNodes,
		FaultJobs:                  test.fields.FaultJobs,
		JobRecentRescheduleRecords: records,
	}
	if len(records) == 0 {
		t.Error("failed to generate records")
	}
	result, err := reCache.writeRescheduleReasonsToCMString()
	if (err != nil) != test.wantErr {
		t.Errorf("writeRescheduleReasonsToCMString() error = %v, wantErr %v", err, test.wantErr)
	}
	if len(result) > MaxKbOfRescheduleRecords {
		t.Error("failed to reduce rescheduling reason length")
	}
	// directly show the result contain additional info
	if len(result) > validLengthOfInfo {
		fmt.Printf("%s", result[:validLengthOfInfo])
	}
}

func initDealReSchedulerCache() *DealReSchedulerCache {
	fields := fakeReSchedulerCache()
	return &DealReSchedulerCache{
		FaultNodes: fields.FaultNodes,
		FaultJobs:  fields.FaultJobs,
	}
}

func TestGetRecentReschedulingRecordsFromCm(t *testing.T) {
	reCache := initDealReSchedulerCache()
	t.Run("01-getRecentReschedulingRecordsFromCm return error when json unmarshal failed",
		func(t *testing.T) {
			patch := gomonkey.ApplyFunc(json.Unmarshal, func(data []byte, v any) error {
				return errors.New("json unmarshal failed")
			})
			defer patch.Reset()
			if _, err := reCache.getRecentReschedulingRecordsFromCm(""); err == nil {
				t.Errorf("getRecentReschedulingRecordsFromCm() error = %v, wantErr is not nil", err)
			}
		})
	t.Run("02-getRecentReschedulingRecordsFromCm return nil when json unmarshal success",
		func(t *testing.T) {
			patch := gomonkey.ApplyFunc(json.Unmarshal, func(data []byte, v any) error {
				return nil
			})
			defer patch.Reset()
			if _, err := reCache.getRecentReschedulingRecordsFromCm(""); err != nil {
				t.Errorf("getRecentReschedulingRecordsFromCm() error = %v, wantErr is nil", err)
			}
		})
}

type dealReSchedulerCacheTestCase struct {
	name             string
	reCache          *DealReSchedulerCache
	firstStartup     bool
	wantErr          bool
	mockGetConfigMap func(kubernetes.Interface, string, string) (*v1.ConfigMap, error)
}

func buildSetJobRecentRescheduleRecordsTestCases() []dealReSchedulerCacheTestCase {
	return []dealReSchedulerCacheTestCase{
		{
			name:    "01-SetJobRecentRescheduleRecords return error when reCache is nil",
			reCache: nil, wantErr: true,
			mockGetConfigMap: func(_ kubernetes.Interface, _, _ string) (*v1.ConfigMap, error) {
				return nil, nil
			},
		},
		{
			name:    "02-SetJobRecentRescheduleRecords return error when GetConfigMap failed",
			reCache: initDealReSchedulerCache(), firstStartup: true, wantErr: true,
			mockGetConfigMap: func(_ kubernetes.Interface, _, _ string) (*v1.ConfigMap, error) {
				return nil, errors.New("get config map failed")
			},
		},
		{
			name:    "03-SetJobRecentRescheduleRecords return err when CMData is empty",
			reCache: initDealReSchedulerCache(), firstStartup: true, wantErr: true,
			mockGetConfigMap: func(_ kubernetes.Interface, _, _ string) (*v1.ConfigMap, error) {
				return &v1.ConfigMap{Data: map[string]string{CmJobRescheduleReasonsKey: ""}}, nil
			},
		},
		{
			name:    "04-SetJobRecentRescheduleRecords return error when getRecentReschedulingRecordsFromCm failed",
			reCache: initDealReSchedulerCache(), firstStartup: true, wantErr: true,
			mockGetConfigMap: func(_ kubernetes.Interface, _, _ string) (*v1.ConfigMap, error) {
				return &v1.ConfigMap{Data: map[string]string{CmJobRescheduleReasonsKey: "testCMData"}}, nil
			},
		},
		{
			name:    "05-SetJobRecentRescheduleRecords return nil when getRecentReschedulingRecordsFromCm success",
			reCache: initDealReSchedulerCache(), firstStartup: true, wantErr: false,
			mockGetConfigMap: func(_ kubernetes.Interface, _, _ string) (*v1.ConfigMap, error) {
				cmValue := dealMarshal(map[api.JobID]*RescheduleReason{})
				return &v1.ConfigMap{Data: map[string]string{CmJobRescheduleReasonsKey: cmValue}}, nil
			},
		},
	}
}

func TestSetJobRecentRescheduleRecords(t *testing.T) {
	testCases := buildSetJobRecentRescheduleRecordsTestCases()
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			patch := gomonkey.ApplyFunc(k8s.GetConfigMap, tt.mockGetConfigMap)
			defer patch.Reset()

			err := tt.reCache.SetJobRecentRescheduleRecords(&tt.firstStartup, nil)

			if (err != nil) != tt.wantErr {
				t.Errorf("SetJobRecentRescheduleRecords() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMarshalCacheDataToString(t *testing.T) {
	t.Run("01-marshalCacheDataToString return error when reCache is nil", func(t *testing.T) {
		patch := gomonkey.ApplyFunc(json.Marshal, func(interface{}) ([]byte, error) {
			return nil, errors.New("marshal failed")
		})
		defer patch.Reset()
		reCache := initDealReSchedulerCache()
		if _, err := reCache.marshalCacheDataToString(nil); err == nil {
			t.Errorf("marshalCacheDataToString() error = %v, wantErr is not nil", err)
		}
	})
}

func TestWriteFaultNodesToCMString(t *testing.T) {
	reCache := initDealReSchedulerCache()
	t.Run("01-writeFaultNodesToCMString return error when marshal failed",
		func(t *testing.T) {
			patch := gomonkey.ApplyFunc(json.Marshal, func(interface{}) ([]byte, error) {
				return nil, errors.New("marshal failed")
			})
			defer patch.Reset()
			if _, err := reCache.writeFaultNodesToCMString(); err == nil {
				t.Errorf("writeFaultNodesToCMString() error = %v, wantErr is not nil", err)
			}
		})
	t.Run("02-writeFaultNodesToCMString return error when marshal success",
		func(t *testing.T) {
			if _, err := reCache.writeFaultNodesToCMString(); err != nil {
				t.Errorf("writeFaultNodesToCMString() error = %v, wantErr is nil", err)
			}
		})
}

func TestGetFaultNodeToCm(t *testing.T) {
	t.Run("01-getFaultNodeToCm return error when marshal failed",
		func(t *testing.T) {
			realFaultNode := map[string]*FaultNode{
				"node0": {
					NodeName:            "node0",
					UpdateTime:          fakeTime,
					UnhealthyNPU:        []string{"Ascend910-0"},
					NetworkUnhealthyNPU: nil,
					IsFaultNode:         true,
					NodeDEnable:         true,
					NodeHealthState:     NodeCardUnhealthy,
					FaultCards: []FaultCard{
						*fakeTestFaultCardUnhealthy("Ascend910-0", "node0", NodeCardUnhealthy),
						*fakeTestFaultCardHealthy("Ascend910-1", "node0"),
						*fakeTestFaultCardHealthy("Ascend910-2", "node0"),
						*fakeTestFaultCardHealthy("Ascend910-3", "node0"),
						*fakeTestFaultCardHealthy("Ascend910-4", "node0"),
						*fakeTestFaultCardHealthy("Ascend910-5", "node0"),
						*fakeTestFaultCardHealthy("Ascend910-6", "node0"),
						*fakeTestFaultCardHealthy("Ascend910-7", "node0"),
					},
				},
			}
			if res := getFaultNodeToCm(realFaultNode); res == nil {
				t.Errorf("getFaultNodeToCm() res = %v, wantRes is not nil", res)
			}
		})
}

func TestGetRemainTimesToCMString(t *testing.T) {
	reCache := initDealReSchedulerCache()
	reCache.JobRemainRetryTimes = make(map[api.JobID]*RemainRetryTimes)
	reCache.JobRemainRetryTimes["testUid"] = &RemainRetryTimes{
		UUID:  "testUuid",
		Times: 0,
	}
	t.Run("01-getRemainTimesToCMString return error when marshal failed",
		func(t *testing.T) {
			patch := gomonkey.ApplyFunc(json.Marshal, func(interface{}) ([]byte, error) {
				return nil, errors.New("marshal failed")
			})
			defer patch.Reset()
			if _, err := reCache.getRemainTimesToCMString(); err == nil {
				t.Errorf("getRemainTimesToCMString() error = %v, wantErr is not nil", err)
			}
		})
	t.Run("01-getRemainTimesToCMString return error when marshal success",
		func(t *testing.T) {
			patch := gomonkey.ApplyFunc(json.Marshal, func(interface{}) ([]byte, error) {
				return nil, nil
			})
			defer patch.Reset()
			if _, err := reCache.getRemainTimesToCMString(); err != nil {
				t.Errorf("getRemainTimesToCMString() error = %v, wantErr is nil", err)
			}
		})
}

func TestWriteRescheduleReasonsToCMString(t *testing.T) {
	reCache := initDealReSchedulerCache()
	reCache.JobRecentRescheduleRecords = make(map[api.JobID]*RescheduleReason)
	reCache.JobRecentRescheduleRecords["testUid"] = &RescheduleReason{
		JobID:                "jobId",
		TotalRescheduleTimes: 0,
		RescheduleRecords:    []RescheduleRecord{},
		AdditionalInfo:       "additionalInfo",
	}
	t.Run("01-writeRescheduleReasonsToCMString return error when marshal failed",
		func(t *testing.T) {
			patch := gomonkey.ApplyFunc(json.Marshal, func(interface{}) ([]byte, error) {
				return nil, errors.New("marshal failed")
			})
			defer patch.Reset()
			if _, err := reCache.writeRescheduleReasonsToCMString(); err == nil {
				t.Errorf("writeRescheduleReasonsToCMString() error = %v, wantErr is not nil", err)
			}
		})
}

// TestJudgePublicFaultInReason pass when public fault in reason
func TestJudgePublicFaultInReason(t *testing.T) {
	t.Run("TestJudgePublicFaultInReason", func(t *testing.T) {
		faultTask := miniFaultTask{
			Reason: []FaultReasonList{
				{
					FaultDeviceList: FaultDeviceList{
						FaultType: PublicFaultType,
					},
				}, {
					FaultDeviceList: FaultDeviceList{
						FaultType: CardUnhealthy,
					},
				},
			},
		}
		if !judgePublicFaultInReason(&faultTask) {
			t.Error("TestJudgePublicFaultInReason failed")
		}
	})
}
