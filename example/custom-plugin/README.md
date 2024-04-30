# Build plugin

## Use `musl-libc` build plugin

Because the default `vc-scheduler` base image is `alpine`, which only has `musl-libc`, so we should use `musl-gcc` to
build the plugin.

### Build the plugin in `Docker`:

```bash
# You may need run this command at the root path of the project, cause this need the go.mod file.
docker run -v `pwd`:/volcano golang:1.20-alpine sh -c "cd /volcano && apk add musl-dev gcc && go build -buildmode=plugin -o example/custom-plugin/magic.so example/custom-plugin/magic.go"
```

### Build the plugin in local:

```bash
# install musl
wget http://musl.libc.org/releases/musl-1.2.1.tar.gz
tar -xf musl-1.2.1.tar.gz && cd musl-1.2.1
./configure
make && sudo make install

# build plugin
CC=/usr/local/musl/bin/musl-gcc CGO_ENABLED=1 go build -buildmode=plugin magic.go
```

## Use `gnu-libc` build plugin

If you want to use `ubuntu` as base image, you can use `gnu-libc` to build the plugin. Since most Linux OS have `gnu-libc`,
you can just build the plugin in local.

```bash
# default CC is gcc
CGO_ENABLED=1 go build -buildmode=plugin magic.go
```
