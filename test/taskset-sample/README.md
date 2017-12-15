## Tutorial of this sample
The sample is to simulate user behavior and show how to use taskset to submit a job.

## Build
Download kube-arbitrator to `$GOPATH/src/github.com/kubernetes-incubator/` from github `https://github.com/kubernetes-incubator/kube-arbitrator`

Build jobclient:

```
# cd $GOPATH/src/github.com/kubernetes-incubator/kube-arbitrator
# make sample
```

## Usage

```
Usage of jobclient:
      --kubeconfig string   Path to kubeconfig file with authorization and master location information
      --namespace string    The namespaces submit job
      --queue string        The queue name
      --sleeptime string    The task running time (default "60")
      --taskno int32        The task number in each job (default 4)
```

Run `jobclient` like following

```
./_output/bin/jobclient --kubeconfig /root/.kube/config --namespace ns01 --queue q01 --taskno 5 --sleeptime 300
```

`jobclient` will create 3 taskset(it is fixed in this sample) under queue `q01`, the queue namespaces is `ns01`, and watch taskset status. The job contains `5` pods and each pod run `300` seconds. 

Then [run kube-arbitrator](https://github.com/kubernetes-incubator/kube-arbitrator/blob/master/doc/usage/tutorial.md) to allocate resources. When a taskset is allocated resources, jobclient will submit a job.

NOTE:

* The Queue `q01` must be created before run the sample. This [document](https://github.com/kubernetes-incubator/kube-arbitrator/blob/master/doc/usage/tutorial.md) show how to create a queue.
* Currently, kube-arbitrator cannot recognize which taskset a job belongs to, `jobclient` need handle it.