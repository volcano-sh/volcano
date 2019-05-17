## Predicates Plugin

## Introduction

Predicates plugin is used by all scheduler actions like allocate, preeempt, backfill and reclaim.
There are multiple predicate function implemented by Kubernetes Scheduler, few of those predicate
checks has been used in Kube-Batch.  Predicates are pure functions which take node and pod object and 
return a bool whether that pod fits onto that node or not.

## Plugin Configuration

In Predicates plugin, few predicates run by default, but few optional predicates can be enabled by providing
appropriate configuration to predicates plugin from scheduler config file.  Optional Predicates supported
by kube-batch are MemoryPressure, DiskPressure, PIDPressure predicate checks. To enable those predicate checks,
scheduler config file should provided with configuration given below.

    User Should give predicatesEnable in this format(predicate.MemoryPressureEnable, predicate.DiskPressureEnable, predicate.PIDPressureEnable.
    Currently supported only for MemoryPressure, DiskPressure, PIDPressure predicate checks.
    
       actions: "reclaim, allocate, backfill, preempt"
       tiers:
       - plugins:
         - name: priority
         - name: gang
         - name: conformance
       - plugins:
         - name: drf
         - name: predicates
           arguments:
             predicate.MemoryPressureEnable: true
             predicate.DiskPressureEnable: true
             predicate.PIDPressureEnable: true
         - name: proportion
         - name: nodeorder