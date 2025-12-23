package scheduler

import (
	"flag"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"k8s.io/utils/set"

	schedulingv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/inspector/mock-actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/gang"
	"volcano.sh/volcano/pkg/scheduler/plugins/predicates"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

type dryRunTest struct {
	Name string
	uthelper.TestCommonStruct

	dryrun *DryrunRequest

	allocateNode set.Set[string]
	err          bool
}

func TestExecute(t *testing.T) {
	plugins := map[string]framework.PluginBuilder{
		predicates.PluginName: predicates.New,
		gang.PluginName:       gang.New,
	}
	options.Default()

	defer klog.Flush()
	flagSet := flag.NewFlagSet("test", flag.ExitOnError)
	klog.InitFlags(flagSet)
	// _ = flagSet.Parse([]string{"--v", "6"})

	trueValue := true
	tiers := []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:                gang.PluginName,
					EnabledJobOrder:     &trueValue,
					EnabledJobReady:     &trueValue,
					EnabledJobPipelined: &trueValue,
					EnabledJobStarving:  &trueValue,
				},

				{
					Name:             predicates.PluginName,
					EnabledPredicate: &trueValue,
				},
			},
		},
	}

	// nodes
	n1 := util.BuildNode("n1", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})
	n2 := util.BuildNode("n2", api.BuildResourceList("2", "4Gi", []api.ScalarResource{{Name: "pods", Value: "10"}}...), map[string]string{})
	// resources for test case 0

	// podgroup
	queue1 := util.BuildQueueWithResourcesQuantity("q1", nil, nil)

	tests := []dryRunTest{
		{
			Name:         "case0: dryrun for pod 1C1Gi had node to allocate",
			allocateNode: set.New("n1"),
			err:          false,
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: plugins,
				Nodes:   []*v1.Node{n1},
				Queues:  []*schedulingv1.Queue{queue1},
			},
			dryrun: &DryrunRequest{
				UUID:      `case0`,
				Namespace: "default",
				Queue:     "q1",
				Tasks: []TaskTemplate{
					{
						Replicas: 1,
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"cpu":    resource.MustParse("1"),
											"memory": resource.MustParse("1Gi"),
										},
										Limits: v1.ResourceList{
											"cpu":    resource.MustParse("1"),
											"memory": resource.MustParse("1Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			Name: "case1: dryrun for pod 10C1Gi no node to allocate",

			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: plugins,
				Nodes:   []*v1.Node{n1},
				Queues:  []*schedulingv1.Queue{queue1},
			},
			err: true,
			dryrun: &DryrunRequest{
				UUID:      `case1`,
				Namespace: "default",
				Queue:     "q1",
				Tasks: []TaskTemplate{
					{
						Replicas: 1,
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"cpu":    resource.MustParse("10"),
											"memory": resource.MustParse("1Gi"),
										},
										Limits: v1.ResourceList{
											"cpu":    resource.MustParse("10"),
											"memory": resource.MustParse("1Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
		},

		{
			Name:         "case2: dryrun for task 4 replica for 1C1Gi had 2 node to allocate",
			allocateNode: set.New("n1", "n2"),
			err:          false,
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: plugins,
				Nodes:   []*v1.Node{n1, n2},
				Queues:  []*schedulingv1.Queue{queue1},
			},
			dryrun: &DryrunRequest{
				UUID:      `case2`,
				Namespace: "default",
				Queue:     "q1",
				Tasks: []TaskTemplate{
					{
						Replicas: 4,
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"cpu":    resource.MustParse("1"),
											"memory": resource.MustParse("1Gi"),
										},
										Limits: v1.ResourceList{
											"cpu":    resource.MustParse("1"),
											"memory": resource.MustParse("1Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
		},

		{
			Name: "case3: dryrun for task 5 replica for 1C1Gi can not allocate",
			err:  true,
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: plugins,
				Nodes:   []*v1.Node{n1, n2},
				Queues:  []*schedulingv1.Queue{queue1},
			},
			dryrun: &DryrunRequest{
				UUID:      `case3`,
				Namespace: "default",
				Queue:     "q1",
				Tasks: []TaskTemplate{
					{
						Replicas: 5,
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Resources: v1.ResourceRequirements{
										Requests: v1.ResourceList{
											"cpu":    resource.MustParse("1"),
											"memory": resource.MustParse("1Gi"),
										},
										Limits: v1.ResourceList{
											"cpu":    resource.MustParse("1"),
											"memory": resource.MustParse("1Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.Name, func(t *testing.T) {
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			jobs := make(map[api.JobID]*api.JobInfo)
			jobInfo := newJobInfo(test.dryrun)
			jobs[jobInfo.UID] = jobInfo

			ssn.Jobs = jobs

			res := allocate.Execute(ssn, jobs)

			nodes, err := res.AllocateNodes()

			if test.err {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
			}

			require.True(t, nodes.Equal(test.allocateNode))
		})
	}
}
