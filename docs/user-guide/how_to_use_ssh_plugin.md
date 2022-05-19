# Volcano Job Plugin -- SSH User Guidance

## Background
**SSH Plugin** is designed for the login without password for pods within a volcano job , which is necessary for workloads 
such as [MPI](https://www.open-mpi.org/). It often works with `SVC` plugin.

## Key Points
* If `ssh-key-file-path` is configured, please ensure the private and public keys exist under the target directory.
Suggest keeping default value in most scenarios.
* If `ssh-private-key` or `ssh-public-key` is configured, please ensure the value is correct. Suggest keeping the default
keys in most scenarios.
* Once `SSH` plugin is configured, a secret whose name joins the job name and `-ssh` will be created, which contains
`authorized_keys`/`id_rsa`/`config` and `id_rsa.pub`. It will be mounted to the given path as a volume for all containers
(including initContainers) within the job.
* You can get all the hostnames within the job in `/root/.ssh/config` by default. This file contains the pairs of hostname
and subdomain.
* If `SSH` plugin is configured, you can sign in any other pods in the same job by `ssh hostname` without password.

## Arguments
| ID  | Name                 | Type   | Default Value       | Required | Description                                         | Example                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |
|-----|----------------------|--------|---------------------|----------|-----------------------------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 1   | `ssh-key-file-path`  | String | `/root/.ssh`        | N        | The path used to store ssh private and public keys. | ssh: ["--ssh-key-file-path=/home/user/.ssh"]                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| 2   | `ssh-private-key`    | String | DEFAULT_PRIVATE_KEY | N        | The input string of the private key.                | ssh: ["--ssh-private-key=-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEAyeyZjWDx5Na9bw1f61M4s+QlLT/kyrB37AR2j5Sb/A9hvJak\nLNQQpNC+KVfYNl4jePG+6lwHqye//pcC9+0SWsHWwgaahjMLnAthR2k8JAakNA9x\nV/wHz0YU99OKEetaOuxXpWZPXCHX0zuQO87YbdKzRbgxACirM3Phkwr7XLtQtWZk\nyXG34CQXZQWgBIS1Fl+PlGOpVpOPnWoZPMpbAK74i/Tz4sP8Zhqc6dya1hrbUwY3\nYfMZNYXpaAw7wWVjq8grfs0+Fl3SxHrzTXge2m+eZAZ6iPJ8cX4uYKxi0ZmxpM/a\ngI6Mmjq0MU75Vxpq22LaUvHIpOfX5UxhkrsxlwIDAQABAoIBAQDGOuIb6zpNn4rl\nBMpPqamW4LimjX08hrWUHGWQWyIu96LJk1GlOKMGSm8FA1odNZm5WApG5QYaPrG7\na+DcJ/7G3ljIrdbxPBd/n6RmiKcj7ukwuqBY8fFwyKo5CZEYOmagRfldRO1P02Gf\n22+jZ1MNrbWVElf4gfRgVLj0s+lEhFkzhi+QGMmMpjEJnnG98xxVGEvWMw1rnKJm\n3Gi771Gltbg3GuEPs3IeoBgba3EaHmSxJnBivAL4zsO8UUCAXB13cUiXx8qO7y1e\nCSWSenRmK2ugbL6v0co12O0n0pxF9xlJ6fALdRWzpJsFlN3ttkY9N5GrQc/pVjOa\nvqa172RRAoGBAOSAIMNLT6QjgYDk5Z7ZxjNnxH/lMso+cx6bxk9YMKRrw0fDQh8m\ncBAihXhuntCPDGhrzQ+Anqx4jJVDFqac0xBck90a8LmmzD0q72eDTCYPouDWe6DL\nJQAc/HDmIC13sADEXmGW3c0Qn4hjBnMd89ouYj7ZajU2sED2irPPc/HLAoGBAOI5\nruL4Q0FarGrP3a9z9EDrVJsK2OfSTaJ7rhZ+uvB838svbHU+4mEYPhx4PCwvrYyi\nFn4hyau003ZmLc1qTABjmwcO/PPiYyoRHJDUIIhiIyIL+id/G53uG2eTzqYtU6uS\nnAIB2rKwwhU8ek+zbJBLu5uxuxlf4mdZITdkwtXlAoGBALH3RQ02A9JgQQYFwP2G\nucLhx/6goX05RGoLg1na4w+8Sr0Cy+X9BvzaFkAlUBY5w700cOLpFyxXO48pUGP1\n8sFkiVmFGQZPbfUaEpn5ff6K4R3ijyk97xR2fvrjkR44gOEoECZL3XZQwx/zmFti\nccF1rNksdnb5oC8IliDTq4cfAoGANyy6asECJj5nLuXju5ccS3kZ+XZ70I6KQMbJ\nftMJ5P2P146JdU8RB31SKL9qbZxzR4mA0uKKvUYtDQN+yErUnoOsm9wb9Z+RcAEc\nZnZWOO02hGdHa7qkkbAxHuH91KnZbk8jnZm2LT7PFz7Y1fd80vSlnSOL7nRkU7B5\nWXlJy8ECgYA4g0wc0Jq8c1Q0FulMkOQqYRDXaDo34987L+mZ70i/RtdkKjK/IKJ9\n18UDCyEaDPD0BWBJGPejZkY8UD6FBG/5k7wNIbT7hHLRSRlw4iRmVX2hRVXrXzD8\nvc86Qyg2iG0JqkMAvRdH40amPKp5bW4VcfcvQo4TSsI972u12rgwtg==\n-----END RSA PRIVATE KEY-----\n"] |
| 3   | `ssh-public-key`     | String | DEFAULT_PUBLIC_KEY  | N        | The input string of the public key.                 | ssh: ["--ssh-public-key=ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQDJ7JmNYPHk1r1vDV/rUziz5CUtP+TKsHfsBHaPlJv8D2G8lqQs1BCk0L4pV9g2XiN48b7qXAerJ7/+lwL37RJawdbCBpqGMwucC2FHaTwkBqQ0D3FX/AfPRhT304oR61o67FelZk9cIdfTO5A7ztht0rNFuDEAKKszc+GTCvtcu1C1ZmTJcbfgJBdlBaAEhLUWX4+UY6lWk4+dahk8ylsArviL9PPiw/xmGpzp3JrWGttTBjdh8xk1heloDDvBZWOryCt+zT4WXdLEevNNeB7ab55kBnqI8nxxfi5grGLRmbGkz9qAjoyaOrQxTvlXGmrbYtpS8cik59flTGGSuzGX root@aiplatform"]                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |

Note:
* `DEFAULT_PRIVATE_KEY` and `DEFAULT_PUBLIC_KEY` are not fully listed for they are too long. Please refer to the examples
behind for cases.
* Volcano is not responsible for the validation of `ssh-key-file-path`. So please guarantee it correct yourself.
* Suggest keeping blank and making use of the default values in most scenarios just like the example behind. If that,
Volcano will help generate a pair of keys and finish all the configuration by default.

## Examples
```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: mpi-job
spec:
  minAvailable: 3
  schedulerName: volcano
  plugins:
    ssh: []   ## SSH plugin register
    svc: []
  tasks:
    - replicas: 1
      name: mpimaster
      template:
        spec:
          containers:
            - command:
                - /bin/bash
                - -c
                - |
                  mkdir -p /var/run/sshd; /usr/sbin/sshd;
                  MPI_HOST=`cat /etc/volcano/mpiworker.host | tr "\n" ","`;
                  sleep 10;
                  mpiexec --allow-run-as-root --host ${MPI_HOST} -np 2 --prefix /usr/local/openmpi-3.1.5 python /tmp/gpu-test.py;
                  sleep 3600;
              image: lyd911/mindspore-gpu-example:0.2.0
              name: mpimaster
              ports:
                - containerPort: 22
                  name: mpijob-port
              workingDir: /home
          restartPolicy: OnFailure
    - replicas: 2
      name: mpiworker
      template:
        spec:
          containers:
            - command:
                - /bin/bash
                - -c
                - |
                  mkdir -p /var/run/sshd; /usr/sbin/sshd -D; 
              image: lyd911/mindspore-gpu-example:0.2.0
              name: mpiworker
              resources:
                limits:
                  nvidia.com/gpu: "1"
              ports:
                - containerPort: 22
                  name: mpijob-port
              workingDir: /home
          restartPolicy: OnFailure
```
Note:
* This example will create a mpi job with 1 `master` and 2 `workers`.
* Because the `SVC` plugin is enabled, you can get all the hosts in any pod by environment variables. Also, you can get
the hosts in `/root/.ssh/config` if you use the default ssh configuration.
```
[root@mpi-job-master-0 /]# cat /root/.ssh/config
StrictHostKeyChecking no
UserKnownHostsFile /dev/null
Host mpi-job-mpimaster-0
  HostName mpi-job-mpimaster-0.mpi-job
Host mpi-job-mpiworker-0
  HostName mpi-job-mpiworker-0.mpi-job
Host mpi-job-mpiworker-1
  HostName mpi-job-mpiworker-1.mpi-job
```
* You can sign in other hosts in `master` pod as follows.
```
[root@mpi-job-master-0 /]# ssh mpi-job-mpiworker-0
Warning: Permanently added 'mpi-job-mpiworker-0.mpi-job,X.X.X.X' (ECDSA) to the list of known hosts.
Welcome to Ubuntu 18.04.3 LTS (GNU/Linux 3.10.0-1160.36.2.el7.x86_64 x86_64)

 * Documentation:  https://help.ubuntu.com
 * Management:     https://landscape.canonical.com
 * Support:        https://ubuntu.com/advantage

This system has been minimized by removing packages and content that are
not required on a system that users do not log into.

To restore this content, you can run the 'unminimize' command.
Last login: Thu Apr 14 07:19:05 2022 from 10.244.0.67
root@mpi-job-mpiworker-0:~# 
```
## Note
* Please ensure `sshd` service is available in all containers.