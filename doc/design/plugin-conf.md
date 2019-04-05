# Dynamic Plugins Configuration

## Table of Contents

   * [Dynamic Plugins Configuration](#dynamic-plugins-configuration)
      * [Table of Contents](#table-of-contents)
      * [Motivation](#motivation)
      * [Function Detail](#function-detail)
      * [Feature Interaction](#feature-interaction)
         * [ConfigMap](#configmap)
      * [Reference](#reference)

Created by [gh-md-toc](https://github.com/ekalinin/github-markdown-toc)

## Motivation

There are several plugins and actions in `kube-batch` right now; the users may want to only enable part of plugins and actions. This document is going to introduce dynamic plugins configuration, so the users can configure `kube-batch` according to their
scenario on the fly.

## Function Detail

The following YAML format will be introduced for dynamic plugin configuration:

```yaml
actions: "list_of_action_in_order"
tiers:
- plugins:
  - name: "plugin_1"
    disableJobOrder: true
  - name: "plugin_2"
- plugins:
  - name: "plugin_3"
    disableJobOrder: true
```

The `actions` is a list of actions that will be executed by `kube-batch` in order, separated
by commas. Refer to the [tutorial](https://github.com/kubernetes-sigs/kube-batch/issues/434) for
the list of supported actions in `kube-batch`. Those actions will be executed in order, although
the "order" maybe incorrect; the `kube-batch` does not enforce that.

The `tiers` is a list of plugins that will be used by related actions, e.g. `allocate`. It includes
several tiers of plugin list by `plugins`; if it fits plugins in high priority tier, the action will not
go through the plugins in lower priority tiers. In each tier, it's considered passed if all the plugins are
fitted in `plugins.names`.

The `options` defines the detail behaviour of each plugins, e.g. whether preemption is enabled. If not
specific, `true` is default value. For now, `preemptable`, `jobOrder`, `taskOrder` are supported.

Takes following example as demonstration:

1. The actions `"reclaim, allocate, backfill, preempt"` will be executed in order by `kube-batch`
1. `"priority"` has higher priority than `"gang, drf, predicates, proportion"`; a job with higher priority
will preempt other jobs, although it's already allocated "enough" resource according to `"drf"`
1. `"tiers.plugins.drf.disableTaskOrder"` is `true`, so `drf` will not impact task order phase/action

```yaml
actions: "reclaim, allocate, backfill, preempt"
tiers:
- plugins:
  - name: "priority"
  - name: "gang"
- plugins:
  - name: "drf"
    disableTaskOrder: true
  - name: "predicates"
  - name: "proportion"
```

## Feature Interaction

### ConfigMap

`kube-batch` will read the plugin configuration from command line argument `--scheduler-conf`; user can
use `ConfigMap` to acesss the volume of `kube-batch` pod during deployment.

## Reference

* [Add preemption by Job priority](https://github.com/kubernetes-sigs/kube-batch/issues/261)
* [Support multiple tiers for Plugins](https://github.com/kubernetes-sigs/kube-batch/issues/484)
