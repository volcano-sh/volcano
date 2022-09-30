# How to Define Custom Volcano Job

## Background
It is strongly recommended to leverage `volcano job`, the native API for Volcano, in the high performance scenarios such
as AI, Big Data and HPC. Although users can still make use of CRDs such as `Spark Job` and `TensorFlow Job` to integrate
with the scheduling abilities in Volcano, it wil lose the chance to experience the whole of Volcano.

## Key Fields
* Volcano Job

| ID  | Field Name | Type       | Required | Description                                                                                                                                |
|-----|------------|------------|----------|--------------------------------------------------------------------------------------------------------------------------------------------|
| 1   | kind       | string     | Y        | CRD kind. The only valid value is `Job`.                                                                                                   |
| 2   | apiVersion | string     | Y        | CRD api version. The current valid value is `batch.volcano.sh/v1alpha1`.                                                                   |
| 3   | metadata   | ObjectMeta | Y        | CRD meta data. Follow the definition [v1.ObjectMeta](https://github.com/kubernetes/apimachinery/tree/master/pkg/apis/meta).                |
| 4   | spec       | JobSpec    | N        | Volcano Job Specification. Follow the definition [JobSpec](https://github.com/volcano-sh/apis/blob/master/pkg/apis/batch/v1alpha1/job.go). |
| 5   | status     | JobStatus  | N        | Volcano Job Status. Follow the definition [JobStatus](https://github.com/volcano-sh/apis/blob/master/pkg/apis/batch/v1alpha1/job.go).      |

* Job Specification

| ID  | Field Name              | Type     | Required | Description                                                                                                                                                                                                        |
|-----|-------------------------|----------|----------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 1   | schedulerName           | string   | N        | Default scheduler for all pods under the job. Default value is `volcano`.                                                                                                                                          |
| 2   | minAvailable            | int      | N        | The minimal number of pods that ensure the success of the job. If not satisfied, all the pods under the job will not been scheduled.                                                                               |
| 3   | volumes                 | array    | N        | Volumes mounted on the pods under the job. Follows the definition [VolumeSpec](https://github.com/volcano-sh/apis/blob/master/pkg/apis/batch/v1alpha1/job.go).                                                     |
| 4   | tasks                   | array    | N        | Different roles in the job. For example, a spark job consists of two tasks: driver and executor. Follows the definition [TaskSpec](https://github.com/volcano-sh/apis/blob/master/pkg/apis/batch/v1alpha1/job.go). |
| 5   | policies                | array    | N        | Default lifecycle of task. Follows the definition [JobStatus](https://github.com/volcano-sh/apis/blob/master/pkg/apis/batch/v1alpha1/job.go).                                                                      |
| 6   | plugins                 | array    | N        | xx                                                                                                                                                                                                                 |
| 7   | runningEstimate         | Duration | N        | xx                                                                                                                                                                                                                 |
| 8   | queue                   | string   | N        | xx                                                                                                                                                                                                                 |
| 9   | maxRetry                | int      | N        | xx                                                                                                                                                                                                                 |
| 10  | ttlSecondsAfterFinished | int      | N        | xx                                                                                                                                                                                                                 |
| 11  | priorityClassName       | string   | N        | xx                                                                                                                                                                                                                 |
| 12  | minSuccess              | int      | N        | xx                                                                                                                                                                                                                 |


## Example

## Note
