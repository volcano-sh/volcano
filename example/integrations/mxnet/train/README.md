# Distributed Training with MXNet and CPU on Volcano

This is an example of running distributed training with MXNet and CPU on Volcano. The source code is taken from
MXNet team's example [here](https://github.com/apache/incubator-mxnet/blob/master/example/distributed_training-horovod/gluon_mnist.py).

The directory contains the following files:
* Dockerfile: Builds the independent worker image.
* Makefile: For building the above image.
* train-mnist-cpu.yaml: The Volcano Job spec.

To run the example, edit `train-mnist-cpu.yaml` for your image's name and version. Then run
```
kubectl apply -f train-mnist-cpu.yaml -n ${NAMESPACE}
```
to create the job.

Then use
```
kubectl -n ${NAMESPACE} describe job.batch.volcano.sh mxnet-job
```
to see the status.
