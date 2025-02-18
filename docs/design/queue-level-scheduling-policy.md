Queue Level Scheduling Policy
---

by [@mahdikhashan](http://github.com/mahdikhashan); 18 Feb, 2025.

# Motivation

The current global scheduling policy in Volcano is pretty rigid, applying the same rules to all queues. This doesn't cut it for the diverse needs of modern workloads, especially in multi-tenant setups. Different jobs—like online services that need quick responses and offline batch tasks that focus on resource efficiency—require their own scheduling strategies. By introducing queue-level scheduling policies, we can allow admins to tailor behaviors for each queue, leading to better resource use and support for different teams.

This change opens up some exciting possibilities. For instance, in a multi-tenant data science platform, research teams could use FairShare policies for fair resource distribution while production ML jobs could benefit from BinPacking strategies. Cloud providers could optimize mixed workloads with FIFO policies for batch processing and GPU-aware policies for machine learning tasks. Finally, this enhancement would boost performance and user experience.

---

# API

To support queue-level scheduling policies, I propose the following changes to the Queue Custom Resource Definition (CRD):

```yaml
apiVersion: scheduling.volcano.sh/v1beta1
kind: Queue
metadata:
  name: data-science-queue
spec:
  weight: 10
  priority: 100
  schedulingPolicy:
    type: "FairShare"
    parameters:
      param1: "value1"
      param2: "value2"
```

In this example:

- A new `schedulingPolicy` field is added to the Queue spec.
- The `type` subfield specifies the name of the scheduling policy (e.g., "FairShare").
- The `parameters` subfield allows for policy-specific configurations.

---
