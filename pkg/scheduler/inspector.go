package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"volcano.sh/volcano/pkg/scheduler/cache"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	sch "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	schedulerOptions "volcano.sh/volcano/cmd/scheduler/app/options"
	"volcano.sh/volcano/pkg/inspector"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const (
	inspectorPort = "8848"

	WorkloadTypeVCJob       = "vcjob"
	WorkloadTypeJob         = "job"
	WorkloadTypeDeploy      = "deploy"
	WorkloadTypeStatefulSet = "statefulset"
	WorkloadTypePod         = "pod"
)

var (
	inspectorActions = []string{
		"allocate",
	}
)

// ScheduleInspector is the Schedule Inspector
type ScheduleInspector struct {
	scheduler *Scheduler
	router    *gin.Engine
}

func (si *ScheduleInspector) initInspectorActions() {
	actions := []framework.Action{}
	for _, actionName := range inspectorActions {
		action, ok := framework.GetAction(strings.TrimSpace(actionName))
		if !ok {
			klog.Fatalf("Failed to find Action %s", actionName)
		}
		actions = append(actions, action)
	}
	si.scheduler.actions = actions
}
func NewScheduleInspector(config *rest.Config, opt *schedulerOptions.ServerOption) (*ScheduleInspector, error) {
	s, e := NewScheduler(config, opt)
	if e != nil {
		return nil, e
	}

	si := &ScheduleInspector{
		scheduler: s,
	}
	si.router = Router(si)
	si.initInspectorActions()
	return si, nil
}

func (si *ScheduleInspector) Run(stopCh <-chan struct{}) {
	pc := si.scheduler
	pc.loadSchedulerConf()
	go pc.watchSchedulerConf(stopCh)
	// Start cache for policy.
	pc.cache.SetMetricsConf(pc.metricsConf)
	pc.cache.Run(stopCh)
	klog.V(2).Infof("Scheduler completes Initialization and start to run")

	go runSchedulerSocket()

	server := &http.Server{
		Addr:    ":" + inspectorPort,
		Handler: si.router,
	}

	go func() {
		<-stopCh
		klog.V(2).Infof("Closing service")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			klog.V(2).Infof("Error shutting down server: %v", err)
		}
	}()

	err := server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		klog.V(2).Infof("Error starting server: %v", err)
	}
}

func addJobPodGroup(job *api.JobInfo) {
	var minRes = make(corev1.ResourceList)
	if job == nil || job.PodGroup != nil {
		return
	}
	for _, task := range job.Tasks {
		for k, v := range task.Resreq.ScalarResources {
			minRes[k] = resource.MustParse(fmt.Sprintf("%f", v))
		}
	}

	pg := &api.PodGroup{
		Version: api.PodGroupVersionV1Beta1,
	}
	pg.Spec.MinResources = &minRes
	job.SetPodGroup(pg)
}

func newJobInfo(dryrun *DryrunRequest) *api.JobInfo {
	jobName := dryrun.UUID
	jobID := api.JobID(fmt.Sprintf("%s/%s", dryrun.Namespace, jobName))
	jobInfo := api.NewJobInfo(jobID)
	for id, task := range dryrun.Tasks {
		for j := 0; j < int(task.Replicas); j++ {
			tpod := &corev1.Pod{}
			tpod.Name = fmt.Sprintf("%s-%d-%d", jobName, id, j)
			tpod.Namespace = dryrun.Namespace
			tpod.Status.Phase = corev1.PodPending
			tpod.Spec = task.Spec
			tpod.UID = types.UID(tpod.Name)
			tpod.Annotations = map[string]string{
				sch.KubeGroupNameAnnotationKey: jobName,
			}

			klog.V(3).Infof("Add task %+v for request:%v", tpod, dryrun.UUID)
			ti := api.NewTaskInfo(tpod)
			addJobPodGroup(jobInfo)
			jobInfo.AddTaskInfo(ti)
		}
	}
	jobInfo.Queue = api.QueueID(dryrun.Queue)
	jobInfo.Namespace = dryrun.Namespace
	jobInfo.Name = jobName
	return jobInfo
}

func (si *ScheduleInspector) simulate(req *DryrunRequest) (res *inspector.ScheduleResult) {
	klog.V(4).Infof("Start simulate ... %+v", req)
	defer klog.V(4).Infof("End simulate ...")

	pc := si.scheduler
	pc.mutex.Lock()
	plugins := pc.plugins
	configurations := pc.configurations
	pc.mutex.Unlock()

	jobs := make(map[api.JobID]*api.JobInfo)
	jobInfo := newJobInfo(req)
	jobs[jobInfo.UID] = jobInfo
	sc := pc.cache.(*cache.SchedulerCache)
	sc.Jobs = jobs

	ssn := framework.OpenSession(pc.cache, plugins, configurations)
	defer func() {
		framework.CloseSession(ssn)
	}()

	ssn.UID = types.UID(req.UUID)
	return inspector.Execute(ssn, jobs)
}

func GetPodQueue[T *corev1.Pod | corev1.Pod | corev1.PodTemplateSpec](pod T) string {
	var labels, annotations map[string]string

	switch p := any(pod).(type) {
	case *corev1.Pod:
		labels = p.Labels
		annotations = p.Annotations
	case corev1.Pod:
		labels = p.Labels
		annotations = p.Annotations
	case corev1.PodTemplateSpec:
		labels = p.Labels
		annotations = p.Annotations
	}

	if labels != nil {
		if queue, ok := labels[batch.QueueNameKey]; ok {
			return queue
		}
	}

	if annotations != nil {
		if queue, ok := annotations[batch.QueueNameKey]; ok {
			return queue
		}
		if queue, ok := annotations[sch.QueueNameAnnotationKey]; ok {
			return queue
		}
	}

	return "default"
}

func (si *ScheduleInspector) reason(ctx context.Context, req *ReasonRequest) (res *inspector.ScheduleResult) {
	klog.V(4).Infof("Start reason ... %+v", req)
	defer klog.V(4).Infof("End reason ...")

	client := si.scheduler.cache.Client()
	vcclient := si.scheduler.cache.VCClient()

	dryrun := &DryrunRequest{
		UUID:      fmt.Sprintf("%s-%s/%s", req.Workload, req.Namespace, req.Name),
		Namespace: req.Namespace,
		Tasks:     make([]TaskTemplate, 0),
	}

	switch req.Workload {
	case WorkloadTypeVCJob:
		vcjob, err := vcclient.BatchV1alpha1().Jobs(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
		if err != nil {
			return inspector.FailResult(err)
		}
		dryrun.Queue = vcjob.Spec.Queue
		for _, task := range vcjob.Spec.Tasks {
			spec := task.Template.Spec
			replica := task.Replicas
			dryrun.Tasks = append(dryrun.Tasks, TaskTemplate{
				Replicas: replica,
				Spec:     spec,
			})
		}

	case WorkloadTypeJob:
		jb, err := client.BatchV1().Jobs(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
		if err != nil {
			return inspector.FailResult(err)
		}
		dryrun.Queue = GetPodQueue(jb.Spec.Template)
		dryrun.Tasks = append(dryrun.Tasks, TaskTemplate{
			Replicas: 1,
			Spec:     jb.Spec.Template.Spec,
		})

	case WorkloadTypeDeploy:
		deploy, err := client.AppsV1().Deployments(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
		if err != nil {
			return inspector.FailResult(err)
		}
		dryrun.Queue = GetPodQueue(deploy.Spec.Template)

		dryrun.Tasks = append(dryrun.Tasks, TaskTemplate{
			Replicas: *deploy.Spec.Replicas,
			Spec:     deploy.Spec.Template.Spec,
		})

	case WorkloadTypeStatefulSet:
		ss, err := client.AppsV1().StatefulSets(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
		if err != nil {
			return inspector.FailResult(err)
		}
		dryrun.Queue = GetPodQueue(ss.Spec.Template)

		dryrun.Tasks = append(dryrun.Tasks, TaskTemplate{
			Replicas: *ss.Spec.Replicas,
			Spec:     ss.Spec.Template.Spec,
		})

	case WorkloadTypePod:
		pod, err := client.CoreV1().Pods(req.Namespace).Get(ctx, req.Name, metav1.GetOptions{})
		if err != nil {
			return inspector.FailResult(err)
		}
		dryrun.Queue = GetPodQueue(pod)
		dryrun.Tasks = append(dryrun.Tasks, TaskTemplate{
			Replicas: 1,
			Spec:     pod.Spec,
		})

	default:
		return inspector.FailResult(fmt.Errorf("unsupported workload type %s", req.Workload))
	}

	return si.simulate(dryrun)
}
