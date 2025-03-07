# Build image and plugin

## Use `musl-libc` build image and plugin

Because the default `vc-scheduler` base image is `alpine`, which only has `musl-libc`, so we should use `musl-gcc` to
build the plugin.

### Build the image and plugin in `Docker`(Recommended):

Please run this command at the root path of the project.
```bash
docker build -t volcanosh/vc-scheduler:custom-plugins -f example/custom-plugin/Dockerfile .
```
And then replace the image and set `--plugins-dir=plugins` parameter in `vc-scheduler` deployment yaml file.

### Build the image and plugin locally:

Please run this command at the root path of the project.
```bash
# install musl
wget http://musl.libc.org/releases/musl-latest.tar.gz
mkdir musl-latest && tar -xf musl-latest.tar.gz -C musl-latest --strip-components=1 && cd musl-latest
./configure
make && sudo make install

# build plugin .so file
CC=/usr/local/musl/bin/musl-gcc CGO_ENABLED=1 go build -buildmode=plugin -ldflags '-linkmode=external' \
    -o example/custom-plugin/magic.so example/custom-plugin/magic.go

# build vc scheduler binary
SUPPORT_PLUGINS=yes make vc-scheduler
cat << EOF > Dockerfile
FROM alpine:latest
COPY _output/bin/vc-scheduler /vc-scheduler
COPY example/custom-plugin/magic.so /plugins/magic.so
ENTRYPOINT ["/vc-scheduler"]
EOF

# build vc scheduler image
docker build -t volcanosh/vc-scheduler:custom-plugins .
```
And then replace the image and set `--plugins-dir=plugins` parameter in `vc-scheduler` deployment yaml file.

## Use `gnu-libc` build plugin

If you want to use `ubuntu` as base image, you can use `gnu-libc` to build the plugin. Since most Linux OS have `gnu-libc`,
you can just build the plugin in local.

```bash
# default CC is gcc
CGO_ENABLED=1 go build -buildmode=plugin magic.go
```
