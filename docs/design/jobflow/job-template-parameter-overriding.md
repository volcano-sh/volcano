# Job Template Parameter Overriding

[@mahdikhashan](https://github.com/mahdikhashan); June 9, 2025

## Summary

This is an enhancement on top of the original design for JobFlow, it will introduce a new field to the FlowSpec that users can reference a Job Template by its name and patch one or more parameters.

## Table of Contents

- [Job Template Parameter Overriding](#job-template-parameter-overriding)
  * [Motivation](#motivation)
  * [User Stories](#user-stories)
    + [Story 1](#story-1)
  * [Design Details](#design-details)
    + [Patch Field](#patch-field)

## Motivation

Currently, when user reference a Job Template inside a Job Flow, they don't have the ability to patch or override a parameter in place like change the version of the image or resources allocated to the related pod. This Forces them to define multiple Job Templates, resulting in reduced reusability and brings frustrations.

## User Stories

### Story 1

As a machine learning engineer, I want to be able to run my tasks on different versions of 
my experiments which are inside a container, being able to override this job template in 
the flow instead of defining multiple job-templates prevents duplication and improves the 
readability of my task description.

Such example could be:

```yaml
apiVersion: flow.volcano.sh/v1alpha1
kind: JobTemplate
metadata:
  name: ml-task
spec:
  minAvailable: 1
  schedulerName: volcano
  queue: default
  tasks:
    - replicas: 1
      name: "ml-task"
      template:
        metadata:
          name: train-job
        spec:
          containers:
            - name: cnn-mnist-torch
              image: cnn-mnist-torch:latest
              command:
                - python
                - train.py
                - --epoch
                - "3"
              imagePullPolicy: IfNotPresent
              resources:
                requests:
                  cpu: "1"
          restartPolicy: OnFailure
---
apiVersion: flow.volcano.sh/v1alpha1
kind: JobFlow
metadata:
  name: mnist-experiments
  namespace: training
spec:
  jobRetainPolicy: delete  
  flows:
  - name: ml-task
    patch:
      spec:
        tasks:
        - template:
            spec:
              containers:
              - image: "myregistry/cnn-mnist-torch-cuda:v1"
                resources:
                  requests:
                    gpu: "1"
```

## Design Details

To implement this feature, I introduce `patch` field to the flow spec, with it, user can address the spec for the JobTemplate.

### Patch Field

```yaml
type Flow struct {
  // +kubebuilder:validation:MinLength=1
  // +required
  Name string `json:"name"`
  // +optional
  DependsOn *DependsOn `json:"dependsOn,omitempty"`
  // +optional
  Patch *Patch `json:"patch,omitempty"`
}
```

```yaml
type Patch struct {
  // +optional
  Spec v1alpha1.JobSpec  `json:"spec,omitempty"`
}
```
