

# Volcano AI Scheduling Primitives Atomic Decoupling and Physical Isolation
 
**Authors:** wangyang0616

---

## 1. Summary
This proposal suggests a structural refactoring of the Volcano controller architecture to achieve physical decoupling of **HyperNode** and **PodGroup**. By leveraging a **Staging Mode** and independent Go modules—while maintaining a **Unified Volcano API Library**—we aim to transform core scheduling units into atomic primitives. This evolution empowers hardware ecosystem co-construction and supports complex AI scenarios such as **PD (Prefill & Decoding) separation** and **multi-dimensional Gang scheduling**.

## 2. Goals & Non-Goals

### 2.1 Goals
* **Physical Isolation**: Move HyperNode and PodGroup logic into `staging/` with independent `go.mod` files to support standalone compilation.
* **Atomic Delivery**: Provide the capability to build and deploy lightweight, independent images (`vc-hypernode`, `vc-podgroup`) for inference and edge scenarios.
* **Ecosystem Empowerment**: Simplify the contribution path for hardware vendors to implement topology discovery within a decoupled HyperNode module.
* **Maintain Monolithic Compatibility**: Ensure the existing `vc-controller-manager` remains the primary delivery vehicle with zero changes to its default behavior.

### 2.2 Non-Goals
* **API Fragmentation**: We will **NOT** split the `volcano.sh/apis` library. All primitives will continue to share the unified API contract.
* **Logic Refactoring**: We will **NOT** perform significant logic rewrites, as Volcano already supports logical decoupling via its registry and lazy-loading Informer mechanisms.
* **Scheduler Core Change**: This proposal does not aim to modify the Volcano Scheduler's core algorithm; it focuses on the Controller's architectural organization.

## 3. Motivation & Value
As an AI-native scheduler, Volcano's core primitives must adapt to next-generation AI infrastructure:

* **Heterogeneous Topology Foundation**: HyperNode requires an independent evolution interface to co-build **Network Topology Domain Primitives** with hardware vendors (GPU/NPU/RDMA).
* **Multi-dimensional Gang Scheduling**: In **PD Separation** scenarios, AI workloads consist of multiple logical groups. PodGroup must evolve to provide **multi-layered resource coordination**.
* **Primitive-level Joint Scheduling**: Enabling dual-axis concurrency of "Topology Awareness + Gang Coordination."
    * **Training**: Improves resource alignment efficiency in large-scale clusters.
    * **Inference**: Ensures shards are placed on optimal paths (NVLink/RDMA) to reduce **TTFT** and increase throughput.
* **Ecosystem Integration**: Allows developers to integrate Volcano's modular capabilities into third-party frameworks without replacing the existing scheduler.

## 4. Detailed Design

### 4.1 Logical Decoupling Standardization
* **Registry Pattern**: Continue using the `Register` pattern in `pkg/controllers/framework`.
* **On-demand Informers**: Ensure decoupled controllers register only the necessary Informers within their `Initialize` functions to minimize API Server pressure.

### 4.2 Directory Restructuring (Staging Mode)


```text
volcano/
├── apis/                       # Unified API Library (Contains all CRDs)
├── pkg/
│   └── controllers/            # Monolithic business logic (Job, Queue, etc.)
├── staging/src/volcano.sh/
│   ├── hypernode/              # HyperNode Independent Module
│   │   ├── go.mod              # Independent Module Definition
│   │   ├── cmd/                # Entry point for vc-hypernode
│   │   └── pkg/controller/     # Core Topology Primitive Logic
│   └── podgroup/               # PodGroup Independent Module
│       ├── go.mod              # Independent Module Definition
│       ├── cmd/                # Entry point for vc-podgroup
│       └── pkg/controller/     # Core Gang Scheduling Logic
├── go.mod                      # Main Repo Entry (replace directives for staging)
```

### 4.3 Engineering Implementation
* **Independent Modules**: Create `go.mod` files within staging. Use `replace` directives to point to `../../../../apis` locally.
* **Dependency Cleanup**: Remove strong type references from `HyperNode/PodGroup` to business packages like `pkg/controllers/job`.
* **Lightweight Entry Points**: Implement minimal `main.go` files in each staging module for highly optimized, atomic binaries.

### 4.4 Dual-Delivery Mode
* **Monolithic Release (Default)**: `cmd/controller-manager` will `import` code from `staging`. This ensures the **Monolithic Image** remains the standard delivery.
* **Atomic Distribution (Optional)**: Build independent `vc-hypernode` or `vc-podgroup` images (binary size reduced by ~60%) for high-concurrency, lightweight AI inference.


## 5. Backward Compatibility
* **API Stability**: The API library remains unified. All CRDs and toolchains (e.g., `vcctl`) remain fully compatible.
* **Deployment Seamlessness**: The default behavior of `vc-controller-manager` remains unchanged; existing clusters can upgrade without configuration modifications.
