# DRA E2E Process

## 当前目标

- [完成] 根据 `_tmp/e2e-dra-supplement-plan.md` 补齐 DRA quota 第一阶段 e2e 用例。
- [完成] 保持默认 e2e 脚本行为为使用 kind 创建集群执行测试。
- [完成] 在 kind 环境中完整执行并通过新增的 DRA quota e2e。
- [未完成] 继续补齐第二阶段边界与 reclaim 相关用例。

## 已完成内容

### 代码实现

- [完成] 将 `test/e2e/dra/dra_quota_test.go` 重构为统一 setup 的稳定用例集合。
- [完成] 将 scheduler 插件切换到 `capacity` 的逻辑收敛到 `BeforeEach`。
- [完成] 修复 framework `Arguments.GetBool` 对字符串布尔值的解析，避免真实 kind 集群里的 scheduler config `"...Enable: \"true\""` 被忽略。
- [完成] 修复 `capacity` 插件在共享 `ResourceClaim` 已被 queue 引用后，后续 task 仍重复计算 DRA 增量导致 queue overused 的问题。
- [完成] 补充 direct `ResourceClaim` 成功调度用例。
- [完成] 补充 `ResourceClaimTemplate` 成功调度用例。
- [完成] 补充共享 direct claim 去重计费用例。
- [完成] 补充 count quota 超限 pending 用例。
- [完成] 补充 `cores.deviceclass/...` capacity quota 超限 pending 用例。
- [完成] 补充 capacity quota 释放后恢复调度用例。

### 测试辅助与框架支持

- [完成] 在 `test/e2e/util/dra.go` 中新增 `CreateResourceClaimTemplate` helper。
- [完成] 在 e2e 测试文件中补充 direct claim、template claim、共享 claim、capacity 请求的构造辅助函数。
- [完成] 共享 claim e2e 用例调整为 `2 replicas + minAvailable=1`，避免与 Volcano gang 调度阶段的 claim 绑定时序互相阻塞，仍然准确覆盖“共享 claim 只计费一次”的目标。
- [完成] 在 scheduler framework 中补充 queue DRA allocated 聚合逻辑。
- [完成] 让 queue `status.allocated` 能带出 `deviceclass/...` 与 `cores.deviceclass/...` 键。
- [完成] 为 framework 新增单测，覆盖共享 claim 去重和 queue status DRA key 编码。
- [完成] 为 `capacity` 插件补充共享 claim 增量去重单测，覆盖“已引用 claim 不再重复占用 queue DRA quota”。

### 脚本与执行策略

- [完成] 尝试过“优先复用本地已有 Kubernetes 集群”的 e2e 脚本改造。
- [完成] 验证本地 `orbstack` 集群不具备 DRA 所需 `resource.k8s.io` API，不能用于 DRA e2e。
- [完成] 将 `hack/run-e2e-kind.sh` 恢复为默认使用 kind 创建集群执行 e2e。
- [完成] 将 `E2E_TYPE=DRA` 与 `E2E_TYPE=ALL` 的 DRA focus 扩展为同时覆盖 `DRA E2E Test` 与 `DRA Quota E2E Test`。
- [完成] 修复 `hack/lib/install.sh` 中 `check-kind` 安装完 `kind` 后当前 shell 无法立即找到二进制的问题。
- [完成] 修复 `kind create cluster` / `kind load docker-image` 失败后脚本仍继续执行的行为，改为失败即退出。
- [完成] 对恢复后的 `hack/run-e2e-kind.sh` 做了语法检查，结果通过。

## 已完成验证

- [完成] `go test ./pkg/scheduler/framework ./pkg/scheduler/plugins/capacity ./pkg/scheduler/cache` 通过。
- [完成] `go test -run '^$' ./test/e2e/dra` 通过，确认 e2e 包可编译。
- [完成] `bash -n hack/run-e2e-kind.sh` 通过。
- [完成] `bash -n hack/lib/install.sh` 通过。
- [完成] 通过 `source hack/lib/install.sh; check-kind` 验证 `kind` 安装后会将 `go bin` 加入 PATH，当前 shell 可继续调用 `kind`。
- [完成] `go test ./pkg/scheduler/plugins/capacity` 通过，覆盖共享 claim queue 增量去重修复。
- [完成] 2026-04-10 提权执行 `DRA_GINKGO_FOCUS='DRA Quota E2E Test' KIND_OPT='--image kindest/node:v1.35.0 --config hack/e2e-kind-config.yaml' CLEANUP_CLUSTER=0 INSTALL_MODE=existing E2E_TYPE=DRA FEATURE_GATES="DynamicResourceAllocation=true,DRAConsumableCapacity=true" ./hack/run-e2e-kind.sh`，结果 `6 Passed / 0 Failed / 18 Skipped`。

## 未完成内容

### e2e 执行

- [完成] 使用 kind 环境完整执行 `DRA Quota E2E Test` 并确认新增 quota 用例全部通过。
- [完成] 在 kind 环境中验证 `ResourceClaimTemplate`、共享 claim 去重、queue status allocated、capacity quota 场景稳定。
- [未完成] 视需要再补一轮 `E2E_TYPE=DRA` 全量回归，确认 quota 修复未影响其它 DRA 基础用例。

### 第二阶段补充

- [未完成] `allocationMode: All` 跳过 quota 跟踪用例。
- [未完成] `FirstAvailable` 跳过 quota 跟踪用例。
- [未完成] 双队列 `deserved / guarantee / reclaim` 场景。

### 环境问题

- [完成] 本机 `helm version --short` 返回 `v4.1.4+g05fa379`，`helm` 可用。
- [完成] kind 环境中的 DRA driver、`resource.k8s.io` API、Volcano 安装流程已在 quota focused e2e 中验证可用。
- [完成] `kindest/node:v1.35.0`、DRA 辅助镜像、Volcano 本地镜像已准备完成并被成功 load 进 kind 集群。

## 当前工作区变更

- [完成] 已修改 `test/e2e/dra/dra_quota_test.go`
- [完成] 已修改 `test/e2e/util/dra.go`
- [完成] 已修改 `pkg/scheduler/framework/session.go`
- [完成] 已修改 `pkg/scheduler/framework/arguments.go`
- [完成] 已修改 `pkg/scheduler/framework/arguments_test.go`
- [完成] 已新增 `pkg/scheduler/framework/session_dra_queue_status.go`
- [完成] 已新增 `pkg/scheduler/framework/session_dra_queue_status_test.go`
- [完成] 已修改 `pkg/scheduler/plugins/capacity/capacity.go`
- [完成] 已修改 `pkg/scheduler/plugins/capacity/capacity_dra_test.go`
- [完成] 已修改 `test/e2e/dra/main_test.go`
- [完成] 已修改 `hack/e2e-kind-config.yaml`
- [完成] 已修改 `hack/run-e2e-kind.sh`
- [完成] 已修改 `hack/lib/install.sh`
- [完成] `.gitignore` 存在用户已有未提交修改，未处理

## 下一步建议

- [未完成] 补齐第二阶段 `allocationMode: All`、`FirstAvailable`、双队列 `deserved / guarantee / reclaim` 场景。
- [未完成] 追加一轮 `E2E_TYPE=DRA` 全量回归，确认 quota 修复没有引入对原有 DRA e2e 的回归。
- [未完成] 如需进一步降低执行时间，可考虑为 quota suite 单独暴露更细粒度的 ginkgo focus 或独立脚本入口。
