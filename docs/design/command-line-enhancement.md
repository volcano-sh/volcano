# `vcctl` Command Line Enhancement

[@jiangkaihua](jiangkaihua1@huawei.com); Dec. 16, 2019

## Catalog
- [`vcctl` Command Line Enhancement](#vcctl-command-line-enhancement)
  - [Catalog](#catalog)
  - [Functions of `vcctl`](#functions-of-vcctl)
    - [Command `vcctl job`](#command-vcctl-job)
    - [Command `vcctl queue`](#command-vcctl-queue)
  - [`vcctl` vs. Slurm Command Line](#vcctl-vs-slurm-command-line)
  - [New Format of Volcano Command Line](#new-format-of-volcano-command-line)
    - [For Common User](#for-common-user)
      - [`vsub` submit via file](#vsub-submit-via-file)
    - [For Administrator](#for-administrator)
  - [Reference](#reference)

## Functions of `vcctl`
`vcctl` is the command line of [volcano](https://github.com/volcano-sh/volcano). The main functions are listed below:

### Command `vcctl job`
| Command Format | Usage |
| - | - |
| `vcctl job delete -N <job_name> -n <namespace>` | delete a job |
| `vcctl job list -S <scheduler> -n <namespace>` | list job info |
| `vcctl job resume -N <job_name> -n <namespace>` | resume a job |
| `vcctl job run -f <yaml_file> -i <image> -L <resource_limit> -m <min_available> -N <job_name> -n <namespace> -r <replicas> -R <resource_requeset> -S <scheduler>` | run job by parameters from the command line |
| `vcctl job suspend -N <job_name> -n <namespace>` | suspend a job |
| `vcctl job view -N <job_name> -n <namespace>` | show a job info |

### Command `vcctl queue`
| Command Format | Usage |
| - | - |
| `vcctl queue create -n <queue_name> -w <weight>` | create a queue |
| `vcctl queue delete -n <queue_name>` | delete a queue |
| `vcctl queue get -n <queue_name>` | get a queue |
| `vcctl queue list ` | list all the queue |
| `vcctl queue operate -a <open/close/update> -n <queue_name> -w <weight>` | operate a queue |

## `vcctl` vs. Slurm Command Line
The similar Slurm command lines are listed below:

| `vcctl` Function | Similar Slurm Command Line |
| - | - |
| `vcctl job run -f <yaml_file>` | `sbatch <job_file>` |
| `vcctl job run -N <job_name>` | `srun -J <job_name> ` |
| `vcctl job delete -N <job_name> -n <namespace>` | `scancel <job_id> / -n <job_name> -u <user>` |
| `vcctl job suspend -N <job_name> -n <namespace>` | `scontrol suspend <job_id>` |
| `vcctl job resume -N <job_name> -n <namespace>` | `scontrol resume <job_id>` |
| `vcctl job view -N <job_name> -n <namespace>` | `scontrol show job <job_id>` |
| `vcctl job list --all-namespaces` | `scontrol show job` |
| `vcctl job list -n <namespace>` | `squeue -u <user>` |
| `vcctl queue create -n <queue_name> -w <weight>` | `scontrol create PartitionName=<partition_name>` |
| `vcctl queue delete -n <queue_name>` | `scontrol delete PartitionName=<partition_name>` |
| `vcctl queue get -n <queue_name>` | `squeue -p <partition_name> & scontrol show partition <partition_name>` |
| `vcctl queue list ` | `squeue -a & scontrol show partition` |
| `vcctl queue operate -a <open/close/update> -n <queue_name> -w <weight>` | no similar commands |

## New Format of Volcano Command Line
### For Common User
| Old Format | New Format |
| - | - |
| `vcctl job run -N <job_name>` | `vsub -j/--job-name <job_file>` |
| `vcctl job delete -N <job_name> -n <namespace>` | `vcancel -n <job_name> -N <namespace>` |
| `vcctl job suspend -N <job_name> -n <namespace>` | `vsuspend -n <job_name> -N <namespace>` |
| `vcctl job resume -N <job_name> -n <namespace>` | `vresume -n <job_name> -N <namespace>` |
| `vcctl job view -N <job_name> -n <namespace>` | `vjobs -n <job_name> -N <namespace>` |
| `vcctl job list -S <scheduler> -n <namespace>` | `vjobs -S <scheduler> -N <namespace>` |
| `vcctl queue get -n <queue_name>` | `vqueues -n <queue_name>` |
| `vcctl queue list ` | `vqueues` |

#### `vsub` submit via file
Command `vsub` can also submit a batch job via `.sh` file, like:
```shell
[user@host]$ vsub test.sh
Submitted batch job test
```
The job file <test.sh> owns a format like:
```shell
#!/bin/bash`

#VSUB jobName test
#VSUB namespace volcano-system
#VSUB queue default
#VSUB schedulerName volcano
#VSUB image busybox
#VSUB replicas 10
#VSUB minAvailable 4
...

echo test.sh start on $(date)
sleep 100
echo test.sh end on $(date)
```


### For Administrator
| Old Format | New Format |
| - | - |
| `vcctl queue create -n <queue_name> -w <weight>` | `vadmin qcreate -n <queue_name> -w <weight>` |
| `vcctl queue delete -n <queue_name>`| `vadmin qcancel -n <queue_name>` |
| `vcctl queue operate -a open -n <queue_name>`| `vadmin qopen -n <queue_name>` |
| `vcctl queue operate -a close -n <queue_name>`| `vadmin qclose -n <queue_name>` |
| `vcctl queue operate -a update -n <queue_name> -w <weight>`| `vadmin qupdate -n <queue_name> -w <weight>` |


operate -a <open/close/update> -n <queue_name> -w <weight>
## Reference
- [Slurm Documentation](https://slurm.schedmd.com/)
- [IBM Platform LSF Command Reference](https://www.ibm.com/support/knowledgecenter/en/SSETD4_9.1.2/lsf_kc_cmd_ref.html)
- [Slurm作业调度系统使用指南](http://hmli.ustc.edu.cn/doc/userguide/slurm-userguide.pdf)
- [SLURM使用基础教程](https://www.hpccube.com/wiki/index.php/SLURM%E4%BD%BF%E7%94%A8%E5%9F%BA%E7%A1%80%E6%95%99%E7%A8%8B)
- [北京大学国际数学中心微型工作站-SLURM 使用参考](http://bicmr.pku.edu.cn/~wenzw/pages/index.html)
