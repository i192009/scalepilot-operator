# ADR 4: configured resource budgets, not scheduler prediction

Accepted. Divide exact CPU/memory budgets (minus reserves) by Pod requests, including init-sidecar and overhead semantics. Fail closed for missing requests or an infeasible minimum. Kubernetes remains responsible for scheduling; report desired and ready capacity separately. Cluster-wide free-capacity estimation is excluded because a node sum ignores placement and concurrent demand.
