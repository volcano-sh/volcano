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
Package plugin is using for HuaWei Ascend pin affinity schedule.
*/
package plugin

import (
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/k8s"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/test"
)

type fields struct {
	NPUPlugins  sets.String
	ScheduleEnv ScheduleEnv
}

type batchNodeOrderFnArgs struct {
	nodes []*api.NodeInfo
	ssn   *framework.Session
}

type batchNodeOrderFnTest struct {
	name    string
	args    batchNodeOrderFnArgs
	want    map[string]float64
	wantErr bool
}

// PatchGetCm go monkey patch get cm
func PatchGetCm(name, nameSpace string, data map[string]string) *gomonkey.Patches {
	return gomonkey.ApplyFunc(k8s.GetConfigMap, func(client kubernetes.Interface, namespace, cmName string) (
		*v1.ConfigMap, error) {
		return test.FakeConfigmap(name, nameSpace, data), nil
	})
}

func buildBatchNodeOrderFn01() batchNodeOrderFnTest {
	return batchNodeOrderFnTest{
		name:    "01-BatchNodeOrderFn nil Test",
		args:    batchNodeOrderFnArgs{nodes: nil, ssn: nil},
		wantErr: true,
	}
}

func buildBatchNodeOrderFn02() batchNodeOrderFnTest {
	tNodes := test.FakeNormalTestNodes(util.NPUIndex2)
	return batchNodeOrderFnTest{
		name:    "02-BatchNodeOrderFn ScoreBestNPUNodes ok Test",
		args:    batchNodeOrderFnArgs{nodes: tNodes, ssn: nil},
		wantErr: false,
	}
}

func buildBatchNodeOrderFn03() batchNodeOrderFnTest {
	ssn := test.FakeNormalSSN(nil)
	handler := newDefaultHandler()
	initNormalsHandlerBySsnFunc(ssn, handler.InitVolcanoFrameFromSsn, handler.InitNodesFromSsn, handler.InitJobsFromSsn)
	return batchNodeOrderFnTest{
		name:    "03-BatchNodeOrderFn ScoreBestNPUNodes score node ok test",
		args:    batchNodeOrderFnArgs{nodes: ssn.NodeList, ssn: ssn},
		wantErr: false,
	}
}

func buildBatchNodeOrderFn() []batchNodeOrderFnTest {
	return []batchNodeOrderFnTest{
		buildBatchNodeOrderFn01(),
		buildBatchNodeOrderFn02(),
		buildBatchNodeOrderFn03(),
	}
}

func TestBatchNodeOrderFn(t *testing.T) {
	tests := buildBatchNodeOrderFn()
	tTask := test.FakeNormalTestTasks(1)[0]
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handle := newDefaultHandler()
			patch1 := PatchGetCm(TorNodeCMName, "kube-system", test.FakeTorNodeData())
			defer patch1.Reset()
			initNormalsHandlerBySsnFunc(tt.args.ssn, handle.InitVolcanoFrameFromSsn, handle.InitNodesFromSsn,
				handle.InitJobsFromSsn, handle.InitTorNodeInfo)
			if strings.Contains(tt.name, SingleLayer) {
				handle.Tors.TorLevel = SingleLayer
			}
			_, err := handle.BatchNodeOrderFn(tTask, tt.args.nodes)
			if (err != nil) != tt.wantErr {
				t.Errorf("BatchNodeOrderFn() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

type beforeCloseHandlerTest struct {
	name   string
	fields fields
}

func buildBeforeCloseHandler() []beforeCloseHandlerTest {
	tests := []beforeCloseHandlerTest{
		{
			name: "01-BeforeCloseHandler no cache test",
			fields: fields{NPUPlugins: map[string]sets.Empty{},
				ScheduleEnv: ScheduleEnv{
					ClusterCache: NewClusterCache(),
					FrameAttr:    VolcanoFrame{}}},
		},
		{
			name: "02-BeforeCloseHandler save cache test",
			fields: fields{NPUPlugins: map[string]sets.Empty{},
				ScheduleEnv: ScheduleEnv{
					OutputCache: ScheduleCache{Names: map[string]string{"fault": "test"},
						Namespaces: map[string]string{"fault": "hahaNameSpace"},
						Data:       map[string]map[string]string{"fault": {"test1": "testData"}}}}},
		},
		{
			name: "03-BeforeCloseHandler save reset cm and tor infos",
			fields: fields{NPUPlugins: map[string]sets.Empty{},
				ScheduleEnv: newDefaultsHandlerByFakeSsn().ScheduleEnv},
		},
	}
	return tests
}

func TestBeforeCloseHandler(t *testing.T) {
	tests := buildBeforeCloseHandler()
	tmpPatche := gomonkey.ApplyFunc(k8s.CreateOrUpdateConfigMap,
		func(k8s kubernetes.Interface, cm *v1.ConfigMap, cmName, cmNameSpace string) error {
			return nil
		})
	tmpPatche2 := gomonkey.ApplyFunc(k8s.GetConfigMapWithRetry, func(
		_ kubernetes.Interface, _, _ string) (*v1.ConfigMap, error) {
		return test.FakeConfigmap(ResetInfoCMNamePrefix, "default", fakeResetCmInfos()), nil
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sHandle := &ScheduleHandler{
				NPUPlugins:  tt.fields.NPUPlugins,
				ScheduleEnv: tt.fields.ScheduleEnv,
			}
			sHandle.BeforeCloseHandler()
		})
	}
	tmpPatche.Reset()
	tmpPatche2.Reset()
}

type initNPUSessionArgs struct {
	ssn *framework.Session
}

type initNPUSessionTest struct {
	name     string
	sHandler *ScheduleHandler
	args     initNPUSessionArgs
	wantErr  bool
}

func buildInitNPUSessionTest() []initNPUSessionTest {
	tests := []initNPUSessionTest{
		{
			name:     "01-InitNPUSession nil ssn test",
			sHandler: &ScheduleHandler{},
			args:     initNPUSessionArgs{ssn: nil},
			wantErr:  true,
		},
		{
			name:     "02-InitNPUSession success test",
			sHandler: newDefaultHandler(),
			args:     initNPUSessionArgs{ssn: test.FakeNormalSSN(test.FakeConfigurations())},
			wantErr:  false,
		},
	}
	return tests
}

func TestInitNPUSession(t *testing.T) {
	tests := buildInitNPUSessionTest()
	patch1 := PatchGetCm(TorNodeCMName, "kube-system", test.FakeTorNodeData())
	defer patch1.Reset()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.sHandler.InitNPUSession(tt.args.ssn); (err != nil) != tt.wantErr {
				t.Errorf("InitNPUSession() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

type preStartPluginArgs struct {
	ssn *framework.Session
}

type preStartPluginTest struct {
	name   string
	fields fields
	args   preStartPluginArgs
}

func buildPreStartPluginTest() []preStartPluginTest {
	tests := []preStartPluginTest{
		{
			name:   "01-PreStartPlugin ok test",
			fields: fields{NPUPlugins: nil},
			args:   preStartPluginArgs{ssn: nil},
		},
	}
	return tests
}

func TestScheduleHandlerPreStartPlugin(t *testing.T) {
	tests := buildPreStartPluginTest()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sHandle := &ScheduleHandler{
				NPUPlugins:  tt.fields.NPUPlugins,
				ScheduleEnv: tt.fields.ScheduleEnv,
			}
			sHandle.preStartPlugin(tt.args.ssn)
		})
	}
}

type initVolcanoFrameFromSsnTestCase struct {
	name    string
	configs []conf.Configuration
	want    VolcanoFrame
}

func buildInitVolcanoFrameFromSsnTestCases() []initVolcanoFrameFromSsnTestCase {
	superPodSizeKey := "super-pod-size"
	reserveNodesKey := "reserve-nodes"
	var testCases []initVolcanoFrameFromSsnTestCase
	testCases = append(testCases,
		getDefaultVolcanoFrameCasesOfSuperPodSizeFormatError(superPodSizeKey, reserveNodesKey)...)
	testCases = append(testCases,
		getDefaultVolcanoFrameCasesOfSuperPodSizeValueError(superPodSizeKey, reserveNodesKey)...)
	testCases = append(testCases,
		getDefaultVolcanoFrameCasesOfReserveNodesSelfValueError(superPodSizeKey, reserveNodesKey)...)
	testCases = append(testCases,
		getDefaultVolcanoFrameCasesOfReserveNodesValueMoreError(superPodSizeKey, reserveNodesKey)...)
	return testCases
}

func getDefaultVolcanoFrameCasesOfReserveNodesSelfValueError(superPodSizeKey,
	reserveNodesKey string) []initVolcanoFrameFromSsnTestCase {
	return []initVolcanoFrameFromSsnTestCase{
		{
			name: "05-GetReserveNodes failed, set default reserve-nodes: 2",
			configs: []conf.Configuration{
				{
					Name: util.CMInitParamKey,
					Arguments: map[string]interface{}{
						superPodSizeKey: "40",
					},
				},
			},
			want: VolcanoFrame{
				ConfigParameters: ConfigParameters{DynamicParameters: DynamicParameters{
					SuperPodSize:   40,
					ReservePodSize: 2,
				}}},
		},
		{
			name: "06-GetReserveNodes failed, set default reserve-nodes: 2",
			configs: []conf.Configuration{
				{
					Name: util.CMInitParamKey,
					Arguments: map[string]interface{}{
						superPodSizeKey: "40",
						reserveNodesKey: "-1",
					},
				},
			},
			want: VolcanoFrame{
				ConfigParameters: ConfigParameters{DynamicParameters: DynamicParameters{
					SuperPodSize:   40,
					ReservePodSize: 2,
				}}},
		},
	}
}

func getDefaultVolcanoFrameCasesOfReserveNodesValueMoreError(superPodSizeKey,
	reserveNodesKey string) []initVolcanoFrameFromSsnTestCase {
	return []initVolcanoFrameFromSsnTestCase{
		{
			name: "07-reserve-nodes is bigger than super-pod-size, set default reserve-nodes: 2",
			configs: []conf.Configuration{
				{
					Name: util.CMInitParamKey,
					Arguments: map[string]interface{}{
						superPodSizeKey: "8",
						reserveNodesKey: "10",
					},
				},
			},
			want: VolcanoFrame{
				ConfigParameters: ConfigParameters{DynamicParameters: DynamicParameters{
					SuperPodSize:   8,
					ReservePodSize: 2,
				}}},
		},
		{
			name: "08-reserve-nodes is bigger than super-pod-size, set default reserve-nodes: 1",
			configs: []conf.Configuration{
				{
					Name: util.CMInitParamKey,
					Arguments: map[string]interface{}{
						superPodSizeKey: "2",
						reserveNodesKey: "90",
					},
				},
			},
			want: VolcanoFrame{
				ConfigParameters: ConfigParameters{DynamicParameters: DynamicParameters{
					SuperPodSize:   2,
					ReservePodSize: 0,
				}}},
		},
	}
}

func getDefaultVolcanoFrameCasesOfSuperPodSizeFormatError(superPodSizeKey,
	reserveNodesKey string) []initVolcanoFrameFromSsnTestCase {
	return []initVolcanoFrameFromSsnTestCase{
		{
			name: "01-GetSizeOfSuperPod and GetReserveNodes failed, set default super-pod-size: 48, " +
				"default reserve-nodes: 2",
			configs: []conf.Configuration{
				{
					Name:      util.CMInitParamKey,
					Arguments: map[string]interface{}{},
				},
			},
			want: VolcanoFrame{
				ConfigParameters: ConfigParameters{DynamicParameters: DynamicParameters{

					SuperPodSize:   defaultSuperPodSize,
					ReservePodSize: defaultReserveNodes,
				}}},
		},
		{
			name: "02-GetSizeOfSuperPod failed, set default super-pod-size: 48",
			configs: []conf.Configuration{
				{
					Name: util.CMInitParamKey,
					Arguments: map[string]interface{}{
						superPodSizeKey: "****",
						reserveNodesKey: "2",
					},
				},
			},
			want: VolcanoFrame{
				ConfigParameters: ConfigParameters{DynamicParameters: DynamicParameters{
					SuperPodSize:   defaultSuperPodSize,
					ReservePodSize: defaultReserveNodes,
				}}},
		},
	}
}

func getDefaultVolcanoFrameCasesOfSuperPodSizeValueError(superPodSizeKey,
	reserveNodesKey string) []initVolcanoFrameFromSsnTestCase {
	return []initVolcanoFrameFromSsnTestCase{
		{
			name: "03-GetSizeOfSuperPod failed, set default super-pod-size: 48",
			configs: []conf.Configuration{
				{
					Name: util.CMInitParamKey,
					Arguments: map[string]interface{}{
						superPodSizeKey: "-1",
						reserveNodesKey: "3",
					},
				},
			},
			want: VolcanoFrame{
				ConfigParameters: ConfigParameters{DynamicParameters: DynamicParameters{
					SuperPodSize:   defaultSuperPodSize,
					ReservePodSize: 3,
				}}},
		},
		{
			name: "04-GetSizeOfSuperPod failed, set default super-pod-size: 48",
			configs: []conf.Configuration{
				{
					Name: util.CMInitParamKey,
					Arguments: map[string]interface{}{
						superPodSizeKey: "0",
						reserveNodesKey: "4",
					},
				},
			},
			want: VolcanoFrame{
				ConfigParameters: ConfigParameters{DynamicParameters: DynamicParameters{
					SuperPodSize:   defaultSuperPodSize,
					ReservePodSize: 4,
				}}},
		},
	}
}

func TestInitVolcanoFrameFromSsn(t *testing.T) {
	ssn := &framework.Session{}
	sHandle := newDefaultHandler()
	for _, tt := range buildInitVolcanoFrameFromSsnTestCases() {
		t.Run(tt.name, func(t *testing.T) {
			ssn.Configurations = tt.configs
			sHandle.InitVolcanoFrameFromSsn(ssn)
			if !reflect.DeepEqual(sHandle.FrameAttr.SuperPodSize, tt.want.SuperPodSize) {
				t.Errorf("InitVolcanoFrameFromSsn() = %v, want %v", sHandle.FrameAttr.SuperPodSize, tt.want.SuperPodSize)
			}
			if !reflect.DeepEqual(sHandle.FrameAttr.ReservePodSize, tt.want.ReservePodSize) {
				t.Errorf("InitVolcanoFrameFromSsn() = %v, want %v", sHandle.FrameAttr.ReservePodSize, tt.want.ReservePodSize)
			}
		})
	}
}

// TestGetPodGroupOwnerRef test of getPodGroupOwnerRef
func TestGetPodGroupOwnerRef(t *testing.T) {
	t.Run("pg without ownerRef", func(t *testing.T) {
		pg := scheduling.PodGroup{}
		expectedOwner := metav1.OwnerReference{}
		owner := getPodGroupOwnerRef(pg)
		if !reflect.DeepEqual(expectedOwner, owner) {
			t.Errorf("getPodGroupOwnerRef = %v, want %v", owner, expectedOwner)
		}
	})
	t.Run("pg with ownerRef", func(t *testing.T) {
		controller := true
		pg := scheduling.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				OwnerReferences: []metav1.OwnerReference{
					{
						Controller: &controller,
					},
				},
			},
		}
		expectedOwner := metav1.OwnerReference{
			Controller: &controller,
		}
		owner := getPodGroupOwnerRef(pg)
		if !reflect.DeepEqual(expectedOwner, owner) {
			t.Errorf("getPodGroupOwnerRef = %v, want %v", owner, expectedOwner)
		}
	})
}

// HandlerStart HuaWei NPU plugin start by frame.
func newDefaultHandler() *ScheduleHandler {
	scheduleHandler := &ScheduleHandler{
		NPUPlugins: sets.String{},
		ScheduleEnv: ScheduleEnv{
			ClusterCache:            NewClusterCache(),
			FrameAttr:               NewVolcanoFrame(),
			JobScheduleInfoRecorder: NewJobScheduleInfoRecorder(),
		},
	}

	scheduleHandler.FrameAttr.OnceInit = &sync.Once{}
	scheduleHandler.PolicyBuilder = func() SchedulerPluginNeed {
		return New(util.NPU910CardName)
	}
	return scheduleHandler
}

func newDefaultsHandlerByFakeSsn() *ScheduleHandler {
	patch1 := PatchGetCm(TorNodeCMName, "kube-system", test.FakeTorNodeData())
	defer patch1.Reset()
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	fakeJob := test.FakeJobInfoByName("pg0", util.NPUIndex8)
	test.AddJobInfoLabel(fakeJob, TorAffinityKey, NormalSchema)
	test.AddJobInfoLabel(fakeJob, util.SinglePodTag, util.EnableFunc)
	test.AddJobInfoIntoSsn(ssn, fakeJob)
	handle := newDefaultHandler()
	initNormalsHandlerBySsnFunc(ssn, handle.InitVolcanoFrameFromSsn, handle.InitNodesFromSsn,
		handle.InitJobsFromSsn, handle.InitTorNodeInfo)
	handle.FrameAttr.KubeClient = fake.NewSimpleClientset()
	return handle
}

func initNormalsHandlerBySsnFunc(ssn *framework.Session, initSsnFunc ...func(ssn *framework.Session)) {
	if ssn == nil {
		return
	}
	for _, initFunc := range initSsnFunc {
		initFunc(ssn)
	}
}

func initNormalsHandlerByNormalFunc(initFuncs ...func()) {
	for _, initFunc := range initFuncs {
		initFunc()
	}
}

func deleteNodeByNodeName(nodes []*api.NodeInfo, nodeName string) []*api.NodeInfo {
	tmpNodes := make([]*api.NodeInfo, 0)
	for _, node := range nodes {
		if node.Name == nodeName {
			continue
		}
		tmpNodes = append(tmpNodes, node)
	}
	return tmpNodes
}

type initCmInformerTest struct {
	name    string
	ssn     *framework.Session
	sHandle *ScheduleHandler
}

func buildInitCmInformerTest01() initCmInformerTest {
	return initCmInformerTest{
		name:    "01-initCmInformerTest will return when kube client is nil",
		sHandle: &ScheduleHandler{},
	}
}

func buildInitCmInformerTest02() initCmInformerTest {
	ssn := test.FakeNormalSSN(test.FakeConfigurations())
	sHandler := newDefaultHandler()
	initNormalsHandlerBySsnFunc(ssn, sHandler.InitVolcanoFrameFromSsn)
	sHandler.FrameAttr.KubeClient = fake.NewSimpleClientset()
	return initCmInformerTest{
		name:    "02-initCmInformerTest will init cm by clusterd cm when conf is used cluster info manager",
		sHandle: sHandler,
	}
}

func buildInitCmInformerTest03() initCmInformerTest {
	tmpConf := test.FakeConfigurations()
	tmpConf[0].Arguments[util.UseClusterInfoManager] = "false"
	ssn := test.FakeNormalSSN(tmpConf)
	sHandler := newDefaultHandler()
	initNormalsHandlerBySsnFunc(ssn, sHandler.InitVolcanoFrameFromSsn)
	sHandler.FrameAttr.KubeClient = fake.NewSimpleClientset()
	return initCmInformerTest{
		name:    "03-initCmInformerTest will init cm by device info cm when conf is not use cluster info manager",
		sHandle: sHandler,
	}
}

func buildInitCmInformerTestCases() []initCmInformerTest {
	return []initCmInformerTest{
		buildInitCmInformerTest01(),
		buildInitCmInformerTest02(),
		buildInitCmInformerTest03(),
	}
}

func TestScheduleHandlerInitCmInformer(t *testing.T) {
	tests := buildInitCmInformerTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.sHandle.initCmInformer()
		})
	}
}

func fakeDeploymentJobInfo() *api.JobInfo {
	trueTag := true
	fakeJob := &api.JobInfo{
		Name:      "fakeJob",
		Namespace: "default",
		PodGroup:  &api.PodGroup{},
	}
	fakeJob.PodGroup.OwnerReferences = []metav1.OwnerReference{
		{
			Kind:       ReplicaSetType,
			Controller: &trueTag,
			Name:       "fakePG",
		},
	}
	return fakeJob
}

func fakeInformerFactory() informers.SharedInformerFactory {
	return informers.NewSharedInformerFactory(fake.NewSimpleClientset(), 0)
}

type getOwnerInfoTest struct {
	name    string
	vf      VolcanoFrame
	jobInfo *api.JobInfo
	wantErr bool
}

func buildGetOwnerInfoTest() []getOwnerInfoTest {
	return []getOwnerInfoTest{
		{
			name:    "01 will return nil when job is not deployment",
			vf:      VolcanoFrame{},
			jobInfo: &api.JobInfo{PodGroup: &api.PodGroup{}},
			wantErr: false,
		},
		{
			name:    "02 will return err when job is not exist",
			vf:      VolcanoFrame{KubeClient: fake.NewSimpleClientset(), informerFactory: fakeInformerFactory()},
			jobInfo: fakeDeploymentJobInfo(),
			wantErr: true,
		},
	}
}

func TestGetOwnerInfo(t *testing.T) {
	for _, tt := range buildGetOwnerInfoTest() {
		t.Run(tt.name, func(t *testing.T) {
			_, err := getOwnerInfo(tt.jobInfo, tt.vf)
			if (err != nil) != tt.wantErr {
				t.Errorf("getOwnerInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}

type getGraceDeleteTimeTest struct {
	name string
	conf map[string]string
	want int64
}

func buildGetGraceDeleteTimeTest() []getGraceDeleteTimeTest {
	return []getGraceDeleteTimeTest{
		{
			name: "01 will return default time when value is not int64",
			conf: map[string]string{GraceOverTimeKey: "test"},
			want: DefaultGraceOverTime,
		},
		{
			name: "01 will return default time when value is lower then 0",
			conf: map[string]string{GraceOverTimeKey: "1"},
			want: DefaultGraceOverTime,
		},
	}
}

func TestGetGraceDeleteTime(t *testing.T) {
	for _, tt := range buildGetGraceDeleteTimeTest() {
		t.Run(tt.name, func(t *testing.T) {
			if got := getGraceDeleteTime(tt.conf); got != tt.want {
				t.Errorf("getGraceDeleteTime() = %v, want %v", got, tt.want)
			}
		})
	}
}
