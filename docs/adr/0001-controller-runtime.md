# ADR 1: controller-runtime and Kubernetes scale subresources

Accepted. Use Kubebuilder's go/v4 project format and controller-runtime informers, queues, leader election, metrics and health infrastructure. Keep business decisions in a pure Go engine. This provides native reconciliation/retry semantics and enables API-server integration tests without a cluster.

Use the `/scale` subresource with optimistic resource versions instead of overwriting full Deployment specifications. Retries revalidate the original observation; if another actor changed replicas, abort for reevaluation. Availability of a scale API does not prove ready execution capacity.
