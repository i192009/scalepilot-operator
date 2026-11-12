# Executable scenarios

`cmd/loadgen` and `cmd/benchmark` accept steady, spike, ramp, burst and overload profiles, a deterministic seed, job type/duration, rate and bounded total count. The benchmark additionally injects Redis outage, worker crash or controller restart after ten seconds.

Run CPU-heavy and I/O-heavy profiles separately with the same requests/minimum/maximum. Use --job-type=long --job-duration=5s for long-running work. Preserve all raw JSON and hardware/version metadata. The report explicitly identifies which combinations actually ran; the scenario matrix is not evidence of completed experiments.
