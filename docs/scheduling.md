# Schedule semantics

Windows use IANA timezone locations with Go's embedded tzdata. A local wall-clock start is inclusive; end is exclusive. The interval must not be empty. Overnight windows belong to their starting weekday: Monday 22:00..02:00 includes Tuesday 01:00. Calendar arithmetic, not fixed UTC offsets, identifies the previous local date.

Rules sort by descending numeric priority, then ascending name. The first active rule wins. The effective minimum is the greater of base and scheduled minima. A scheduled maximum may lower the base maximum, never increase it. Resource budgets further restrict the maximum. Impossible scheduled minima fail closed rather than exceed a budget.

Polling occurs at the configured metric collection period. A boundary is reflected at the next evaluation; there is no sub-second cron scheduler. Cooldown and rate limits still apply to schedule-driven changes.

For DST, repeated local minutes in the autumn belong to the window on both occurrences; nonexistent spring-forward minutes never occur. The tests cover start/end boundaries, overnight/year boundaries, London spring/autumn transitions, overlapping priority, and deterministic reevaluation from persisted state. If jurisdictions change timezone legislation, rebuild with updated Go/tzdata; a fixed offset is not an adequate replacement.
