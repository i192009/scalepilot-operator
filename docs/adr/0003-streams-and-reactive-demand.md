# ADR 3: Redis Streams and measured reactive demand

Accepted. Use one consumer group and PEL recovery with atomic ownership-checked acknowledgement. A stream's total length is not queue backlog. Snapshot unread lag and PEL independently; measure service duration in workers and arrivals in producers. Bound telemetry retention and explicitly report missing samples.

Combine queue depth, remaining age budget, drain horizon/arrival pressure and measured execution capacity by taking the maximum valid recommendation. Apply schedules, exact resource budgets and durable anti-flapping policies afterward. Do not advertise forecasting or exactly-once external effects.
