## Configuration

The following are the list configurable parameters of Volcano Chart and their default values.

| Parameter|Description|Default Value|
|----------------|-----------------|----------------------|
|`basic.image_tag_version`| Docker image version Tag | `latest`|
|`basic.controller_image_name`|Controller Docker Image Name|`volcanosh/vk-controllers`|
|`basic.scheduler_image_name`|Scheduler Docker Image Name|`volcanosh/vk-kube-batch`|
|`basic.admission_image_name`|Admission Controller Image Name|`volcanosh/vk-admission`|
|`basic.admission_secret_name`|Volcano Admission Secret Name|`volcano-admission-secret`|
|`basic.scheduler_config_file`|Configuration File name for Scheduler|`kube-batch.conf`|
|`basic.image_pull_secret`|Image Pull Secret|`""`|
|`basic.image_pull_policy`|Image Pull Policy|`IfNotPresent`|
|`basic.admission_app_name`|Admission Controller App Name|`volcano-admission`|
|`basic.controller_app_name`|Controller App Name|`volcano-controller`|
|`basic.scheduler_app_name`|Scheduler App Name|`volcano-scheduler`|

## Updating Helm Package

Any changes made on volcano helm chart should be reflected in Helm Package for users(who don't clone code to install volcano) to get that update, 
helm package can be updated using following process.

```bash
cd installer
helm package chart/volcano

cp volcano-{$version}.tgz helm_package
helm repo index helm_package/ 
```
