### 一、 Argo Workflows的工作流特征以及在Volcano上已有的整合

Argo Workflows简称Argo， 是一个容器原生的工作流/流水线引擎，Argo工作流以CRD形式实现。它除了定义要执行的工作流，还会存储工作流程的状态。

Argo工作流的每一个步骤都是一个容器。多步骤的工作流建模为任务的序列，或者基于dag来捕获任务之间的依赖。其并行能力让计算密集型任务，例如机器学习、数据处理，可以在有限的时间内完成。Argo工作流可以用来运行CI/CD流水线。

Argo的templates主要分为两类，用来定义Argo的工作流特征：

- 定义具体的工作流

  - Container（是最常用的模版类型，调度一个container,模版规范和k8s的容器规范相同）
  - Script（作为container的另一种包装形式，其定义方法与container相同，只是增加了source字段用于自定义脚本）
  - Resource（用于直接在k8s集群上执行集群资源操作，可以get, create,apply,delete,replace,patch集群资源。**目前volcano 就是主要利用argo里面的resource template来创建 Volcano Job**）
  - Suspend（用于暂定一段时间，也可以手动恢复）

- 调用其他模版提供并行机制

  - Steps（通过一系列的步骤来定义任务，结构是 “list of lites”，外部列表顺序执行，内部列表并行执行），其中也支持条件语句与循环语句。
  - Dag（主要用于定义任务的依赖关系，可以设置开始特定任务之前，必须完成其他任务，没有依赖关系的任务将立即执行）

  **注：目前volcano支持的多task的job，有一些线性依赖就选用step工作流，多任务的前后依赖关系则用dag工作流。**

以volcano github官网上volcano/example/integrations/argo/10-job-step.yaml与volcano/example/integrations/argo/10-job-step.yaml/20-job-DAG.yaml为例，提交这两个Workflow，在控制台上查看其执行状态为：

![tdmsolution](./images/workflow1.png)

![tdmsolution](./images/workflow2.png)

### 二、 最新版本中Argo的新特性

在2021.08.20 最新发布的Argo Workflows V3.2新增了几个特性，其中比较典型的有：

#### 1.HTTP Template

HTTP  template类似于指定DAG，steps，container。HTTP template是一种可以执行HTTP请求的template。之前可能需要启动一个pod来发出HTTP请求，引入HTTP template以后将工作流与外部系统集成，不再需要为额外专门启动一个pod。

同时，v3.2引入了`Agent`体系结构，在单个pod中执行多个HTTP模板，提高了性能和资源利用率。`WorkflowTaskSet` CRD 用于workflow 主控制器和`Agent`之间的数据交换。

详细如下：

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: http-template-
spec:
  entrypoint: main
  templates:
    - name: main
      steps:
        - - name: good
            template: http
            arguments:
              parameters: [{name: url, value: "https://raw.githubusercontent.com/argoproj/argo-workflows/4e450e250168e6b4d51a126b784e90b11a0162bc/pkg/apis/workflow/v1alpha1/generated.swagger.json"}]
    - name: http
      inputs:
        parameters:
          - name: url
      http:
       # url: http://dummy.restapiexample.com/api/v1/employees
       url: "{{inputs.parameters.url}}"
```

#### 2.Inline Template

可以在dag和steps中内联其他模版，但是只能内联一次，并且在dag中内联dag是无效的。

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: dag-inline-
spec:
  entrypoint: main
  templates:
    - name: main
      dag:
        tasks:
          - name: a
            inline:
              container:
                image: argoproj/argosay:v2
```

#### 3.Conditional Based Retry Strategy

v3.2增强了现有的RetryStrategy以支持基于条件的RetryStrategy。如果条件满足，将触发重试。

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: retry-script-
spec:
  entrypoint: main
  templates:
    - name: main
      steps:
        - - name: safe-to-retry
            template: safe-to-retry
        - - name: retry
            template: retry-script
            arguments:
              parameters:
                - name: safe-to-retry
                  value: "{{steps.safe-to-retry.outputs.result}}"

    - name: safe-to-retry
       script:
         image: python:alpine3.6
         command: ["python"]
         source: |
           print("true")

    - name: retry-script
       inputs:
         parameters:
             - name: safe-to-retry
       retryStrategy:
         limit: "3"
         # Only continue retrying if the last exit code is greater than 1 and the input parameter is true
         expression: "asInt(lastRetry.exitCode) > 1 && {{inputs.parameters.safe-to-retry}} == true"
       script:
         image: python:alpine3.6
         command: ["python"]
         # Exit 1 with 50% probability and 2 with 50%
        source: |
          import random;
          import sys;
          exit_code = random.choice([1, 2]);
          sys.exit(exit_code)
```

### 三、 volcano后续可以借鉴的部分

##### 1.volcano可以借鉴 Argo中的Workflow中允许使用变量

在Argo的Workflow中是允许使用变量的，比如变量还可以进行一些函数运算，主要有：

- filter：过滤
- asInt：转换为Int
- asFloat：转换为Float
- string：转换为String
- toJson：转换为Json	

在高性能计算场景下，如动画渲染、基因计算、气象计算等场景，使用并行计算框架MPI，主从线程之间有着明确的分工流程，并且涉及到大量的科学计算，此特性可以考虑融入。



##### 2.argo的制品库mino

例如下面的官方给出的例子：

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: artifact-passing-
spec:
  entrypoint: artifact-example
  templates:
  - name: artifact-example
    steps:
    - - name: generate-artifact
        template: whalesay
    - - name: consume-artifact
        template: print-message
        arguments:
          artifacts:
          - name: message
            from: "{{steps.generate-artifact.outputs.artifacts.hello-art}}"

  - name: whalesay
    container:
      image: docker/whalesay:latest
      command: [sh, -c]
      args: ["sleep 1; cowsay hello world | tee /tmp/hello_world.txt"]
    outputs:
      artifacts:
      - name: hello-art
        path: /tmp/hello_world.txt

  - name: print-message
    inputs:
      artifacts:
      - name: message
        path: /tmp/message
    container:
      image: alpine:latest
      command: [sh, -c]
      args: ["cat /tmp/message"]
```

其分为两步：

- 首先生成制品
- 然后获取制品

在volcano的机器学习作业的场景下，涉及到频繁地ps与worker之间的信息交互，如果传输的数量很大，阻塞的通信方式可能会影响任务的性能，因此可以考虑使用中间件的异步通信。此时可以融入Argo的artifacts。



#### 3.argo新增的HTTP Template and Agent

pod之间的消息传递是不可避免的，在使用到基于HTTP的网络传输中，可以考虑融入Argo的HTTP template，在单个pod中执行多个HTTP模板，提高性能和资源利用率。



