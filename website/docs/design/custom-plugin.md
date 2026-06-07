# Custom Plugin

## Background

Until now, plugins like `binpack`, `drf`, `gang` are provided by official. But some users may want to implement the
plugin by themselves. So if scheduler can dynamically load plugins, that will make it more flexible when handling
different business scenarios.

## How to build a plugin

### 1. Build pluginable scheduler

#### A. `alpine`

If use `alpine` as base docker image, you should use `musl-gcc` to build scheduler.

```bash
# install musl
wget http://musl.libc.org/releases/musl-1.2.1.tar.gz
tar -xf musl-1.2.1.tar.gz && cd musl-1.2.1
./configure
make && sudo make install

# build scheduler
make images CC=/usr/local/musl/bin/musl-gcc SUPPORT_PLUGINS=yes
```

If use `ubuntu` as base docker image

```bash
make images SUPPORT_PLUGINS=yes
```

### 2. Coding

```go
// magic.go

package main // note!!! package must be named main

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

### 3. Build the plugin to .so

#### A. Use musl-libc build plugin

Because the default `vc-scheduler` base image is `alpine`, which only has `musl-libc`, so we should use `musl-gcc` to
build the plugin.

```bash
docker run -v `pwd`:/work golang:1.14-alpine sh -c "cd /work && apk add musl-dev gcc && go build -buildmode=plugin magic.go"
```

Or build the plugin in local.

```bash
# install musl
wget http://musl.libc.org/releases/musl-1.2.1.tar.gz
tar -xf musl-1.2.1.tar.gz && cd musl-1.2.1
./configure
make && sudo make install

# build plugin
CC=/usr/local/musl/bin/musl-gcc CGO_ENABLED=1 go build -o plugins/magic.so -buildmode=plugin magic.go
```

#### B. Use gnu-libc build plugin

If want to use `ubuntu` as base image, you can use `gnu-libc` to build the plugin. Since most Linux OS have `gnu-libc`,
you can just build the plugin in local.

```bash
# default CC is gcc
CGO_ENABLED=1 go build -o plugins/magic.so -buildmode=plugin magic.go
```

### 4. Add plugins into container

Your can build your docker image

```dockerfile
#Dockerfile
FROM volcanosh/vc-scheduler:latest

COPY plugins plugins
```

```
docker build -t volcanosh/vc-scheduler:magic-plugins .
```



Or just use `pvc` to mount these plugins

### 4. Specify deployment
```yaml
...
    containers:
    - name: volcano-scheduler
      image: volcanosh/vc-scheduler:magic-plugins
      args:
       - --logtostderr
       - --scheduler-conf=/volcano.scheduler/volcano-scheduler.conf
       - --enable-healthz=true
       - --enable-metrics=true
       - -v=3
       - --plugins-dir=plugins  # specify plugins dir path
       - 2>&1
```

### 5. Update volcano-scheduler-configmap

Add your custom plugin name in configmap

```
kubectl edit cm volcano-scheduler-configmap -n volcano-system
```



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
