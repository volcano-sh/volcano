# Build plugin

## In docker
Plugin must use `musl-gcc` to build. The best choose is build it in the docker. 

```bash
docker run -v `pwd`:/work golang:1.14-alpine sh -c "cd /work && apk add musl-dev gcc && go build -buildmode=plugin magic.go"
```

## In local
You can also build it in local.

```bash
# install musl
wget http://musl.libc.org/releases/musl-1.2.1.tar.gz
tar -xf musl-1.2.1.tar.gz && cd musl-1.2.1
./configure
make && sudo make install

# build plugin
CC=/usr/local/musl/bin/musl-gcc CGO_ENABLED=1 go build magic.go
```