#!/usr/bin/env python3
# Copyright 2026 The Volcano Authors.
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#     http://www.apache.org/licenses/LICENSE-2.0
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Reproduce #5958 against a fresh, dedicated volcano-5958 Kind cluster."""

import argparse
import json
import pathlib
import subprocess
import time

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--kubeconfig", required=True, type=pathlib.Path)
parser.add_argument("--scheduler", required=True, type=pathlib.Path)
parser.add_argument("--evidence-dir", required=True, type=pathlib.Path)
parser.add_argument("--enable-fragmentation-order", action="store_true")
args = parser.parse_args()
P = args.evidence_dir.resolve()
P.mkdir(parents=True, exist_ok=True)
K = [
    "kubectl",
    "--kubeconfig",
    str(args.kubeconfig.resolve()),
    "--context",
    "kind-volcano-5958",
]
(P / "scheduler.conf").write_text("""actions: "allocate, gangreclaim"
tiers:
- plugins:
  - name: priority
  - name: gang
    enablePreemptable: false
  - name: capacity
    enableHierarchy: false
- plugins:
  - name: predicates
  - name: nodeorder
""")
if args.enable_fragmentation_order:
    with (P / "scheduler.conf").open("a") as config:
        config.write("""configurations:
- name: gangreclaim
  arguments:
    enableFragmentationAwareVictimOrder: true
""")


def kub(*args, obj=None):
    r = subprocess.run(
        K + list(args),
        input=json.dumps(obj) if obj is not None else None,
        text=True,
        capture_output=True,
    )
    if r.returncode:
        raise RuntimeError(r.stderr)
    return r.stdout


def get(*args):
    return json.loads(kub("get", *args, "-o", "json"))


def apply(items):
    kub("apply", "-f", "-", obj={"apiVersion": "v1", "kind": "List", "items": items})


def cpu(v):
    return int(v[:-1]) if str(v).endswith("m") else int(float(v) * 1000)


def pod(ns, name, node, group, count=1000, preemptable=False):
    md = {
        "name": name,
        "namespace": ns,
        "labels": {"volcano.sh/preemptable": str(preemptable).lower()},
    }
    if group:
        md["annotations"] = {"scheduling.k8s.io/group-name": group}
    spec = {
        "schedulerName": "volcano-5958" if group else "default-scheduler",
        "terminationGracePeriodSeconds": 0,
        "containers": [
            {
                "name": "pause",
                "image": "registry.k8s.io/pause:3.10",
                "imagePullPolicy": "IfNotPresent",
                "resources": {"requests": {"cpu": str(count) + "m"}},
            }
        ],
    }
    if node:
        spec["nodeName"] = node
    return {"apiVersion": "v1", "kind": "Pod", "metadata": md, "spec": spec}


def wait_running(ns, want, timeout=90):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        items = get("pods", "-n", ns)["items"]
        if len(items) == want and all(
            p.get("status", {}).get("phase") == "Running" for p in items
        ):
            return items
        time.sleep(1)
    raise RuntimeError(
        "victims did not become Running: " + kub("get", "pods", "-n", ns, "-o", "wide")
    )


nodes = sorted(
    n["metadata"]["name"]
    for n in get("nodes")["items"]
    if "node-role.kubernetes.io/control-plane" not in n["metadata"]["labels"]
)
if len(nodes) != 2 or not all(n.startswith("volcano-5958-worker") for n in nodes):
    raise RuntimeError("Expected two workers in a dedicated volcano-5958 Kind cluster")
if any(n["metadata"]["name"].startswith("repro-") for n in get("namespaces")["items"]):
    raise RuntimeError("Remove existing repro-* namespaces before running this harness")
if any(
    q["metadata"]["name"] in ["victim", "reclaimer"] for q in get("queues")["items"]
):
    raise RuntimeError("The harness requires unused victim/reclaimer queue names")
if kub("config", "current-context").strip() != "kind-volcano-5958":
    raise RuntimeError("The scheduler kubeconfig must select kind-volcano-5958")
summaries = []
for index, (spread_first, deserved) in enumerate(
    [(True, 4), (False, 4), (True, 6), (False, 6)]
):
    ns = "repro-" + str(index)
    spread, packed = (
        ("a-spread", "b-packed") if spread_first else ("b-spread", "a-packed")
    )
    items = [{"apiVersion": "v1", "kind": "Namespace", "metadata": {"name": ns}}]
    for name, amount in [("victim", deserved), ("reclaimer", 2)]:
        items.append(
            {
                "apiVersion": "scheduling.volcano.sh/v1beta1",
                "kind": "Queue",
                "metadata": {"name": name},
                "spec": {
                    "weight": 1,
                    "reclaimable": True,
                    "deserved": {"cpu": str(amount)},
                },
            }
        )
    for name, queue, min_member in [
        (spread, "victim", 1),
        (packed, "victim", 1),
        ("protected", "victim", 2),
        ("reclaimer", "reclaimer", 1),
    ]:
        items.append(
            {
                "apiVersion": "scheduling.volcano.sh/v1beta1",
                "kind": "PodGroup",
                "metadata": {"name": name, "namespace": ns},
                "spec": {"minMember": min_member, "queue": queue},
            }
        )
    apply(items)
    for q in ["victim", "reclaimer"]:
        kub(
            "patch",
            "queue",
            q,
            "--subresource=status",
            "--type=merge",
            "-p",
            json.dumps({"status": {"state": "Open"}}),
        )
    for name in [spread, packed, "protected"]:
        kub(
            "patch",
            "podgroup",
            name,
            "-n",
            ns,
            "--type=merge",
            "-p",
            json.dumps({"status": {"phase": "Running"}}),
        )
    kub(
        "patch",
        "podgroup",
        "reclaimer",
        "-n",
        ns,
        "--type=merge",
        "-p",
        json.dumps({"status": {"phase": "Inqueue"}}),
    )
    n1, n2 = nodes
    all_pods = get("pods", "-A")["items"]
    for node in nodes:
        alloc = cpu(get("node", node)["status"]["allocatable"]["cpu"])
        used = sum(
            cpu(c.get("resources", {}).get("requests", {}).get("cpu", "0"))
            for p in all_pods
            if p["spec"].get("nodeName") == node
            and p.get("status", {}).get("phase") not in ["Succeeded", "Failed"]
            for c in p["spec"]["containers"]
        )
        padding = alloc - used - 4000
        assert padding >= 0, (node, alloc, used)
        items = [pod(ns, "padding-" + node, node, None, padding)]
        apply(items)
    workload = [
        pod(ns, "spread-1", n1, spread, preemptable=True),
        pod(ns, "spread-2", n2, spread, preemptable=True),
        pod(ns, "spread-anchor", n1, spread),
        pod(ns, "packed-1", n2, packed, preemptable=True),
        pod(ns, "packed-2", n2, packed, preemptable=True),
        pod(ns, "packed-anchor", n1, packed),
        pod(ns, "protected-1", n1, "protected"),
        pod(ns, "protected-2", n2, "protected"),
    ]
    apply(workload)
    before = wait_running(ns, 10)
    (P / (ns + "-before.json")).write_text(json.dumps(before, indent=2))
    log = (P / (ns + "-scheduler.log")).open("w")
    proc = subprocess.Popen(
        [
            str(args.scheduler.resolve()),
            "--kubeconfig=" + str(args.kubeconfig.resolve()),
            "--scheduler-name=volcano-5958",
            "--scheduler-conf=" + str(P / "scheduler.conf"),
            "--leader-elect=false",
            "--listen-address=127.0.0.1:18085",
            "--cache-dumper=false",
            "--schedule-period=1s",
            "--v=4",
        ],
        stdout=log,
        stderr=subprocess.STDOUT,
    )
    try:
        apply([pod(ns, "reclaimer", None, "reclaimer", 2000)])
        end = time.monotonic() + 40
        while time.monotonic() < end:
            if proc.poll() is not None:
                raise RuntimeError(
                    "scheduler exited, see " + str(P / (ns + "-scheduler.log"))
                )
            current = get("pods", "-n", ns)["items"]
            r = next(p for p in current if p["metadata"]["name"] == "reclaimer")
            if r.get("status", {}).get("phase") == "Running":
                break
            time.sleep(1)
        after = get("pods", "-n", ns)["items"]
        events = get("events", "-n", ns)
        (P / (ns + "-after.json")).write_text(json.dumps(after, indent=2))
        (P / (ns + "-events.json")).write_text(json.dumps(events, indent=2))
        present = {p["metadata"]["uid"] for p in after}
        evicted = sorted(
            p["metadata"]["name"] for p in before if p["metadata"]["uid"] not in present
        )
        r = next(p for p in after if p["metadata"]["name"] == "reclaimer")
        summary = {
            "case": index,
            "fragmentation_order_enabled": args.enable_fragmentation_order,
            "spread_first": spread_first,
            "victim_deserved_cpu": deserved,
            "evicted": evicted,
            "reclaimer_phase": r["status"]["phase"],
            "reclaimer_node": r["spec"].get("nodeName"),
            "scheduler_exit": proc.poll(),
        }
        summaries.append(summary)
        print(json.dumps(summary), flush=True)
        (P / "summary.json").write_text(json.dumps(summaries, indent=2))
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=10)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait()
        log.close()
    kub("delete", "namespace", ns, "--wait=true", "--timeout=60s")
    kub("delete", "queue", "victim", "reclaimer", "--wait=true", "--timeout=60s")
print("SUMMARY", json.dumps(summaries), flush=True)
