# Guide for running kubemark with kube-batch scheduler

## 1.Pre-requisites
There are several requirements before installing kubemark cluster for test.
1. **Docker**: Building kubernetes releases with command``make quick-release`` require having a docker environment.
2. **Google cloud SDKs**: Kubemark utilize GCE to create its master node in default, therefore you need ensure
   google cloud SDKs are installed on your environment. **NOTES**: ``gcloud beta`` is required in addition.
3. **External Kubernetes cluster**: Kubemark utilize external kubernetes cluster to hold its ``Hollow Nodes``, before
   creating kubemark cluster, please ensure your environment can successfully execute command: ``kubectl get nodes``
4. **Python**: There are some commands which use python binaries to perform specific tasks, please ensure it's installed
   on your environment.
5. **Google Container Registry API**: Google container register API should be enabled to upload images during setup cluster
   process.

## 2. Setup kubemark clusters
All GCE related configure options are located in file 
``vendor/k8s.io/kubernetes/cluster/kubemark/gce/config-default.sh``, it's usually required to update some of them according
to your own GCP environment before executing. For instance:
```$xslt
ZONE=<specify your own GCP zone>
NETWORK=<specify your own network resource name>
```
then execute command in root folder:
```$xslt
./test/kubemark/start-kubemark.sh
```
Setup script will clone kubernetes project to temp folder and build its releases every time, 
In order to speed up the performance, it's better to clone & build the kubernetes yourself.
Then, try the commands in root folder:
```$xslt
SOURCE_OUTPUT=<path to kubernetes `_output` folder> ./test/kubemark/start-kubemark.sh
```

After script successfully executed, you can verify your kubemark cluster with command in kube-batch root folder:
```$xslt
➜  kube-batch git: kubectl --kubeconfig test/kubemark/kubeconfig.kubemark get nodes
NAME                STATUS   ROLES    AGE    VERSION
hollow-node-2wgdq   Ready    <none>   5m8s   v1.15.0-alpha.1.162+b3847120241251
hollow-node-8kksj   Ready    <none>   5m8s   v1.15.0-alpha.1.162+b3847120241251
......
```
## 3. Running performance tests:
Command as below:
```$xslt
go test ./test/e2e/kube-batch -v -timeout 30m --ginkgo.focus="Feature:Performance"
```
When finished, there would be a metric json file with timestamp generated in folder ```test/e2e```

## 4. Cleanup kubemark cluster
Command as below:
```$xslt
./test/kubemark/stop-kubemark.sh
```
