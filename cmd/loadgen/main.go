package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/scalepilot/scalepilot-operator/internal/queue"
	"log/slog"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	var name, group, kind, pattern, tenant, run string
	var rate, burst float64
	var jobs int
	var duration, jobDuration time.Duration
	var seed uint64
	flag.StringVar(&name, "queue", "document-jobs", "queue name")
	flag.StringVar(&group, "group", "workers", "consumer group")
	flag.StringVar(&kind, "job-type", "io", "fast, medium, long, cpu, io")
	flag.StringVar(&pattern, "pattern", "steady", "steady, spike, ramp, burst, overload, recovery")
	flag.StringVar(&tenant, "tenant", "demo", "tenant identifier")
	flag.StringVar(&run, "run-id", "", "idempotency namespace; unique by default")
	flag.Float64Var(&rate, "rate", 20, "jobs/sec")
	flag.Float64Var(&burst, "burst", 10, "peak rate multiplier")
	flag.IntVar(&jobs, "jobs", 1000, "maximum total jobs")
	flag.DurationVar(&duration, "duration", time.Minute, "maximum run duration")
	flag.DurationVar(&jobDuration, "job-duration", 500*time.Millisecond, "mean processing time")
	flag.Uint64Var(&seed, "seed", 42, "deterministic random seed")
	flag.Parse()
	if rate <= 0 || rate > 100000 || burst < 1 || jobs < 1 || duration <= 0 || jobDuration < time.Millisecond || jobDuration > time.Hour {
		panic("invalid load settings")
	}
	switch pattern {
	case "steady", "spike", "ramp", "burst", "overload", "recovery":
	default:
		panic("unsupported traffic pattern")
	}
	switch kind {
	case "fast", "medium", "long", "cpu", "io":
	default:
		panic("unsupported job type")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	client, e := queue.ClientFromEnv()
	if e != nil {
		panic(e)
	}
	defer func() { _ = client.Close() }()
	q := queue.Queue{Client: client, Name: name, Group: group}
	if e = q.Ensure(ctx); e != nil {
		panic(e)
	}
	rng := rand.New(rand.NewPCG(seed, seed+1))
	start := time.Now()
	if run == "" {
		run = fmt.Sprintf("%s-%d", tenant, start.UnixNano())
	}
	count := 0
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	tokens := 0.0
	for count < jobs {
		select {
		case <-ctx.Done():
			report(count, start)
			return
		case <-ticker.C:
			elapsed := time.Since(start).Seconds()
			mult := 1.0
			switch pattern {
			case "spike":
				mult = burst
			case "ramp":
				mult = 1 + (burst-1)*min(1, elapsed/duration.Seconds())
			case "burst":
				if int(elapsed/10)%2 == 0 {
					mult = burst
				} else {
					mult = 0
				}
			case "overload":
				mult = burst
			case "recovery":
				mult = 1
			}
			tokens += rate * mult * .01
			for tokens >= 1 && count < jobs {
				ms := max(1, int64(jobDuration.Milliseconds())*int64(75+rng.IntN(51))/100)
				j := queue.Job{ID: fmt.Sprintf("%s-%d", run, count), Kind: kind, DurationMS: ms, Tenant: tenant}
				op, done := context.WithTimeout(ctx, 3*time.Second)
				_, err := q.Enqueue(op, j)
				done()
				if err != nil {
					slog.Error("enqueue failed", "enqueued", count)
					os.Exit(1)
				}
				count++
				tokens--
			}
		}
	}
	report(count, start)
}
func report(n int, start time.Time) {
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"enqueued": n, "elapsedSeconds": time.Since(start).Seconds()})
}
