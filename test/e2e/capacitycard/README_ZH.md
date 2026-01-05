# Capacity-Card 插件集成测试

本文档介绍了用于验证 Volcano 调度插件 `capacity-card` 功能的集成测试。

## 测试结构

集成测试位于 `/mnt/d/code/volcano/test/e2e/capacitycard/` 目录下，包含以下文件：

- `main_test.go`: 测试主函数，初始化测试环境
- `e2e_test.go`: 测试套件注册函数
- `capacity_card_test.go`: 主要测试用例实现
- `README.md`: 测试文档说明

## 测试功能

测试用例涵盖以下核心功能：

1. **基本队列容量管理测试**
   - 验证带有 GPU 卡片配额注解的队列创建和状态管理

2. **作业入队性检查 - 成功案例**
   - 测试带有卡片资源请求的作业能否正常入队
   - 验证队列卡片配额与作业卡片请求的关系

3. **任务卡片资源分配测试**
   - 验证任务级别的卡片名称请求功能
   - 测试带有卡片名称注解的任务调度

4. **卡片资源超出配额测试**
   - 测试当队列内已使用部分卡片配额后，新作业请求超出剩余配额的场景
   - 验证队列卡片资源配额的限制机制

## 如何运行测试

### 前置条件

- Kubernetes 集群已部署
- Volcano 已安装
- 调度器配置已启用 capacity-card 插件
- 已安装 fake-gpu-operator Helm Chart 来模拟 GPU 卡片资源

### 安装 fake-gpu-operator

通过以下命令为节点打标，指定节点所属的 GPU 卡片资源池：

<node-name> 为节点名称，用于指定要打标的节点。
<config> 为节点打标配置，用于指定节点所属的 GPU 卡片资源池，参考 fake-gpu-operator/values.yaml 中的 nodePools 配置。

```bash
kubectl label node <node-name> run.ai/simulated-gpu-node-pool=<config>
```

在运行测试之前，需要先安装 fake-gpu-operator Helm Chart 来模拟 GPU 卡片资源。可以使用以下命令安装：
其中 `--version <VERSION>` 为可选项

```bash
helm upgrade -i gpu-operator oci://ghcr.io/run-ai/fake-gpu-operator/fake-gpu-operator --namespace gpu-operator --create-namespace --version <VERSION> -f ../hack/fake-gpu-operator-values.yaml
```

### 运行命令

根据项目的Makefile结构，正确的运行命令如下：

```bash
cd /mnt/d/code/volcano
E2E_TYPE=CAPACITYCARD ./hack/run-e2e-kind.sh
```

或者可以构建镜像后运行：

```bash
cd /mnt/d/code/volcano
make images
E2E_TYPE=CAPACITYCARD ./hack/run-e2e-kind.sh
```
## 测试说明

测试使用 Ginkgo 和 Gomega 测试框架，遵循 Volcano E2E 测试的标准结构：

1. 每个测试用例都会创建独立的测试命名空间
2. 测试会创建带有卡片配额注解的队列
3. 创建包含卡片资源请求的作业
4. 验证作业是否正确创建和管理
5. 测试结束后会清理所有创建的资源

## 测试注解说明

- `volcano.sh/card.quota`: 队列级别的卡片资源配额，以 JSON 格式表示
- `volcano.sh/card.request`: 作业级别的卡片资源请求，以 JSON 格式表示
- `volcano.sh/card.name`: 任务级别的卡片名称请求

## 注意事项

- 测试环境需要支持 GPU 资源，特别是 `nvidia.com/gpu` 资源类型
- 确保调度器配置中启用了 capacity-card 插件
- 测试运行过程中会创建和删除资源，请在测试环境中执行