This document helps you get started debuging the Volcano code locally.
If you follow this guide and find some problem, please take a few minutes to update this file.

- [Prerequisites](#Prerequisites)
- [Debug Controllers](#debug-controllers)
- [Debug Scheduler](#debug-webhook)


## Prerequisites
Firstly, please make sure that you have installed the following dependencies (If not, please install them):
- Git
- Golang (version >= 1.13)
- Docker (version >= 19.03)
- GNU Make
- VSCode

Secondly, you need to clone/download the main `volcano` repo to local for the following debug.
```shell
$ git clone https://github.com/volcano-sh/volcano.git
```

Then, you need to deploy the local k8s cluster and install volcano (you can also use existing k8s cluster and install volcano by [this doc](https://volcano.sh/en/docs/installation/)).
```shell
$ cd <your-local-dir-to-volcano>
$ ./hack/local-up-volcano.sh
```

Finally, you get the local debugging environment.
```shell
$ kubectl get pods -n volcano-system
NAME                                       READY   STATUS      RESTARTS   AGE
pod/volcano-admission-5bd5756f79-p89tx     1/1     Running     0          6m10s
pod/volcano-admission-init-d4dns           0/1     Completed   0          6m10s
pod/volcano-controllers-687948d9c8-bd28m   1/1     Running     0          6m10s
pod/volcano-scheduler-94998fc64-9df5g      1/1     Running     0          6m10s
```

## Debug
This debugging method is based on VSCode, and you can also refer to this method for debugging on other IDEs.
### Debug Scheduler
1. Scale the number of volcano-shceduler replicas to 0
```shell
$ kubectl scale --replicas=0 deployment -n volcano-system volcano-scheduler 
```
2. Create profile for volcano-shceduler (like volcano-scheduler-configmap)
```shell
$ cd <your-local-dir-to-volcano>
$ cat<<EOF >volcano-scheduler.conf
actions: "enqueue, allocate, reclaim, preempt, backfill, shuffle"
tiers:
- plugins:
  - name: priority
  - name: pdb
EOF

```
> **Note**: This is a demo of volcano-scheduler.conf, you can create your own profile based on your needs.

3. Configure the startup file for debugging
```shell
$ cd <your-local-dir-to-volcano>
$ cat<<EOF >.vscode/launch.json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Scheduler Debug",
            "type": "go",
            "request": "launch",
            "mode": "debug",
            "program": "cmd/scheduler/main.go",
            "args": ["--logtostderr", "--scheduler-conf=<your-local-dir-to-volcano>/volcano-scheduler.conf", "--enable-healthz=true", 
            "--enable-metrics=true", "--leader-elect=false", "-v=4", "2>&1", "--kubeconfig=<your-local-path-to-kubeconfig>"]
        }
    ]
}
EOF

```
> **Note**: You need to change the `<your-local-dir-to-volcano>/volcano-scheduler.conf` to the local path to `volcano-scheduler.conf`, the `<your-local-path-to-kubeconfig>` to the local path to `kubeconfig` and adjust the other parameters based on your needs.

4. Go to debug happily

### Debug Controllers
TODO
### Debug Webhook
TODO