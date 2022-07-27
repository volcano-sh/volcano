package pytorch

import (
	"flag"
	"fmt"
	"strconv"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// PytorchPluginName is the name of the plugin
	PytorchPluginName = "pytorch"
	// DefaultPort is the default port for pytorch
	DefaultPort = 23456
	// DefaultMaster is the default task name of master host
	DefaultMaster = "master"
	// DefaultWorker is the default task name of worker host
	DefaultWorker = "worker"

	// EnvMasterPort is the env name of master port
	EnvMasterPort = "MASTER_PORT"
	// EnvMasterAddr is the env name of master addr
	EnvMasterAddr = "MASTER_ADDR"
	// EnvWorldSize is the env name of world size
	EnvWorldSize = "WORLD_SIZE"
	// EnvRank is the env name of rank
	EnvRank = "RANK"
)

type pytorchPlugin struct {
	pytorchArguments []string
	clientset        pluginsinterface.PluginClientset
	masterName       string
	workerName       string
	port             int
}

// New creates pytorch plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	pp := pytorchPlugin{pytorchArguments: arguments, clientset: client}
	pp.addFlags()
	return &pp
}

func (pp *pytorchPlugin) addFlags() {
	flagSet := flag.NewFlagSet(pp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&pp.masterName, "master", DefaultMaster, "name of master role task")
	flagSet.StringVar(&pp.workerName, "worker", DefaultWorker, "name of worker role task")
	flagSet.IntVar(&pp.port, "port", DefaultPort, "open port for containers")
	if err := flagSet.Parse(pp.pytorchArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", pp.Name(), err)
	}
}

func (pp *pytorchPlugin) Name() string {
	return PytorchPluginName
}

func (pp *pytorchPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	taskType := helpers.GetTaskKey(pod)
	masterIndex := helpers.GetTasklndexUnderJob(pp.masterName, job)
	if masterIndex == -1 {
		klog.Errorf("job %v doesn't have task %v", job.Name, pp.masterName)
		return nil
	}

	masterEnvVars := []v1.EnvVar{}
	masterAddr := pp.generateMasterAddr(job.Spec.Tasks[masterIndex], job.Name)
	masterEnvVars = append(masterEnvVars, v1.EnvVar{
		Name:  EnvMasterAddr,
		Value: masterAddr,
	}, v1.EnvVar{
		Name:  EnvMasterPort,
		Value: fmt.Sprintf("%v", pp.port),
	})

	masterRank := 0
	workerRank := 0
	if taskType == pp.workerName {
		index, err := strconv.Atoi(helpers.GetPodIndexUnderTask(pod))
		if err != nil {
			return err
		}

		workerRank = index + 1
	}

	totalReplicas := pp.getTotalReplicas(job)
	for i, c := range pod.Spec.Containers {
		pp.openContainerPort(&c, i, pod)

		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, masterEnvVars...)
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
			Name:  EnvWorldSize,
			Value: strconv.Itoa(int(totalReplicas)),
		})

		if taskType == pp.workerName {
			pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
				Name:  EnvRank,
				Value: strconv.Itoa(workerRank),
			})
		} else if taskType == pp.masterName {
			pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
				Name:  EnvRank,
				Value: strconv.Itoa(masterRank),
			})
		}
	}

	return nil
}

func (pp *pytorchPlugin) getTotalReplicas(job *batch.Job) int32 {
	jobReplicas := int32(0)
	for _, task := range job.Spec.Tasks {
		jobReplicas += task.Replicas
	}

	return jobReplicas
}

func (pp *pytorchPlugin) generateMasterAddr(task batch.TaskSpec, jobName string) string {
	hostName := task.Template.Spec.Hostname
	subdomain := task.Template.Spec.Subdomain
	if len(hostName) == 0 {
		hostName = helpers.MakePodName(jobName, task.Name, 0)
	}
	if len(subdomain) == 0 {
		subdomain = jobName
	}

	host := hostName + "." + subdomain
	return host
}

func (pp *pytorchPlugin) openContainerPort(c *v1.Container, index int, pod *v1.Pod) {
	hasPort := false
	for _, p := range c.Ports {
		if p.ContainerPort == int32(pp.port) {
			hasPort = true
			break
		}
	}

	if !hasPort {
		port := v1.ContainerPort{
			Name:          "pytorchjob-port",
			ContainerPort: int32(pp.port),
		}

		pod.Spec.Containers[index].Ports = append(pod.Spec.Containers[index].Ports, port)
	}
}

func (pp *pytorchPlugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+pp.Name()] == pp.Name() {
		return nil
	}
	job.Status.ControlledResources["plugin-"+pp.Name()] = pp.Name()
	return nil
}

func (pp *pytorchPlugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+pp.Name()] != pp.Name() {
		return nil
	}
	delete(job.Status.ControlledResources, "plugin-"+pp.Name())
	return nil
}

func (pp *pytorchPlugin) OnJobUpdate(job *batch.Job) error {
	return nil
}
