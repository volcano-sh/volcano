# MinSuccess

## Introduction

Sometimes we want to deploy our job with some service tasks (nginx, redis and so on) which will always be in `Running`
state. So how to determine such kind job is completed? Only with `minAvailable` is not enough, because
`minAvailable` only work at **Job pending** and **all tasks Completed/Failed**. It lacks the ability to change the Job
state during the **Job is Running** state, and reclaim the service tasks.

In order to solve this problem, we provide `minSuccess` field which will help the job to determine how many succeed
tasks does it need to change its state to `Completed`, and reclaim the other Running pods.

![](images/min-success-1.png)
![](images/min-success-2.png)