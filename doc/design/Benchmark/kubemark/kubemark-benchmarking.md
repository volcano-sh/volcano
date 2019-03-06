## BACKGROUND

Performance evaluation of KubeBatch scheduler. Kubemark will be used as a tool for benchmarking.
Kubemark is a performance testing tool which we use to run simulated kubernetes clusters. The primary use case is scalability 
testing, as creating simulated clusters is faster and requires less resources than creating real ones.

## OBJECTIVE

-  Evaluating scheduler performance of kube-batch

## DESIGN OVERVIEW

We assume that we want to benchmark a test T with kube-batch scheduler.
For the benchmarking to be meaningful,these kube-batch should be running in a kubemark cluster and
at  scale (eg.  run 100 nodes).
At a high-level, the kubemark should:

- *choose the set of runs* of tests T executed on environment using the kube-batch scheduler by setting "schedulerName: kube-batch" in the pod spec,
- *obtain the relevant metrics (E2e Pod startup latencies)* for the runs chosen for comparison,
- *compute the similarity* for  metric, across both the samples,

Final output of the tool will be the answer to the question performance of the kube-batch scheduler. 



### Choosing set of metrics to compare

Kubermark give the following metrics
- pod startup latency
- api request serving latency (split by resource and verb)

In our case we would requiring <b> E2E Pod Starup Latency </b> which would include
   - The Latency for <b> scheduling the pod (create to schedule) </b>
   - The Latency for <b> binding the pod to node (schedule to run) </b>

## Changes to be done to the kubemark

  1) Modify the kube-mark startup script to bring up kube-batch scheduler along with the external kubernetes cluster. <br /> 
     Create CRDs for podgroup and queue.
  2) Run the density/latency test 3k pods on 100 (fake) nodes with batch scheduler.<br />
     For Batch scheduler test case : Modify the density/latency test case to create podgroup and add the podgroup name to the annotation along with "schedulerName: kube-batch" in the pod spec.
  3) Print out the metrics.

