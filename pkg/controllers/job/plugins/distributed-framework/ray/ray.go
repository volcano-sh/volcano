package ray

import (
	"flag"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	"volcano.sh/volcano/pkg/controllers/job/helpers"
	pluginsinterface "volcano.sh/volcano/pkg/controllers/job/plugins/interface"
)

const (
	// RayPluginName is the name of the plugin
	RayPluginName = "ray"
	// DefaultPort is the default port for Ray
	DefaultPort = 6379
	// DefaultHead is the default task name of head node
	DefaultHead = "head"
	// DefaultWorker is the default task name of worker node
	DefaultWorker = "worker"

	// EnvRayHeadService is the env name of Ray head service
	EnvRayHeadService = "RAY_HEAD_SERVICE_HOST"
	// EnvRayPort is the env name of Ray port
	EnvRayPort = "RAY_PORT"
	// EnvRayRedisPassword is the env name of Ray Redis password
	EnvRayRedisPassword = "RAY_REDIS_PASSWORD"
)

type rayPlugin struct {
	rayArguments []string
	clientset    pluginsinterface.PluginClientset
	headName     string
	workerName   string
	port         int
}

// New creates ray plugin.
func New(client pluginsinterface.PluginClientset, arguments []string) pluginsinterface.PluginInterface {
	klog.Infof("Initializing Ray plugin with arguments: %v", arguments)
	rp := rayPlugin{rayArguments: arguments, clientset: client}
	rp.addFlags()
	return &rp
}

func (rp *rayPlugin) addFlags() {
	klog.Infof("Adding flags for Ray plugin")
	flagSet := flag.NewFlagSet(rp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&rp.headName, "head", DefaultHead, "name of head role task")
	flagSet.StringVar(&rp.workerName, "worker", DefaultWorker, "name of worker role task")
	flagSet.IntVar(&rp.port, "port", DefaultPort, "open port for containers")
	if err := flagSet.Parse(rp.rayArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", rp.Name(), err)
	}
}

func (rp *rayPlugin) Name() string {
	klog.Infof("Ray plugin name requested: %s", RayPluginName)
	return RayPluginName
}

func (rp *rayPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	headIndex := helpers.GetTaskIndexUnderJob(rp.headName, job)
	if headIndex == -1 {
		klog.Errorf("job %v doesn't have task %v", job.Name, rp.headName)
		return nil
	}

	headEnvVars := []v1.EnvVar{}
	headService := rp.generateHeadService(job.Spec.Tasks[headIndex], job.Name)
	headEnvVars = append(headEnvVars, v1.EnvVar{
		Name:  EnvRayHeadService,
		Value: headService,
	}, v1.EnvVar{
		Name:  EnvRayPort,
		Value: fmt.Sprintf("%v", rp.port),
	}, v1.EnvVar{
		Name:  EnvRayRedisPassword,
		Value: "password", // TODO: Make this configurable
	})

	for i, c := range pod.Spec.Containers {
		rp.openContainerPort(&c, i, pod)
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, headEnvVars...)
	}

	return nil
}

func (rp *rayPlugin) generateHeadService(task batch.TaskSpec, jobName string) string {
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

func (rp *rayPlugin) openContainerPort(c *v1.Container, index int, pod *v1.Pod) {
	hasPort := false
	for _, p := range c.Ports {
		if p.ContainerPort == int32(rp.port) {
			hasPort = true
			break
		}
	}

	if !hasPort {
		port := v1.ContainerPort{
			Name:          "ray-port",
			ContainerPort: int32(rp.port),
		}

		pod.Spec.Containers[index].Ports = append(pod.Spec.Containers[index].Ports, port)
	}
}

func (rp *rayPlugin) OnJobAdd(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+rp.Name()] == rp.Name() {
		return nil
	}
	job.Status.ControlledResources["plugin-"+rp.Name()] = rp.Name()
	return nil
}

func (rp *rayPlugin) OnJobDelete(job *batch.Job) error {
	if job.Status.ControlledResources["plugin-"+rp.Name()] != rp.Name() {
		return nil
	}
	delete(job.Status.ControlledResources, "plugin-"+rp.Name())
	return nil
}

func (rp *rayPlugin) OnJobUpdate(job *batch.Job) error {
	return nil
}
