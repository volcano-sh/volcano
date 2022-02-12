# Extender User Guide

### Install volcano

#### 1. Install from source

Refer to [Install Guide](../../installer/README.md) to install volcano.

#### 2. Deploy extender 

Deploy extender into kubernetes cluster. Extender needs to expose domain name or IP address and verbs that can be provided.

#### 3. Update Volcano configuration
```shell script
kubectl edit cm -n volcano-system volcano-scheduler-configmap
```

Users can view the meaning of the parameters through the [documentation](https://github.com/volcano-sh/volcano/blob/master/docs/design/extender.md)
```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "reclaim, allocate, backfill, preempt"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
    - plugins:
      - name: drf
      - name: predicates
      - name: extender
        arguments:
          extender.urlPrefix: http://127.0.0.1:8713
          extender.httpTimeout: 100ms
          extender.onSessionOpenVerb: onSessionOpen
          extender.onSessionCloseVerb: onSessionClose
          extender.predicateVerb: predicate
          extender.prioritizeVerb: prioritize
          extender.preemptableVerb: preemptable
          extender.reclaimableVerb: reclaimable
          extender.queueOverusedVerb: queueOverused
          extender.jobEnqueueableVerb: jobEnqueueable
          extender.ignorable: true
```

### Verify Extender is working
  The user can see in the log something like : 'Initialize extender plugin with configuration : {your configuration}'

