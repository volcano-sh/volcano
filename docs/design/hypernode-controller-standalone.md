# HyperNode Controller Standalone Deployment

Author: wangyang0616 · 2026-08-09

---

## 1. Summary

HyperNode is Volcano's standard abstraction for hierarchical network topology. Today, the HyperNode controller can run only as part of `vc-controller-manager`. This model works well for users deploying the complete Volcano stack, but it makes HyperNode difficult to adopt independently for users who already operate their own scheduler or need only topology discovery and management.

This proposal provides two deployment modes backed by the same implementation:

- **Controller-manager mode — default:** HyperNode continues to run in `vc-controller-manager`; existing users keep their current deployment model.
- **Standalone mode — optional:** HyperNode runs as a dedicated Volcano component for topology-focused environments or environments using a custom scheduler.

The central decision is to **decouple HyperNode deployment from other controllers without splitting HyperNode into a separate product**. The deployment boundary is expanded, while the API, source ownership, community governance, and release boundaries remain aligned with Volcano:

- users can choose integrated or standalone deployment;
- both deployment modes share one implementation and produce semantically equivalent HyperNode resources;
- the HyperNode API remains the ecosystem integration boundary;
- source, versioning, and releases remain under the Volcano community.

This gives users an independent operating model without creating a second implementation, compatibility contract, or maintenance lifecycle. This proposal neither introduces nor plans a separate HyperNode Go module or repository.

## 2. Background and Community Value

Communication topology is becoming a common scheduling input for large-scale AI training and inference. Different accelerator generations, network architectures, and vendor management systems also require topology discovery to evolve independently from scheduling policy.

The community therefore needs to support two legitimate adoption models:

1. users adopting Volcano as a complete scheduling system;
2. users adopting only the HyperNode topology capability alongside an existing scheduler or AI platform.

The current aggregate controller process serves the first model but creates unnecessary operational coupling for the second. Volcano does not provide an official way to deploy, roll out, scale, or manage RBAC for HyperNode independently, so users that only need HyperNode topology must still operate unrelated controller-manager capabilities.

The existing `--controllers` flag selects which controller logic runs in the process, but it does not provide an independent binary, image, Deployment, fault domain, or RBAC boundary. It therefore cannot replace an officially supported standalone runtime.

This is primarily a community adoption and long-term maintenance problem, not a source-code layout problem. Volcano already has a clear resource boundary: the HyperNode controller discovers and maintains HyperNode resources, and schedulers consume those resources through `volcano.sh/apis`. What is missing is an officially supported standalone deployment built around that boundary.

[Issue #5133](https://github.com/volcano-sh/volcano/issues/5133) discusses broader controller modularization. This proposal intentionally covers HyperNode only. HyperNode has a clear API boundary and a concrete use case for standalone deployment; other components should be evaluated separately according to their own user value and coupling rather than inheriting this proposal's conclusion.

## 3. Community User Scenarios

| Community user | Expected way to use HyperNode | Support after this proposal |
| --- | --- | --- |
| Existing Volcano user | Upgrade Volcano and continue running HyperNode in `vc-controller-manager` | Supported by default with no migration |
| Standalone HyperNode or custom-scheduler user | Deploy the official standalone HyperNode image without the complete Volcano controller manager | Officially supported |
| User building Volcano independently | Clone or fork Volcano and build the standalone component from the corresponding source version | Supported |
| User maintaining environment-specific discovery | Implement and register a private Discoverer in a Volcano fork, then build an enhanced image | Supported; the user owns rebasing and compatibility testing |
| User contributing generally useful discovery | Implement a Discoverer through the existing registry and contribute it upstream | Supported and preferred; after merge, it is delivered in official images |
| Custom scheduler or AI platform | Consume HyperNode resources through `volcano.sh/apis` | Supported and recommended |
| Go project importing controller implementation directly | Treat internal controller packages as a reusable SDK | Not supported or planned |

These scenarios lead to three community-level conclusions:

- The community should provide official deployment choices instead of requiring standalone HyperNode users to maintain a custom deployment.
- Generally useful topology discovery and reconciliation capabilities should converge upstream so that compatibility, CI, and release maintenance are shared.
- The HyperNode API, rather than internal controller packages, should remain the primary integration boundary between independently evolving systems.

## 4. Goals and Non-Goals

### 4.1 Goals

- Provide an officially supported standalone HyperNode controller while retaining controller-manager mode as the default.
- Allow standalone HyperNode and custom-scheduler users to adopt HyperNode without running unrelated Volcano controllers.
- Preserve the existing HyperNode resource API, configuration, discovery behavior, and scheduler integration.
- Preserve the existing Discoverer registry so that new discovery methods work in both deployment modes.
- Ensure that both deployment modes behave consistently and that exactly one process owns HyperNode reconciliation.
- Deliver the standalone component with the same production-grade operational capabilities and release process as other Volcano components.
- Establish maintainable source and extension boundaries.

### 4.2 Non-Goals

- Move HyperNode into `staging/` or create a separate Go module or repository.
- Publish internal controller or Discoverer packages as a stable public SDK.
- Establish an independent HyperNode version, release cadence, or governance model.
- Change the HyperNode CRD, resource semantics, discovery configuration schema, webhook, or scheduling behavior.
- Apply the same separation decision directly to PodGroup or other Volcano controllers.

## 5. Proposal

### 5.1 Overview

This proposal changes only how HyperNode is deployed and delivered. It does not change the resource API, functional semantics, source ownership, or community governance boundary:

| Area | Definition in this proposal |
| --- | --- |
| Deployment | Add an optional standalone mode alongside the existing controller-manager mode |
| Implementation | Both deployment modes share the implementation under `pkg/controllers/hypernode` |
| Integration | Custom schedulers and AI platforms continue to consume topology information through the HyperNode API |
| Delivery | The community publishes the standalone binary, image, and installation support with Volcano releases |
| Versioning and governance | HyperNode continues to use Volcano's version, release cadence, and community process |

The standalone controller is therefore an official deployment mode for HyperNode, not a separate product.

### 5.2 Code and Delivery Structure

This proposal retains the existing HyperNode implementation directory and adds a standalone process and delivery entry points within the main Volcano repository. The primary layout is:

```text
volcano/
├── cmd/
│   ├── controller-manager/                    # Existing aggregate process; runs HyperNode by default
│   └── hypernode-controller-manager/          # New: standalone entry point, options, and startup logic
│       ├── main.go
│       └── app/
│           ├── server.go
│           └── options/options.go
├── pkg/
│   ├── controllers/
│   │   └── hypernode/                         # Retained: discovery, registry, reconciliation, and tests
│   │       ├── api/
│   │       ├── config/
│   │       └── discovery/
│   │           ├── label/                     # Updated: reuse process-provided informers
│   │           └── ufm/
│   └── util/
│       └── hypernode/                         # New: repository-internal MemberSelector helpers
├── installer/
│   ├── dockerfile/
│   │   └── hypernode-controller-manager/      # New: standalone image build entry point
│   └── helm/chart/volcano/
│       ├── values.yaml                        # Updated: runtime mode and standalone settings
│       └── templates/
│           ├── controllers.yaml               # Updated: control HyperNode in the aggregate process
│           └── hypernode_controller.yaml      # New: Deployment, ServiceAccount, RBAC, and Service
├── Makefile                                   # Updated: binary, image, and release targets
└── hack/                                      # Updated: manifest generation and release verification
```

The implementation scope is:

| Area | Primary change |
| --- | --- |
| Standalone command | Add `vc-hypernode-controller-manager`, assembling only the HyperNode controller and its required runtime facilities |
| Aggregate command | Preserve existing default behavior; in standalone mode, disable HyperNode in the aggregate process through the existing controller-selection mechanism |
| Runtime wiring | The standalone command constructs the Kubernetes and Volcano clients and shared informer factories required by the existing HyperNode controller contract |
| HyperNode controller | Remove dependencies on scheduler implementation packages and accept only the required process-provided informers |
| Discoverers | Preserve the existing registry; make the Label Discoverer reuse the Node and HyperNode shared informers; keep other discoverers, including UFM, behaviorally unchanged |
| Repository-internal utility | Move MemberSelector resolution from `pkg/scheduler/api` to `pkg/util/hypernode` for reuse by the controller and scheduler without changing `volcano.sh/apis` |
| Helm and RBAC | Add mutually exclusive runtime modes, a standalone Deployment, and a least-privilege ServiceAccount/RBAC; share the existing controller ConfigMap and create no standalone resources by default |
| Build and release | Integrate the new binary and image with the existing Makefile, image builds, manifest generation, CI, and Volcano release pipeline |

### 5.3 Runtime Modes

The Helm chart uses one setting to control which process owns HyperNode reconciliation:

```yaml
custom:
  hypernode_controller_mode: controller-manager # controller-manager | standalone | disabled
```

The setting provides three mutually exclusive modes:

| Mode | Effective behavior |
| --- | --- |
| `controller-manager` | Default; no standalone resources are created, and HyperNode remains governed by the existing controller gates in `vc-controller-manager` |
| `standalone` | Helm disables HyperNode in `vc-controller-manager` and creates the dedicated `vc-hypernode-controller-manager` Deployment |
| `disabled` | No standalone resources are created, and HyperNode is disabled in `vc-controller-manager` |

In `standalone` and `disabled` modes, the chart appends `-hyperNode-controller` to the aggregate controller selection. The `controller-manager` mode continues to honor the existing controller gates unchanged. Do not explicitly enable `hyperNode-controller` through `controller_enabled_controllers` in `standalone` or `disabled` mode; conflicting controller gates are unsupported.

Because the two deployment modes use different leader-election Leases, leader election cannot prevent a brief period of concurrent reconciliation during a transition. This proposal requires transitions through the `disabled` intermediate state:

```text
controller-manager → disabled → standalone
standalone → disabled → controller-manager
```

After entering `disabled`, the operator must confirm that the previous controller has stopped before enabling the target deployment mode. Existing HyperNode resources remain in the cluster during the transition. The initial implementation does not support a single-step transition with a zero-overlap guarantee.

Standalone mode always enables leader election, including for a single replica, so a
replacement Pod cannot reconcile until it acquires the Lease.

### 5.4 Compatibility Commitments

Both deployment modes use the existing HyperNode implementation, controller ConfigMap, and Discoverer registry. Given the same topology source configuration and set of registered Discoverers, they must produce semantically equivalent HyperNode resources. The default upgrade path creates no standalone workload and requires no API, configuration, or data migration.

Standalone mode requires built-in Discoverers to reuse process-provided informers. Adapting the Label Discoverer to this ownership model exposed an existing gap in the ConfigMap-driven reload path: results from a retired Discoverer instance could overlap with or be acknowledged to its replacement. The manager lifecycle is therefore hardened for both deployment modes without changing the HyperNode API, discovery configuration, steady-state reconciliation semantics, or the legacy client-based Discoverer registration contract. This lifecycle does not coordinate deployment-mode transitions, which remain governed by the Helm `disabled` intermediate state described above.

The standalone component provides the standard operational capabilities expected of a Volcano controller, including leader election, health and metrics endpoints, graceful shutdown, and least-privilege RBAC. It uses a dedicated leader-election Lease so that leader election and failover do not depend on `vc-controller-manager`. Exact flags and resource names are implementation details and are intentionally left out of this proposal.

### 5.5 Resource Access Boundary

The standalone process initializes only the clients and informers that HyperNode requires; it must not inherit the resource access scope of unrelated Volcano controllers:

| Resource | Access | Purpose |
| --- | --- | --- |
| Node | List/Watch | Read topology labels and calculate the node count represented by each HyperNode |
| HyperNode | Get/List/Watch and reconciliation writes | Maintain topology resources and their status |
| Controller ConfigMap | List/Watch the installation's configuration object through a `metadata.name` field selector | Load and update topology discovery configuration |
| UFM Secret | Get by name only when referenced; no Watch | Retrieve credentials for the external discovery system |
| Leader-election Lease | Get/Create/Update as required by leader election | Provide high availability for the standalone controller |

The standalone controller must not watch Pods, PodGroups, Volcano Jobs, Queues, storage resources, or any other resources unrelated to HyperNode. Discoverers must reuse the process-provided Node and HyperNode shared informers rather than creating duplicate List/Watch streams and caches. Standalone RBAC must match this access scope.

Default RBAC does not grant cluster-wide Secret access. The installation namespace is authorized by default. If a UFM Secret is outside that namespace, an administrator must create a Role and RoleBinding for the standalone ServiceAccount in the target namespace.

For compatibility with Kubernetes versions that do not use field selectors during RBAC authorization, the Role grants ConfigMap List/Watch within the installation namespace. The client request itself is restricted to the release's controller ConfigMap by a `metadata.name` field selector.

### 5.6 Distribution and Release

The community publishes an official `vc-hypernode-controller-manager` binary and image with every Volcano release. The component uses the corresponding Volcano version and is deployed only when the user selects standalone mode.

HyperNode is not versioned or released independently. Its documentation, security response, compatibility validation, and release testing remain part of the standard Volcano community process.

### 5.7 Source and Extension Boundary

The HyperNode implementation remains under `pkg/controllers/hypernode` in the root `volcano.sh/volcano` module. The standalone command is another official Volcano process assembled from the same implementation, not a separate project.

The source boundary must ensure that HyperNode discovery and reconciliation do not depend on scheduler implementation packages or unrelated controllers. The community maintains this boundary through code organization, review, and dependency checks rather than a second `go.mod`.

HyperNode already provides a Discoverer registry. Community users can implement and register new topology discovery methods, while the HyperNode controller converts discovery results into HyperNode resources and reconciles those resources:

- Generally useful discovery capabilities should be contributed upstream. After merge, the community builds, tests, maintains compatibility, and releases them for both controller-manager and standalone modes.
- Environment-specific capabilities that cannot be generalized or contributed upstream can be registered as private Discoverers in a Volcano fork. The user then builds and maintains the enhanced image.

The existing registry remains a source-level, compile-time Discoverer extension mechanism. This proposal does not publish it as a stable public SDK for external Go modules and does not promise cross-version compatibility for internal interfaces.

## 6. Risks and Mitigations

| Risk | Mitigation |
| --- | --- |
| A standalone component expands the community's maintenance and release surface | Reuse one implementation, test suite, and Volcano release pipeline for both deployment modes |
| The aggregate and standalone processes may reconcile HyperNodes concurrently | Make deployment modes mutually exclusive and document an explicit two-step transition procedure |
| The two deployment modes may diverge in behavior | Treat behavioral parity as a compatibility commitment and release-validation requirement |
| Standalone runtime wiring may introduce unrelated or duplicate resource watches | Initialize informers on demand, require Discoverers to reuse shared informers, and enforce the boundary through RBAC and tests |
| Users may interpret the standalone controller as a separate product | Keep its source, versioning, release, and governance explicitly within Volcano |

## 7. Success Criteria

This proposal is successful when:

- existing users can upgrade without changing how HyperNode runs;
- standalone HyperNode and custom-scheduler users can deploy an official, production-ready component;
- controller-manager and standalone modes preserve equivalent HyperNode behavior;
- installation and transition procedures prevent the two processes from reconciling HyperNodes concurrently;
- the standalone process neither watches unrelated resources nor creates duplicate Node or HyperNode informers;
- the standalone component is built, tested, security-scanned, and released with Volcano;
- the HyperNode controller no longer depends on `pkg/scheduler/api` or implementation packages of unrelated controllers.

Implementation validation must cover binary and image builds, behavioral parity, ownership transitions, informer reuse and resource watch scope, high availability, RBAC, upgrade compatibility, and API-based integration. Detailed test cases belong with the implementation change.

---

## 8. References

- [Issue #5133: HyperNode controller decoupling background and discussion](https://github.com/volcano-sh/volcano/issues/5133)
- [Network Topology Aware Scheduling](./Network%20Topology%20Aware%20Scheduling.md)
- [Volcano APIs](../../staging/src/volcano.sh/apis/README.md)
- [Current HyperNode controller](../../pkg/controllers/hypernode)
