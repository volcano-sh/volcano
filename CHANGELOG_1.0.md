## v1.0.1

#### Changelog since v1.0.0
- Fix job's scheduler name does not take effect ([#944](https://github.com/volcano-sh/volcano/pull/944), [@hzxuzhonghu](https://github.com/hzxuzhonghu))
- Fix podgroup status and event  ([#951](https://github.com/volcano-sh/volcano/pull/951), [@hzxuzhonghu](https://github.com/hzxuzhonghu))
- Support job scale down to zero ([#945](https://github.com/volcano-sh/volcano/pull/945), [@hzxuzhonghu](https://github.com/hzxuzhonghu))
- Fix queue capability validation failed when some running jobs finished or deleted ([#959](https://github.com/volcano-sh/volcano/pull/959), [@Thor-wl](https://github.com/Thor-wl))

## Volcano v1.0 Release Notes

### 1.0 What's New

**1. GPU Sharing**

Volcano now supports gpu sharing between different pods ([#852](https://github.com/volcano-sh/volcano/pull/852), [@tizhou86](https://github.com/tizhou86), [@hzxuzhonghu](https://github.com/hzxuzhonghu)).

**2. Preempt abd reclaim enhancement**

Volcano is now able to support preempt for batch job ([#738](https://github.com/volcano-sh/volcano/pull/738), [@carmark](https://github.com/carmark)).

**3. Dynamic scale up and down**

Volcano job now supports dynamically scale up and down ([#787](https://github.com/volcano-sh/volcano/pull/787), [@hzxuzhonghu](https://github.com/hzxuzhonghu)).

**4. Support integrate with flink operator**

Users are now able to run flink job with volcano. Follow the [instructions here](https://github.com/GoogleCloudPlatform/flink-on-k8s-operator/blob/master/docs/volcano_integration.md) to make use of the feature. [@hzxuzhonghu](https://github.com/hzxuzhonghu)).

**5. Support DAG job with argo**

Users are now able to run DAG job with volcano. Follow the [instructions here](https://github.com/volcano-sh/volcano/blob/master/example/integrations/argo/README.md) to make use of the feature. [@alcorf-mizar](https://github.com/alcorf-mizar)).

### Other Notable Changes

- Update go version to 1.14 ([#886](https://github.com/volcano-sh/volcano/pull/886), [@hzxuzhonghu](https://github.com/hzxuzhonghu))
- Bump to k8s 1.18 to keep up with kubernetes ([#855](https://github.com/volcano-sh/volcano/pull/855), [@hzxuzhonghu](https://github.com/hzxuzhonghu))
- Add mindspore example ([#845](https://github.com/volcano-sh/volcano/pull/845), [@lyd911](https://github.com/lyd911))
- Add golangCi-lint check ([#799](https://github.com/volcano-sh/volcano/pull/799), [#821](https://github.com/volcano-sh/volcano/pull/821), [#824](https://github.com/volcano-sh/volcano/pull/824), [#825](https://github.com/volcano-sh/volcano/pull/825), [#827](https://github.com/volcano-sh/volcano/pull/827), [#829](https://github.com/volcano-sh/volcano/pull/829), [#830](https://github.com/volcano-sh/volcano/pull/830), [#833](https://github.com/volcano-sh/volcano/pull/833), [#835](https://github.com/volcano-sh/volcano/pull/835), [#841](https://github.com/volcano-sh/volcano/pull/841). [@masihtehrani](https://github.com/masihtehrani) [@daixiang0](https://github.com/daixiang0) [@Thor-wl](https://github.com/Thor-wl))
- Allow specifying admission webhook port ([#832](https://github.com/volcano-sh/volcano/pull/832), [@hzxuzhonghu](https://github.com/hzxuzhonghu))
- Add support for most requested priority ([#831](https://github.com/volcano-sh/volcano/pull/831), [@daixiang0](https://github.com/daixiang0))
- Set pod DNSPolicy to ClusterFirstWithHostNet when hostnetwork set ([#779](https://github.com/volcano-sh/volcano/pull/779), [@hzxuzhonghu](https://github.com/hzxuzhonghu))
- Add bestNodeFn for plugins to select best node of its own ([#790](https://github.com/volcano-sh/volcano/pull/790), [@jiangkaihua](https://github.com/jiangkaihua))
- Add e2e cases for drf ([#905](https://github.com/volcano-sh/volcano/pull/905), [@Thor-wl](https://github.com/Thor-wl))
- Add e2e cases for reclaim ([#898](https://github.com/volcano-sh/volcano/pull/898) [#906](https://github.com/volcano-sh/volcano/pull/906), [@alcorj-mizar](https://github.com/alcorj-mizar))
- Add e2e cases for preempt ([#892](https://github.com/volcano-sh/volcano/pull/892), [@Thor-wl](https://github.com/Thor-wl) )
- Add e2e for queue ([#872](https://github.com/volcano-sh/volcano/pull/872), [@Thor-wl](https://github.com/Thor-wl))
- Add test code for queue controller ([#858](https://github.com/volcano-sh/volcano/pull/779), [@alcorj-mizar](https://github.com/alcorj-mizar))

### Bug Fixes
- Fix panic in controller ([#903](https://github.com/volcano-sh/volcano/pull/903), [@Thor-wl](https://github.com/Thor-wl))
- Fix panic in allocate ([#843](https://github.com/volcano-sh/volcano/pull/843), [@k82cn](https://github.com/k82cn))
- Fix job phase transition time set ([#789](https://github.com/volcano-sh/volcano/pull/789), [@hzxuzhonghu](https://github.com/hzxuzhonghu))
- Fix crd to support job patch ops ([#786](https://github.com/volcano-sh/volcano/pull/786), [@hzxuzhonghu](https://github.com/hzxuzhonghu))