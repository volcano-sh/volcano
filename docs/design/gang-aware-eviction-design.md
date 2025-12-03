# Gang-Aware Preemption & Reclaim Design Document

[@vzhou-p](https://github.com/vzhou-p); Dec 3, 2025

## 1. Problem Statement

### 1.1 Current Limitation
The Volcano scheduler's existing actions (`allocate`, `preempt`, `reclaim`) suffer from architectural coupling and "Task-Centric" decision making.

1.  **Gang Blindness (Preempt/Reclaim)**: 
    - Victims are selected greedily (Task-by-Task).
    - Evicting 1 task from 5 different gangs might be chosen over evicting 1 whole gang, causing 5 cascading failures instead of 1.
    - Global cost (total resources of the broken gang) is ignored.

2.  **Topology Coupling (Allocate)**:
    - The `allocate` action contains **hardcoded logic** for "HyperNode Tiers" and "LCA Checks".
    - This violates the plugin architecture; `allocate.go` should not know about network topology details.
    - This makes it impossible to reuse the topology logic in `preempt` or `reclaim` without duplicating code.

3.  **Topology Blindness (Preempt/Reclaim)**:
    - Because the topology logic is trapped in `allocate.go`, the `preempt` and `reclaim` actions simply ignore it.
    - They might evict tasks on nodes that are technically valid for the pod but invalid for the Job's Hard Topology constraints.

### 1.2 Optimization Goal
1.  **Decouple Topology Logic**: Move strict topology constraints out of `allocate.go` and into the plugin layer.
2.  **Gang-Aware Eviction**: Implement a "Holistic" victim selection strategy that considers the Gang as the atomic unit of eviction.
3.  **Unified Search Constraint**: Create a mechanism where `allocate`, `preempt`, and `reclaim` all respect the same set of hard constraints via a composable search space reduction.

---

## 2. Architectural Change: The Gang-Level Pipeline

To resolve these issues, we introduce three new extension points that allow plugins to control the "If", "Where", and "Who" of Gang Scheduling.

### 2.1 New Plugin Extension Points

```go
// pkg/scheduler/api/interface.go

// 1. The "If" (Optimization)
// GangPredicateFn checks if the entire gang fits in the current cluster state.
// Returns an error if the job should be skipped entirely (Fast Fail).
type GangPredicateFn func(job *JobInfo, snapshot *ClusterSnapshot) error

// 2. The "Where" (Constraint)
// GangSearchSpaceFn returns a list of "Feasible Domains"
// The Gang must be scheduled entirely within ONE of these sets.
// IMPORTANT: Domains must be returned in preference order (Best to Worst).
// - Purpose: Allows the plugin to optimize sorting/pruning for the specific action.
//   - Allocate: Plugin should return ALL valid domains (or a wide set). The 'allocate' action
//     will simulate ALL of them, accumulate scores, and pick the global best.
//   - Preempt/Reclaim: Plugin MUST return a TRUNCATED list (Top-K) sorted by feasibility.
//     The action will greedily try them in order and stop at the first success to avoid
//     performance explosion (exhaustively simulating preemption on all domains is too expensive).
type GangSearchSpaceFn func(job *JobInfo, clusterSnapshot *ClusterSnapshot, purpose SearchPurpose) ([][]*NodeInfo, error)

type SearchPurpose int
const (
    PurposeAllocate SearchPurpose = iota
    PurposeEvict
)

// 3. The "Who" (Policy)
// GangVictimSelectorFn selects specific victim gangs to satisfy a resource requirement.
// Returns: A list of JobIDs (gangs) recommended for eviction.
// Used to override default "Least Global Waste" logic.
type GangVictimSelectorFn func(preemptor *JobInfo, neededResources *Resource, candidates []*JobInfo) ([]JobID, error)
```

> **Note**: The purpose parameter of GangSearchSpaceFn decides different results for different actions.
>    *   **Allocate**: Plugin returns **ALL** feasible domains. The `allocate` action performs an exhaustive simulation + scoring to find the optimal placement.
>    *   **Evict**: Plugin returns **Top-K** domains sorted by feasibility (Cost to Enter). The `preempt` action uses a greedy "First Fit" approach to minimize scheduler latency.

### 2.2 Execution Pipeline (Composable Search)

The Session executes a 3-stage pipeline for Gang operations:

#### Phase 1: Feasibility Check (GangPredicate)
*   Call `GangPredicateFn(job)` on all plugins.
*   **Purpose**: Fast-fail jobs that cannot possibly fit (e.g., "Cluster total capacity < Job request").

#### Phase 2: Search Space Reduction (GangSearchSpace)
*   Call `GangSearchSpaceFn(job)` on plugins ordered by Priority (Config order).
*   **Logic (Primary Driver)**: The **First Plugin** that returns a non-empty result defines the Search Space. Subsequent plugins are ignored for domain generation.
*   **Reasoning**: This "Winner-Takes-All" approach avoids complex intersection logic and ordering conflicts. The highest-priority plugin (e.g., `network-topology-aware`) drives the structural constraint.
*   **Result**: A list of `OrderedValidDomains` (`[][]*NodeInfo`).

#### Phase 3: Assignment or Eviction

The execution logic diverges based on the action type:

*   **Allocate Action (Exhaustive Search)**:
    *   Iterate through **ALL** `OrderedValidDomains`.
    *   Simulate allocation on each domain to calculate a score (Fit Score + Topology Score).
    *   Select the **Global Best** domain, Commit, and Return.
*   **Preempt/Reclaim Action (Greedy First-Fit)**:
    *   Iterate through `OrderedValidDomains` (D1, D2, ...) in the returned order.
    *   **Try D1**: Search victims in D1. If valid victims found & simulation passes, **Evict & Return Immediately**.
    *   If D1 fails, try D2.

---

## 3. Proposed Solution: Gang-Aware Eviction (`gang-preempt` / `gang-reclaim`)

With the pipeline in place, Gang-Aware Reclaim follows this flow: (Preemption will be discussed with minor changes in another section)

### 3.1 Algorithm

1.  **Identify Need**: Preemptor Job P needs resources.
2.  **Determine Search Space**:
    - Call `ssn.GetGangValidDomains(P)` (Phase 2 above).
    - We receive `[PreferredDomain, SecondaryDomain, ...]`.
3.  **Process Domains (Greedy)**:
    - **For each Domain** (set of nodes):
        - **Select Victims (Domain-Wide)**: Call `SelectGangVictimsInDomain(Preemptor, Domain)`.
            - This differs from legacy preemption by considering the *entire footprint* of victim gangs within the domain, rather than iterating node-by-node for each task of the gang.
        - **Simulate & Execute**:
            - Run the **Task-to-Node Nomination** logic (see Section 3.1.2) on the domain assuming victims are evicted.
            - **If Simulation Succeeds**:
                - **Evict**: Evict the selected victims.
                - **Nominate**: Apply the results of the simulation (update `.status.nominatedNodeName`).
                - **Return Success**.
            - **If Simulation Fails**:
                - Continue to next domain.

### 3.1.1 Why NominatedNodeName is Critical (Gang vs. Task)
Unlike the legacy `reclaim` action, `gang-reclaim` **MUST** set the `.status.nominatedNodeName` (via `Pipeline`).

*   **The Risk**: If we only evict victims without reserving the nodes, the resources become "Free" in the next scheduling cycle. A smaller job (or a higher-priority single-task job) could "steal" one of the freed nodes.
*   **The Gang Consequence**: For a single task, losing a node is an inconvenience. For a Gang requiring 5 specific nodes (topology domain), losing **one** node means the **entire Gang fails** to start. The eviction of the other 4 nodes becomes "wasted waste." By setting `nominatedNodeName`, the `allocate` action in the next cycle will prioritize these specific nodes for the Gang's tasks, guaranteeing the "Domain" remains intact.

### 3.1.2 Task-to-Node Nomination Strategy
A critical challenge in `gang-reclaim` is determining **which task** in the gang should be nominated to **which node** in the cleared domain.

**Strategy: Reuse Allocate Logic (Dry-Run)**

Instead of inventing a new matching algorithm, we reuse the existing `allocate` action logic to determine the best placement. This ensures consistency between the *Nomination* (in `gang-reclaim`) and the eventual *Binding* (in `allocate`).

1.  **Snapshot & Filter**:
    *   Create a temporary `Session` snapshot.
    *   Restrict the available nodes to **only** the nodes in the Cleared Domain

2.  **Simulate Allocation**:
    *   Call the standard `allocate.allocateResourcesForTasks()` function.
    *   This function already handles:
        *   Iterating tasks.
        *   Running Predicates (Fit Check).
        *   Running Priorities (Scoring).
        *   Topology constraints are already respected by `GangSearchSpaceFn`

3.  **Apply Nomination**:
    *   The simulation returns a `Statement` containing `Allocate` operations.
    *   Convert these `Allocate` operations into `Pipeline` operations.
    *   Execute `stmt.Pipeline(Task, Node)` to update the `.status.nominatedNodeName`.

> **Note**: While the standard `reclaim` action in Volcano does *not* use nomination (it relies on queue priority), the `preempt` action *does*. For `gang-reclaim`, we align with the `preempt` pattern because locking the specific topology domain is essential for the Gang's atomicity. A Gang cannot rely on general queue priority to secure a specific set of 5 connected nodes.

### 3.2 Victim Selection (Bundles)

When selecting victims within a Domain, we categorize the "footprint" of each candidate gang into "Bundles":

1.  **Safe Bundle**:
    - **Surplus**: Tasks above `MinAvailable` (Ready > Min).
    - **Broken**: Tasks from a gang that is *already* broken (Ready < Min) or *will be* broken in this cycle.
    - **Cost**: Zero (Free to evict).

2.  **Whole Bundle**:
    - **Core**: Tasks required to maintain `MinAvailable`.
    - **Cost**: High (Requires restarting the whole gang).

**Sorting Logic (The "Sound" Strategy)**:
We sort bundles to minimize disruption:
1.  **Gang State**: Always take **Safe/Broken** bundles first (Priority 0).
2.  **Queue Policy**: Respect `VictimQueueOrderFn` (e.g., reclaim from over-quota queues).
3.  **Global Efficiency** (For Whole Bundles):
    - `Efficiency = (Resources Freed in Domain) / (Total Global Resources of Gang)`
    - Break gangs that are primarily located *inside* the target domain. Avoid destroying a large distributed job to free a small local resource.
4.  **Job Priority**: Respect `JobOrderFn` (Priority, DRF, etc.).

### 3.2.1 Victim Selection Algorithm Pseudo-Code

```python
# ALGORITHM: SelectGangVictimsInDomain

# INPUT:
# - Preemptor: The job needing resources
# - Domain: The specific set of nodes we are clearing
# - Candidates: Jobs with tasks on these nodes
# - Session: Scheduler session (plugins)

Bundles = List()

# --- PHASE 1: Split & Bundle ---
# Try to split tasks of each job to 2 bundles. One is Safe, and the other is Core.
FOR EACH Job IN Candidates:
    # 1. Identify tasks IN THE DOMAIN
    LocalTasks = Job.Tasks.Where(task => Domain.Contains(task.Node))
    IF LocalTasks Is Empty: CONTINUE

    # 2. Sort LocalTasks (Evict low priority tasks within job first)
    # We define a custom 'VictimOrder' for Gang Preemption
    # Priority 1: Coverage (Maximize Utility)
    #   - Score = Min(Task.Res, Needed) / Needed
    #   - We prefer tasks that satisfy a larger chunk of the requirement.
    # Priority 2: Task Priority (Lowest First)
    # Priority 3: Least Waste (Best Fit)
    #   - If Coverage is equal (e.g., both tasks > needed), prefer the smaller task.
    #   - Break ties between a 4-core victim and 64-core victim (pick 4-core).
    # Priority 4: Creation Time (Youngest First)
    LocalTasks.Sort(CustomGangVictimComparator)

    # 3. Calculate Surplus Budgets (Global & Role-Aware)
    # Job might be "Broken" if it's already failing/starting
    Ready = Job.ReadyTasks
    GlobalMin = Job.MinAvailable
    GlobalSurplus = Ready - GlobalMin

    # Handle Elastic Jobs (MinAvailable < Sum(RoleMin))
    # If job is elastic, we ignore specific role constraints for safety checks.
    EnforceRoles = (GlobalMin >= Job.TaskMinAvailableTotal)

    RoleSurplus = Map()
    IF EnforceRoles:
        FOR Role IN Job.Roles:
             RoleSurplus[Role] = Job.RoleReady(Role) - Job.RoleMinAvailable(Role)

    # 4. Create Bundles
    IF GlobalSurplus < 0:
        # CASE A: Already Broken (Cheapest)
        # Treat as SAFE. Global Cost = 0.
        Bundles.Add(NewBundle(Type: SAFE, Tasks: LocalTasks, Job: Job))

    ELSE:
        # CASE B: Healthy
        # Identify SAFE tasks (must satisfy BOTH Global AND Role constraints)
        SafeTasks = List()
        CoreTasks = List()

        FOR task IN LocalTasks:
            Role = task.Role
            
            # Check if evicting this task violates either Global or Role constraints
            IsRoleSafe = !EnforceRoles OR (RoleSurplus[Role] > 0)

            IF GlobalSurplus > 0 AND IsRoleSafe:
                SafeTasks.Add(task)
                GlobalSurplus--      # Decrement available budget
                IF EnforceRoles: RoleSurplus[Role]--  # Decrement role budget
            ELSE:
                CoreTasks.Add(task)

        IF SafeTasks Is Not Empty:
            Bundles.Add(NewBundle(Type: SAFE, Tasks: SafeTasks, Job: Job))

        IF CoreTasks Is Not Empty:
            Bundles.Add(NewBundle(Type: WHOLE, Tasks: CoreTasks, Job: Job))


# --- PHASE 2: Initial Sort (Plugin Presentation) ---
# Goal: Present "Cheapest" and "Most Fair" victims to the stateful plugins first.
# This ensures that limited quotas (e.g. Queue Deserved) are consumed by the preferred victims.
Sort Bundles using Comparator(Bundle A, Bundle B):

    # 1. GANG STATE (Absolute Rule)
    # Safe/Broken bundles are "Free". Whole bundles are "Expensive".
    IF A.Type != B.Type:
        RETURN A.Type < B.Type # Safe (0) < Whole (1)

    # 2. QUEUE HIERARCHY (Plugin: VictimQueueOrderFn)
    # e.g., Reclaim from over-quota queues first.
    Result = Session.VictimQueueOrderFn(A.Job.Queue, B.Job.Queue, Preemptor.Queue)
    IF Result != EQUAL:
        RETURN Result

    # 3. GLOBAL EFFICIENCY (Topology Rule - Only for WHOLE bundles)
    # If both are expensive, pick the one with lower Global Cost per Local Gain.
    # We use a weighted ROI (Return on Investment) metric.
    # ROI = LocalGainScore / GlobalCostScore
    
    ROI_A = CalculateROI(A, Preemptor.Request, Cluster.Capacity)
    ROI_B = CalculateROI(B, Preemptor.Request, Cluster.Capacity)
    
    # Threshold check prevents overriding User Priority for negligible efficiency gains.
    # Recommended Value: 0.05 (5% difference required to ignore priority)
    IF Abs(ROI_A - ROI_B) > Threshold:
        RETURN ROI_A > ROI_B

    # 4. JOB PRIORITY / POLICIES (Plugin: JobOrderFn)
    # Standard plugin logic (Priority, DRF, etc.)
    Result = Session.JobOrderFn(A.Job, B.Job)
    IF Result != EQUAL:
        RETURN Result

    # 5. TIE-BREAKER
    RETURN A.Job.CreationTime < B.Job.CreationTime


# --- PHASE 3: Global Plugin Validation (Stateful Check) ---
# CRITICAL: We must validate ALL sorted candidates in one batch.
# Plugins like 'capacity' track cumulative queue usage (allocations) during the check.
# If we checked job-by-job, the plugin would reset its state, causing "Double Counting" 
# where multiple jobs from the same queue are all approved for eviction, 
# violating the queue's minimum 'Deserved' guarantee.

# 1. Flatten Sorted Bundles to Ordered Task List
OrderedCandidates = Bundles.SelectMany(b => b.Tasks)

# 2. Call ReclaimableFn ONCE
# The plugin will approve tasks in order until constraints (like Queue Deserved) are met.
# It acts as a "Cut-Off" filter on our priority list.
AllowedTasksSet = Session.Reclaimable(Preemptor.AnyTask, OrderedCandidates).ToSet()

# 3. Prune & Shrink Bundles
ValidBundles = List()
FOR EACH Bundle IN Bundles:
    IF Bundle.Tasks.All(t => AllowedTasksSet.Contains(t)):
        # All tasks in bundle are reclaimable -> Keep Bundle
        ValidBundles.Add(Bundle)
    ELSE IF Bundle.Type == SAFE:
        # For SAFE (surplus) bundles, partial eviction is allowed.
        # We take whatever the plugin allows.
        AllowedPart = Bundle.Tasks.Where(t => AllowedTasksSet.Contains(t))
        IF AllowedPart Is Not Empty:
            Bundle.Tasks = AllowedPart
            ValidBundles.Add(Bundle)
    # ELSE: Reject WHOLE bundle if any part is rejected by plugin.


# --- PHASE 4: Final Sort (Selection Optimization) ---
# Goal: Re-rank valid bundles based on final resource values.
# Since "Safe" bundles may have shrunk in Phase 3, their ROI/Coverage metrics changed.
Sort ValidBundles using Comparator(Bundle A, Bundle B):
    # Repeat the same sorting in Phase 2

# --- PHASE 5: Selection ---
Victims = List()
ResourcesFreed = 0

FOR EACH Bundle IN ValidBundles:
    # Logic note: Because "Safe" bundles always sort before "Whole" bundles 
    # for the same job, we don't need complex state tracking here.
    
    Victims.Add(Bundle.Tasks)
    ResourcesFreed += Bundle.LocalResources

    IF ResourcesFreed >= NeededResources:
        BREAK

RETURN Victims
```

### 3.2.2 Bundle Scoring (ROI)

To handle heterogeneous resources (CPU, Memory, GPU), we define **Local Gain** and **Global Cost** using a weighted normalization strategy.

1.  **Local Gain Score** (Preemptor-Centric):
    *   Measures utility to the preemptor.
    *   Normalize `Bundle.LocalResources` against `Preemptor.NeededResources`.
    *   *Formula*: `Sum(Min(Local[i], Needed[i]) / Needed[i])` for each resource dimension `i` where `Needed[i] > 0`.
    *   *Why*: If preemptor needs GPU, a victim freeing CPU has 0 utility.

2.  **Global Cost Score** (Preemptor-Centric):
    *   Measures total disruption relative to the need.
    *   Normalize `Bundle.GlobalResources` against `Preemptor.NeededResources`.
    *   *Formula*: `Sum(Global[i] / Needed[i])` for each resource dimension `i` where `Needed[i] > 0`.
    *   *Why*: We calculate the "Opportunity Cost" strictly in terms of the resources we are trying to obtain. If I need CPU, the "cost" of a victim is defined by how many CPUs I am destroying globally. This ensures apples-to-apples comparison between Local Gain and Global Cost.

3.  **Efficiency (ROI)**:
    *   `Efficiency = LocalGainScore / GlobalCostScore`
    *   We prioritize victims that solve the local bottleneck with the least global cluster disruption.

### 3.3 New Action Structure

```go
package gang_preempt

func (a *Action) Execute(ssn *framework.Session) {
    // Iterate Jobs
    for _, job := range ssn.Jobs {
        // ...
        
        // 1. Fast Fail (GangPredicate)
        if !ssn.GangPredicatesPass(job) { continue }
        
        // 2. Get Valid Search Space (GangSearchSpace)
        validDomains := ssn.GetGangValidDomains(job) // [[NodeA, NodeB], [NodeC, NodeD]]
        
        // 3. Greedy Domain Search
        for _, domain := range validDomains {
             
             // 4. Gang-Aware Victim Search (Domain-Wide)
             // Returns optimal victims across the entire domain based on Bundle Strategy
             victims, err := ssn.SelectGangVictimsInDomain(job, domain)
             if err != nil { continue }
             
             // 5. Simulate & Execute
             if victims != nil && SimulateAndExecute(domain, victims, job) {
                 break
             }
        }
    }
}
```

---

### 3.4 Action-Specific Implementation

Instead of introducing complex new plugin interfaces, we implement distinct logic for `gang-preempt` and `gang-reclaim` to handle their differing policy requirements, while sharing core "Gang Utility" functions.

#### 3.4.1 Shared Utility (Mechanism)
Both actions utilize the same underlying mechanisms provided by the `Session` or helper packages:
1.  **Search Space**: `ssn.GetGangValidDomains(job)`
2.  **Bundle Logic**: `util.CreateBundles(candidates)` (Categorizes tasks into Safe/Whole bundles)
3.  **Efficiency Check**: `util.CalculateROI(bundle, needed)`

#### 3.4.2 Gang-Preempt Victim Selection Strategy (Priority-Driven)
*   **Goal**: Satisfy high-priority jobs by evicting low-priority ones with minimal disruption.
*   **Filter**: Strictly `Preemptor.Priority > Victim.Priority`.
*   **Ordering**:
    1.  **Gang State**: Prefer `Safe` bundles (Priority 0) over `Whole` bundles (Priority 1).
    2.  **Job Priority**: Prefer evicting lower priority victims first.
    3.  **Efficiency**: Break ties with ROI (Global Cost).

#### 3.4.3 Gang-Reclaim Selection Strategy (Fairness-Driven)
*   **Goal**: Recover resources from over-quota queues for under-quota queues.
*   **Filter**: Respect `Queue.Reclaimable()` and `Queue.Overused()`.
*   **Ordering**:
    1.  **Gang State**: Prefer `Safe` bundles.
    2.  **Queue Fairness**: Prefer evicting from queues that are most over-quota (`ssn.VictimQueueOrderFn`).
    3.  **Efficiency**: Break ties with ROI.
    4.  **Job Priority**: Only used as a final tie-breaker.

This separation ensures that Preemption never violates priority for efficiency, while Reclaim always respects queue fairness guarantees.
