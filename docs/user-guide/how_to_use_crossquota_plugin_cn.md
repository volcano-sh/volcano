# CrossQuota 插件用户指南

## 简介

CrossQuota 插件是一个强大的 Volcano 调度器插件，专为具有混合 GPU 和 CPU 工作负载的异构集群而设计。它提供两个关键功能：

1. **多资源配额管理**：限制 GPU 节点上非 GPU 任务的资源使用，以确保 GPU 任务有充足的配套资源（CPU、内存等）。

2. **智能节点排序**：根据资源利用率模式对节点进行评分和排序，根据可配置策略优化任务放置。

本指南将引导您有效地设置和使用 CrossQuota 插件。

## 功能特性

- ✅ 多资源配额支持（CPU、内存、临时存储、大页内存、自定义资源）
- ✅ 灵活的配额配置（绝对值和百分比）
- ✅ 通过注解实现节点特定配额覆盖
- ✅ 两种评分策略：最高利用率和最低利用率
- ✅ 可配置的资源权重以实现精细控制
- ✅ 通过注解实现 Pod 级别的策略选择
- ✅ 自动 GPU 节点和 CPU Pod 检测
- ✅ 基于标签的 Pod 过滤以实现选择性配额执行
- ✅ 向后兼容仅 CPU 配置

## 前提条件

- Volcano v1.14.0 或更高版本
- 具有 GPU 节点的 Kubernetes 集群
- 基本了解 Kubernetes 资源管理

## 环境设置

### 安装 Volcano

如果您还没有安装 Volcano，请参考[安装指南](https://github.com/volcano-sh/volcano/blob/master/installer/README.md)。

### 启用 CrossQuota 插件

编辑 Volcano 调度器配置：

```bash
kubectl edit configmap volcano-scheduler-configmap -n volcano-system
```

将 `crossquota` 插件添加到调度器配置中：

```yaml
kind: ConfigMap
apiVersion: v1
metadata:
  name: volcano-scheduler-configmap
  namespace: volcano-system
data:
  volcano-scheduler.conf: |
    actions: "enqueue, allocate, backfill"
    tiers:
    - plugins:
      - name: priority
      - name: gang
      - name: conformance
    - plugins:
      - name: drf
      - name: predicates
      - name: crossquota
        arguments:
          # 配置 GPU 资源模式（支持正则表达式）
          gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
          
          # 指定要进行配额控制的资源
          quota-resources: "cpu,memory"
          
          # 设置配额限制（绝对值）
          quota.cpu: "32"
          quota.memory: "64Gi"
          
          # 节点排序配置
          crossQuotaWeight: 10
          weight.cpu: 10
          weight.memory: 1
      - name: nodeorder
      - name: binpack
```

### 重启 Volcano 调度器

更新配置后，重启调度器：

```bash
kubectl rollout restart deployment volcano-scheduler -n volcano-system
```

## 配置指南

### 基本配置

#### GPU 资源检测

配置 GPU 资源模式以识别 GPU 节点：

```yaml
gpu-resource-names: "nvidia.com/gpu,amd.com/gpu"
```

支持正则表达式模式以增加灵活性：

```yaml
gpu-resource-names: ".*\\.com/gpu"  # 匹配 nvidia.com/gpu、amd.com/gpu 等
```

#### 配额资源

指定要控制哪些资源：

```yaml
quota-resources: "cpu,memory"
```

支持标准和自定义资源：

```yaml
quota-resources: "cpu,memory,ephemeral-storage,hugepages-1Gi"
```

#### 配额限制

设置绝对配额：

```yaml
quota.cpu: "32"           # 32 个 CPU 核心
quota.memory: "64Gi"      # 64 GiB 内存
```

或基于百分比的配额：

```yaml
quota-percentage.cpu: "50"      # 节点 CPU 的 50%
quota-percentage.memory: "75"   # 节点内存的 75%
```

### 节点排序配置

#### 插件权重

控制插件在调度决策中的整体影响：

```yaml
crossQuotaWeight: 10  # 默认值：10，设置为 0 禁用节点排序
```

#### 资源权重

为不同资源分配重要性：

```yaml
weight.cpu: 10      # CPU 高重要性
weight.memory: 1    # 内存低重要性
```

权重越高意味着该资源对最终分数的影响越大。

### 节点特定配置

使用注解为特定节点覆盖全局配额：

```bash
kubectl annotate node gpu-node-1 \
  volcano.sh/crossquota-cpu="48" \
  volcano.sh/crossquota-memory="96Gi"
```

或使用百分比覆盖：

```bash
kubectl annotate node gpu-node-1 \
  volcano.sh/crossquota-percentage-cpu="75" \
  volcano.sh/crossquota-percentage-memory="80"
```

### Pod 级别配置

为单个 Pod 指定评分策略：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-cpu-pod
  annotations:
    volcano.sh/crossquota-scoring-strategy: "most-allocated"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: nginx
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

可用策略：
- `most-allocated`：优先选择利用率较高的节点（默认）
- `least-allocated`：优先选择利用率较低的节点

### 标签选择器配置

使用标签选择器来控制哪些 Pod 受配额执行约束：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "32"
    quota.memory: "64Gi"
    # 仅对具有这些标签的 Pod 应用配额
    label-selector:
      matchLabels:
        quota-controlled: "true"
        team: "data-science"
      matchExpressions:
        - key: environment
          operator: In
          values:
            - production
            - staging
```

**支持的操作符**：
- `In`：标签值必须在指定列表中
- `NotIn`：标签值不能在指定列表中
- `Exists`：标签键必须存在（值无关紧要）
- `DoesNotExist`：标签键不能存在

**Pod 标签示例**：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: quota-controlled-pod
  labels:
    quota-controlled: "true"
    team: "data-science"
    environment: "production"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: my-app:latest
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

## 使用示例

### 示例 1：基本多资源配额

**场景**：限制 GPU 节点上 CPU Pod 的 CPU 和内存使用。

**配置**：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "32"
    quota.memory: "64Gi"
```

**结果**：
- GPU 节点上的 CPU Pod 不能超过 32 个 CPU 核心
- GPU 节点上的 CPU Pod 不能超过 64 GiB 内存
- GPU Pod 可以使用完整的节点资源

### 示例 2：基于百分比的配额

**场景**：为 GPU 任务保留 50% 的资源。

**配置**：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota-percentage.cpu: "50"
    quota-percentage.memory: "50"
```

**结果**：
- CPU Pod 最多可以使用每个 GPU 节点 50% 的 CPU
- CPU Pod 最多可以使用每个 GPU 节点 50% 的内存
- 配置自动适应不同的节点大小

### 示例 3：资源整合策略

**场景**：将批处理作业整合到更少的节点上，为大型 GPU 任务保留容量。

**配置**：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "32"
    quota.memory: "64Gi"
    crossQuotaWeight: 10
    weight.cpu: 10
    weight.memory: 1
```

**Pod 注解**：

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: batch-job
spec:
  template:
    metadata:
      annotations:
        volcano.sh/crossquota-scoring-strategy: "most-allocated"
    spec:
      schedulerName: volcano
      containers:
      - name: worker
        image: batch-processor:latest
        resources:
          requests:
            cpu: "4"
            memory: "8Gi"
```

**结果**：
- 批处理作业优先选择 CPU/内存利用率较高的节点
- 作业整合到更少的节点上
- 更多节点可用于大型 GPU 任务

### 示例 4：资源分布策略

**场景**：均匀分布长期运行的服务以避免热点。

**配置**：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota-percentage.cpu: "60"
    quota-percentage.memory: "60"
    crossQuotaWeight: 10
    weight.cpu: 10
    weight.memory: 1
```

**Pod 注解**：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: web-service
  annotations:
    volcano.sh/crossquota-scoring-strategy: "least-allocated"
spec:
  schedulerName: volcano
  containers:
  - name: nginx
    image: nginx:latest
    resources:
      requests:
        cpu: "2"
        memory: "4Gi"
```

**结果**：
- 服务优先选择利用率较低的节点
- 在 GPU 节点间均匀分布
- 避免资源热点

### 示例 5：节点特定配额

**场景**：不同的 GPU 节点类型需要不同的配额。

**节点配置**：

```bash
# 高端 GPU 节点 - 为 GPU 任务保留更多资源
kubectl annotate node gpu-node-a100 \
  volcano.sh/crossquota-cpu="16" \
  volcano.sh/crossquota-memory="32Gi"

# 标准 GPU 节点 - 允许更多 CPU 任务分配
kubectl annotate node gpu-node-v100 \
  volcano.sh/crossquota-cpu="48" \
  volcano.sh/crossquota-memory="96Gi"
```

**调度器配置**：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    # 默认配额（由节点注解覆盖）
    quota.cpu: "32"
    quota.memory: "64Gi"
```

**结果**：
- A100 节点为 GPU 任务保留更多资源
- V100 节点允许更多 CPU 任务分配
- 灵活的每节点配置

### 示例 6：自定义资源

**场景**：控制 GPU 节点上的大页内存使用。

**配置**：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory,hugepages-1Gi"
    quota.cpu: "32"
    quota.memory: "64Gi"
    quota.hugepages-1Gi: "8Gi"
    weight.cpu: 10
    weight.memory: 1
    weight.hugepages-1Gi: 5
```

**结果**：
- CPU Pod 限制为 8 GiB 的 1Gi 大页内存
- 大页内存使用在节点评分中被考虑
- 全面的资源控制

### 示例 7：基于标签的选择性配额执行

**场景**：仅对开发和测试工作负载应用配额，允许生产工作负载使用完整资源。

**配置**：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "16"
    quota.memory: "32Gi"
    label-selector:
      matchExpressions:
        - key: environment
          operator: In
          values:
            - development
            - testing
        - key: priority
          operator: NotIn
          values:
            - high
            - critical
```

**开发 Pod**（受配额控制）：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: dev-pod
  labels:
    environment: "development"
    priority: "normal"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: dev-app:latest
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

**生产 Pod**（不受配额控制）：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: prod-pod
  labels:
    environment: "production"
    priority: "high"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: prod-app:latest
    resources:
      requests:
        cpu: "8"
        memory: "16Gi"
```

**结果**：
- 开发和测试 Pod 限制为 16 个 CPU 核心和 32 GiB 内存
- 生产 Pod 可以使用完整的节点资源
- 基于工作负载特征的灵活策略执行

### 示例 8：基于团队的多租户配额

**场景**：不同团队在共享 GPU 节点上有不同的配额限制。

**配置**：

```yaml
- name: crossquota
  arguments:
    gpu-resource-names: "nvidia.com/gpu"
    quota-resources: "cpu,memory"
    quota.cpu: "24"
    quota.memory: "48Gi"
    label-selector:
      matchLabels:
        quota-enabled: "true"
```

**团队 A 的节点特定覆盖**：

```bash
kubectl annotate node gpu-node-team-a \
  volcano.sh/crossquota-cpu="32" \
  volcano.sh/crossquota-memory="64Gi"
```

**团队 A Pod**：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: team-a-pod
  labels:
    quota-enabled: "true"
    team: "team-a"
spec:
  schedulerName: volcano
  nodeSelector:
    team: "team-a"
  containers:
  - name: app
    image: team-a-app:latest
    resources:
      requests:
        cpu: "8"
        memory: "16Gi"
```

**团队 B Pod**（没有 quota-enabled 标签）：

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: team-b-pod
  labels:
    team: "team-b"
spec:
  schedulerName: volcano
  containers:
  - name: app
    image: team-b-app:latest
    resources:
      requests:
        cpu: "4"
        memory: "8Gi"
```

**结果**：
- 团队 A Pod 受配额控制（默认 24 个 CPU 核心，48 GiB 内存）
- 没有 quota-enabled 标签的团队 B Pod 可以使用完整资源
- 每节点配额覆盖允许团队特定配置

## 评分策略说明

### 最高利用率策略

**何时使用**：
- 批处理作业
- 短期任务
- 需要整合工作负载时
- 需要为大型任务保留空节点时

**公式**：
```
score = (used + requested) / quota × resourceWeight
```

**示例**：
```
节点 A：CPU 使用率 80%，内存使用率 70% → 分数较高
节点 B：CPU 使用率 30%，内存使用率 40% → 分数较低
结果：优先选择节点 A
```

### 最低利用率策略

**何时使用**：
- 长期运行的服务
- 高可用性工作负载
- 需要负载均衡时
- 需要避免资源热点时

**公式**：
```
score = (quota - used - requested) / quota × resourceWeight
```

**示例**：
```
节点 A：CPU 使用率 80%，内存使用率 70% → 分数较低
节点 B：CPU 使用率 30%，内存使用率 40% → 分数较高
结果：优先选择节点 B
```

## 最佳实践

### 配置最佳实践

1. **从保守配置开始**：从限制性配额开始，根据观察到的使用模式逐渐增加。

   ```yaml
   quota.cpu: "16"  # 从较低开始
   quota.memory: "32Gi"
   ```

2. **对异构集群使用百分比**：当节点容量不同时，基于百分比的配额自动适应。

   ```yaml
   quota-percentage.cpu: "50"
   quota-percentage.memory: "50"
   ```

3. **设置适当的资源权重**：通过分配更高权重来优先考虑关键资源。

   ```yaml
   weight.cpu: 10      # CPU 是关键资源
   weight.memory: 3    # 内存相对重要
   ```

4. **先在非生产环境测试**：在部署到生产环境之前在测试环境中验证配置。

### 运维最佳实践

1. **监控资源利用率**：定期检查配额使用情况以优化设置。

   ```bash
   # 启用详细日志记录
   kubectl logs -n volcano-system volcano-scheduler-xxx --tail=100 | grep crossquota
   ```

2. **对特殊情况使用节点注解**：为特定节点类型覆盖全局设置。

   ```bash
   kubectl annotate node special-gpu-node \
     volcano.sh/crossquota-cpu="8"
   ```

3. **根据工作负载类型应用策略**：不同工作负载受益于不同策略。

   ```yaml
   # 批处理作业
   volcano.sh/crossquota-scoring-strategy: "most-allocated"
   
   # 服务
   volcano.sh/crossquota-scoring-strategy: "least-allocated"
   ```

4. **记录您的配置**：维护清晰的配额设置文档和基本原理。

### 策略选择指南

| 工作负载类型 | 推荐策略 | 原因 |
|-------------|---------|------|
| 批处理作业 | `most-allocated` | 整合到更少节点 |
| Web 服务 | `least-allocated` | 分布以提高可靠性 |
| 数据处理 | `most-allocated` | 最大化节点可用性 |
| 监控/日志 | `least-allocated` | 避免集中 |
| CI/CD 作业 | `most-allocated` | 优化资源打包 |

### 标签选择器最佳实践

1. **使用选择加入方式**：仅对具有特定标签的 Pod 应用配额以避免意外。

   ```yaml
   label-selector:
     matchExpressions:
       - key: quota-controlled
         operator: Exists
   ```

2. **组合多个条件**：分层多个条件以实现精确控制。

   ```yaml
   label-selector:
     matchLabels:
       team: "data-science"
     matchExpressions:
       - key: environment
         operator: In
         values:
           - development
           - testing
   ```

3. **豁免关键工作负载**：使用 `DoesNotExist` 或 `NotIn` 排除高优先级 Pod。

   ```yaml
   label-selector:
     matchExpressions:
       - key: priority
         operator: NotIn
         values:
           - critical
           - high
   ```

4. **记录标签要求**：维护清晰的配额控制所需标签文档。

5. **使用准入 Webhook**：在准入前验证 Pod 具有所需标签以避免调度问题。

## 监控和调试

### 启用详细日志

设置日志级别以查看详细的插件活动：

```bash
# 编辑调度器部署
kubectl edit deployment volcano-scheduler -n volcano-system

# 添加标志：--v=4 用于详细日志，--v=5 用于非常详细的日志
```

### 日志消息

**插件初始化** (V=3)：
```
crossquota initialized. GPUPatterns=[nvidia.com/gpu], quotaResources=[cpu memory], labelSelector=quota-controlled=true
crossquota: plugin weight=10, resource weights=map[cpu:10 memory:1]
```

**标签选择器解析** (V=4)：
```
crossquota: parsed label selector: quota-controlled=true,environment in (production,staging)
```

**配额计算** (V=4)：
```
crossquota: node gpu-node-1 quota cpu set to 32000 (original 64000)
```

**评分** (V=4)：
```
Crossquota score for Task default/my-pod on node gpu-node-1 is: 75.5
```

**详细资源评分** (V=5)：
```
Task default/my-pod on node gpu-node-1 resource cpu, strategy: most-allocated, 
weight: 10, need 4000.000000, used 20000.000000, total 32000.000000, score 7.500000
```

### 常见问题和解决方案

#### 问题 1：Pod 未被调度

**症状**：Pod 保持在 Pending 状态，并显示配额超出错误。

**解决方案**：
1. 检查当前配额使用情况：
   ```bash
   kubectl logs -n volcano-system volcano-scheduler-xxx | grep "quota exceeded"
   ```

2. 增加配额或调整 Pod 请求：
   ```yaml
   quota.cpu: "48"  # 从 32 增加
   ```

#### 问题 2：节点分布不均

**症状**：所有 Pod 调度到相同节点。

**解决方案**：
1. 验证策略设置正确：
   ```yaml
   volcano.sh/crossquota-scoring-strategy: "least-allocated"
   ```

2. 增加插件权重：
   ```yaml
   crossQuotaWeight: 20  # 从 10 增加
   ```

#### 问题 3：节点排序不工作

**症状**：插件权重为零或 Pod 策略未应用。

**解决方案**：
1. 验证插件权重非零：
   ```yaml
   crossQuotaWeight: 10  # 必须 > 0
   ```

2. 检查 Pod 注解语法：
   ```yaml
   volcano.sh/crossquota-scoring-strategy: "most-allocated"  # 正确
   ```

#### 问题 4：Pod 不匹配标签选择器

**症状**：尽管有配置，Pod 未受配额控制。

**解决方案**：
1. 验证 Pod 具有所需标签：
   ```bash
   kubectl get pod my-pod -o jsonpath='{.metadata.labels}'
   ```

2. 测试标签选择器匹配：
   ```bash
   kubectl get pods -l quota-controlled=true
   ```

3. 检查配置中的标签选择器语法：
   ```yaml
   label-selector:
     matchLabels:
       quota-controlled: "true"  # 必须是字符串
   ```

4. 查看调度器日志中的标签选择器警告：
   ```bash
   kubectl logs -n volcano-system volcano-scheduler-xxx | grep "label selector"
   ```

## 高级用法

### 多层配额

为不同 GPU 类型配置不同配额：

```bash
# NVIDIA A100 节点 - 严格限制
kubectl annotate node gpu-a100-* \
  volcano.sh/crossquota-cpu="16" \
  volcano.sh/crossquota-memory="32Gi"

# NVIDIA V100 节点 - 适度限制
kubectl annotate node gpu-v100-* \
  volcano.sh/crossquota-cpu="32" \
  volcano.sh/crossquota-memory="64Gi"

# NVIDIA T4 节点 - 宽松限制
kubectl annotate node gpu-t4-* \
  volcano.sh/crossquota-cpu="48" \
  volcano.sh/crossquota-memory="96Gi"
```

### 添加多资源支持

从仅 CPU 逐步扩展到多资源：

```yaml
# 步骤 1：从仅 CPU 开始（向后兼容）
quota-resources: "cpu"
quota.cpu: "32"

# 步骤 2：添加内存
quota-resources: "cpu,memory"
quota.cpu: "32"
quota.memory: "64Gi"

# 步骤 3：根据需要添加更多资源
quota-resources: "cpu,memory,ephemeral-storage"
quota.cpu: "32"
quota.memory: "64Gi"
quota.ephemeral-storage: "100Gi"
```

## 参考

### 配置参数

| 参数 | 类型 | 默认值 | 描述 |
|-----|------|-------|------|
| `gpu-resource-names` | string | - | 逗号分隔的 GPU 资源模式（支持正则表达式） |
| `quota-resources` | string | `"cpu"` | 逗号分隔的配额资源列表 |
| `quota.<resource>` | string | - | 资源的绝对配额 |
| `quota-percentage.<resource>` | string | - | 资源的百分比配额 (0-100) |
| `crossQuotaWeight` | int | 10 | 节点排序的插件权重 |
| `weight.<resource>` | int | 不同 | 评分中资源的权重 |
| `label-selector` | object | - | 过滤 Pod 的标签选择器（matchLabels 和 matchExpressions） |

### 注解

| 注解 | 作用域 | 值 | 描述 |
|-----|-------|---|------|
| `volcano.sh/crossquota-<resource>` | Node | quantity | 覆盖特定节点的配额 |
| `volcano.sh/crossquota-percentage-<resource>` | Node | 0-100 | 覆盖节点的配额百分比 |
| `volcano.sh/crossquota-scoring-strategy` | Pod | `most-allocated`, `least-allocated` | 评分策略选择 |

### 资源权重默认值

| 资源 | 默认权重 |
|-----|---------|
| `cpu` | 10 |
| `memory` | 1 |
| 其他 | 1 |

