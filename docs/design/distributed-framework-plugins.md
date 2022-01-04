# Distributed Framework Plugins

  - [Motivation](#motivation)
  - [Goals](#goals)
  - [Design](#design)
    - [Introduction of ML Distributed Pattern](#introduction-of-ml-distributed-pattern)
    - [Implementation](#implementation)
      - [Tensorflow Plugin](#tensorflow-plugin)
      - [Other Framework](#other-framework)
      - [Task Launch Sequence](#task-launch-sequence)
      - [Webhook](#webhook)
## Motivation

Volcano is widely used in machine learning, but sometimes it is quite complicated for users to set configs.

- User has to be familiar with the volcano job plugins (i.e the `svc`, `env` and `ssh` job plugins). 
  - For example, if you want to execute a MPI job using volcano, you should know exactly the behavior which is providing all worker node's names in a file like `/etc/volcano/mpiworker.host`.
- User has to be familiar with the shell syntax, which is used for generating a cluster spec parameter from the file produced by plugins. 
  - e.g.  generate `TF_CONFIG` from files `worker.host`  and `ps.host` for Tensorflow job
- It is not straightforward enough to run a distributed ML job via Volcano. User has to carefully set the `lifeCyclePolicy` and `restartPolicy` in every task for distributed training. 
  - For example, in MPI job, the master task will be failed and restarted until all worker tasks are ready. Therefore, user should add  `OnFailure` restart policy and `TaskCompleted-CompleteJob` lifecycle policy to master task.

If we can add more in-tree plugins for distributed ML job, which lets Volcano know more about the job type, the complexity of using volcano for ML workloads will be reduced.

## Goals

- Add several plugins for distributed framework, including but not limited to Tensorflow-Plugin, MPI-Plugin, Pytorch-Plugin, MxNet-Plugin

  - These plugins will patch pod spec to fit the distributed pattern of a specified framework.

- Make it easier to set ML distributed topology.

  - By using init containers, plugins will make sure tasks is launched in the topology order.

- Make it easier to use these plugin. Users only need to add a few lines in job spec.  e.g.
  ```yaml
  spec:
    plugins:
      tensorflow: []
  ```

## Design

### Introduction of ML Distributed Pattern 

Here is a summary of distributed training pattern in various ML frameworks, including the node topology, environment variables, file and entrypoint for distributed training.

| Framework                         | Topology                                                     | Environment Variables                                        | File                                           | Entrypoint                                                   |
| --------------------------------- | ------------------------------------------------------------ | ------------------------------------------------------------ | ---------------------------------------------- | ------------------------------------------------------------ |
| Tensorflow                        | **PS mode**: Chief + Evaluator + Worker + PS<br />**All-Reduce mode**: multiple workers | `TF_CONFIG`: tensorflow cluster spec                         | none                                           | `python TRAINING_SCRIPT.py (--arg1 ... train script args...)` |
| Pytorch (Official recommendation) | Master + Worker                                              | `PET_MASTER_ADDR`: master address<br/>`PET_MASTER_PORT`: master port<br />`PET_NNODES`: node number<br/>`PET_NPROC_PER_NODE`: process number in every node<br/>`PET_NODE_RANK`: current node index | none                                           | `python -m torch.distributed.run TRAINING_SCRIPT.py (--arg1 ... train script args...)` |
| Pytorch (custom)                  | Master + Worker                                              | `MASTER_ADDR`: master address<br/>`MASTER_PORT`: master port<br />`WORLD_SIZE`: node number<br/>`RANK`: current node index | none                                           | `python TRAINING_SCRIPT.py (--arg1 ... train script args...)` |
| MXNet                             | Scheduler + Worker + PS                                      | `DMLC_PS_ROOT_URI`: scheduler address<br />`DMLC_PS_ROOT_PORT`: scheduler port<br/>`DMLC_NUM_SERVER`: parameter server number <br/>`DMLC_NUM_WORKER`: worker number<br/>`DMLC_ROLE`: current node role | none                                           | `python TRAINING_SCRIPT.py (--arg1 ... train script args...)` |
| MPI                               | Master + Worker                                              | `OMPI_MCA_orte_default_hostfile`: default host file path<br /> | hostfile: with every node name and slot number | **master node**: `mpirun --hostfile HOSTFILE (-H HOSTS)  python TRAINING_SCRIPT.py (--arg1 ... train script args...)`<br />**worker node**: `/usr/sbin/sshd -D` |

### Implementation

With the introduction of distributed pattern in various frameworks, we can implement various plugins.

#### Tensorflow Plugin

The key implementation of tensorflow plugin is that how to set correct `TF_CONFIG` environment variable for every pod.

Firstly, we must known the cluster role of task in volcano job, and the port to be exposed. And this information can be passed by plugin arguments, which is defined in job spec.

```yaml
spec:
  plugins:
    # set tensorflow plugin
    tensorflow: ["--port=5000", "--worker=worker", "--ps=ps"]
```



In the implementation of `tensorflowPlugin`, these arguments will be parsed.

```go
// tensorflowPlugin is plugin for tensorflow framework
type tensorflowPlugin struct {
	tfArguments   []string
	Clientset     pluginsinterface.PluginClientset
	psName        string
	workerName    string
	chiefName     string
	evaluatorName string
	port          int
}
// parse all arguments
func (tp *tensorflowPlugin) addFlags() {
	flagSet := flag.NewFlagSet(tp.Name(), flag.ContinueOnError)
	flagSet.StringVar(&tp.psName, "ps", "ps", "name of ps role task")
	flagSet.StringVar(&tp.workerName, "worker", "worker", "name of ps role task")
	flagSet.StringVar(&tp.chiefName, "chief", "chief", "name of chief role task")
	flagSet.StringVar(&tp.evaluatorName, "evaluator", "evaluator", "name of evaluator role task")
	flagSet.IntVar(&tp.port, "port", 2222, "serviec port")
	if err := flagSet.Parse(sp.tfArguments); err != nil {
		klog.Errorf("plugin %s flagset parse failed, err: %v", tp.Name(), err)
	}
}
```



And then patch the pod spec in method `OnPodCreate`.

```go
func (tp *tensorflowPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	// do not patch if job is not distributed
	if len(job.Spec.Tasks) == 1 && job.Spec.Tasks[0].Replicas == 1 {
		return nil
	}
	// generate tfconfig spec
	c, err := tp.generateTFConfig(pod, job)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	// add TF_CONFIG envrionment
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
			Name:  "TF_CONFIG",
			Value: string(raw),
		})
	}
	return nil
}
```



Here is the structure of  `TF_CONFIG`:

```go
type tfClusterSpec struct {
	Cluster clusterInfo `json:"cluster"`
	Task    taskInfo    `json:"task"`
}

type clusterInfo struct {
	PS        []string `json:"ps,omitempty"`
	Worker    []string `json:"worker,omitempty"`
	Chief     []string `json:"chief,omitempty"`
	Evaluator []string `json:"evaluator,omitempty"`
}

type taskInfo struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}
```

And we can generate a `tfClusterSpec` for each pod in the job, here is an example:
```go
// generateTFConfig generate tfClusterSpec by a given pod and job
func (tp *tensorflowPlugin) generateTFConfig(pod *v1.Pod, job *batch.Job) (tfClusterSpec, error) {
	// get task index by pod
	index, err := strconv.Atoi(helpers.GetPodIndexUnderTask(pod))
	if err != nil {
		return tfClusterSpec{}, err
	}
	// get task type by pod and job
	taskType := tp.getTaskType(pod, job)
	// get cluster info by job
	spec := tfClusterSpec{
		Cluster: tp.getClusterInfo(job),
		Task: taskInfo{
			Type:  taskType,
			Index: index,
		},
	}
	return spec, nil
}

// getClusterInfo return a clusterInfo by a given job
func (tp *tensorflowPlugin) getClusterInfo(job *batch.Job) clusterInfo {
	cluster := clusterInfo{}
	for _, ts := range job.Spec.Tasks {
		hosts := []string{}
		for i := 0; i < int(ts.Replicas); i++ {
			// generate domain name for each task replicas
			hosts = append(hosts, helpers.MakeDomainName(job.Name, ts, i))
		}
		// assign all hostnames to clusterInfo
		switch ts.Name {
		case tp.psName:
			cluster.PS = hosts
		case tp.workerName:
			cluster.Worker = hosts
		case tp.chiefName:
			cluster.Chief = hosts
		case tp.evaluatorName:
			cluster.Evaluator = hosts
		}
	}
	return cluster
}
```

#### Other Framework

Most of other frameworks is similar to Tensorflow. But the MPI framework is special. In most case, It needs a `hostfile`, e.g. :

```
jobname-worker-0.jobname slots=4
jobname-worker-1.jobname slots=4
jobname-worker-2.jobname slots=4
```

To generate the `hostfile`, we need to create a `configMap` in `OnJobAdd` phase.

```go
func (mp *mpiPlugin) OnJobAdd(job *batch.Job) error {
	// generate hostfile, and create a configmap
	data := map[string]string{"hostfile": mp.hostfile(job)}
	if err := helpers.CreateOrUpdateConfigMap(job, mp.Clientset.KubeClients, data, mp.cmName(job)); err != nil {
		return err
	}

	if job.Status.ControlledResources["plugin-"+mp.Name()] == mp.Name() {
		return nil
	}
	job.Status.ControlledResources["plugin-"+mp.Name()] = mp.Name()
	return nil
}
```

The data in `configMap` is as follows:

```yaml
data:
  hostfile: |-
    jobname-worker-0.jobname slots=4
    jobname-worker-1.jobname slots=4
    jobname-worker-2.jobname slots=4
```

> The utility function `CreateOrUpdateConfigMap` will add an owner reference in the Configmap's metadata, thus it will be deleted when the job is deleted.



In `OnPodCreate` phase, the `hostfile` will be added into pod volumes, and mouted to specified path (e.g. `/etc/mpi/hostfile`). The `OMPI_MCA_orte_default_hostfile` environment variable should also be set.

```go
func (mp *mpiPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	// generate hostfile volume and volumeMount
	volume := mp.hostfileVolume(job)
	mount := mp.hostfileVolumeMount(job)
	// add to pod and containers
	pod.Spec.Volumes = append(pod.Spec.Volumes, vm)
	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, mount)
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, v1.EnvVar{
		    Name: "OMPI_MCA_orte_default_hostfile",
		    Value: "/etc/mpi/hostfile",
		})
	}
	return nil
}
```

#### Task Launch Sequence

As mentioned in section *Motivation*, task-level topology and launch sequence is common in ML distributed training. But there is no task-level scheduling policy in Volcano at present.
We could set task dependency in plugins, 

 - e.g. we could use `InitContainer` to control the dependency of tasks.
 - any other approaches are welcomed.

##### Using InitContainer

In `OnPodCreate`, we could patch `InitContainers` to pod. Here is an example for MPI-Plugin:

```go
func (mp *mpiPlugin) OnPodCreate(pod *v1.Pod, job *batch.Job) error {
	// ......
	// Other code
	// ......

	// Add an init container to wait for dependency tasks
	// Get dependency tasks by pod and job
	depTasks := mp.getDepTasks(pod, job)
	if len(depTasks) != 0 {
	    // Generate an init container and insert it into pod spec
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, mlhelpers.CreateInitContainer(depTasks, job))
	}
	return nil
}
```

For MPI-Plugin, master task should wait for worker task. So we only generate dependency tasks for master pod:

```go
func (mp *mpiPlugin) getDepTasks(pod *v1.Pod, job *batch.Job) (tasks []batch.TaskSpec) {
	// get task name from pod
	taskName := mlhelpers.GetTaskName(pod, job)
	if taskName == mp.masterName {
	    // get task spec from job by a given task name
		if t, ok := mlhelpers.GetTaskSpec(mp.workerName, job); ok {
			tasks = append(tasks, t)
		}
	}
	return
}
```

We offer one implementation for `InitContainer`, it has limitations but works for common scenarios. we welcome better approaches.

The logic in the init container is quite simple. It will send an ICMP message to the domain name of every task pod to check if the pod is alived. Here is an shell script example:

```shell
SECONDS=0
while true
do
	ok=true
	for ip in ${IP_LIST}
	do
		ping -c 1 $ip >/dev/null
		if [ $? -ne 0 ]
		then
			ok=false
			break
		fi
	done
	if $ok
	then
		exit 0
	else
		if [ $SECONDS -gt ${WAIT_TIMEOUT} ]
		then
			exit 1
		fi
		sleep 5
	fi
done
```



##### Alternatives Considered

With the introduction of [Task Launch Order Design](https://github.com/volcano-sh/volcano/blob/master/docs/design/task-launch-order-within-job.md),  we can use the existing solution to manage task launch sequence.

#### Webhook

The Distributed Framework Plugins mentioned above work depending on svc plugin or others, thus we need to add new logic to ensure that svc plugin or others exist.

The new logic to be added in webhook is shown as in below:

1. Check if Distributed-Framework plugins exist
2. Patch job spec with plugin denpendency