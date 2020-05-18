# Click-Through-Rate Distributed Training with PaddlePaddle on Volcano

This is an example of running Click-Through-Rate(ctr) distributed training with PaddlePaddle on Volcano. The source code
is taken from PaddlePaddle EDL team's example [here](https://github.com/PaddlePaddle/edl/tree/develop/example/ctr).

The directory contains the following files:
* ctr-paddlepaddle-on-volcano.yaml: The Volcano Job spec.

To run the example, edit `ctr-paddlepaddle-on-volcano.yaml` for your image's name and version. Then run
```
kubectl apply -f ctr-paddlepaddle-on-volcano.yaml -n ${NAMESPACE}
```
to create the job.

Then use
```
kubectl -n ${NAMESPACE} describe job.batch.volcano.sh ctr-volcano
```
to see the status.
