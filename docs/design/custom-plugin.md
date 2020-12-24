# Custom Plugin

## Background

Until now, plugins like `binpack`, `drf`, `gang` are provided by official.
But some users may want to implement the plugin by themselves. So if scheduler can dynamically load plugins, that will make it more flexible when handling different business scenarios.

## How to build a plugin

### 1. Coding

```go
// magic.go

package main  // note!!! package must be named main

import (
    "volcano.sh/volcano/pkg/scheduler/framework"
)

const PluginName = "magic"

type magicPlugin struct {}

func (mp *magicPlugin) Name() string {
    return PluginName
}

func New(arguments framework.Arguments) framework.Plugin {  // `New` is PluginBuilder
    return &magicPlugin{}
}

func (mp *magicPlugin) OnSessionOpen(ssn *framework.Session) {}

func (mp *magicPlugin) OnSessionClose(ssn *framework.Session) {}
```

### 2. Build the plugin to .so

vc-scheduler binary is build by `musl-gcc`, so we also need to build the plugin by `musl-gcc`.

```bash
# install musl-gcc
wget http://musl.libc.org/releases/musl-1.2.1.tar.gz
tar -xf musl-1.2.1.tar.gz && cd musl-1.2.1
./configure
make && sudo make install

# build plugin
CC=/usr/local/musl/bin/musl-gcc go build -buildmode=plugin -o plugins/magic.so magic.go
```

Note the `.so` plugin name must be same with `PluginName`

### 3. Add plugins into container

Your can build your docker image

```dockerfile
FROM volcano.sh/vc-scheduler:${VERSION}

COPY plugins plugins
```

Or just use `pvc` to mount these plugins

### 4. Specify deployment
```yaml
...
    containers:
    - name: volcano-scheduler
      image: volcano.sh/vc-scheduler:${VERSION}
      args:
        - --logtostderr
        - --scheduler-conf=/volcano.scheduler/volcano-scheduler.conf
        - -v=3
        - --plugins-dir=plugins  # specify plugins dir path
        - 2>&1
```

### 5. Update volcano-scheduler-configmap

Add your custom plugin name in configmap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
      - name: magic       # activate your custom plugin
    - plugins:
      - name: drf
      - name: predicates
      - name: proportion
      - name: nodeorder
      - name: binpack
```

## Note

1. Plugins should be rebuilt after volcano source code modified.
2. Plugin package name must be **main**.
