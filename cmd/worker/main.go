package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"github.com/scalepilot/scalepilot-operator/internal/queue"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	var name, group, consumer string
	var reclaim, grace time.Duration
	var maxAttempts int
	flag.StringVar(&name, "queue", "document-jobs", "queue name")
	flag.StringVar(&group, "group", "workers", "consumer group")
	flag.StringVar(&consumer, "consumer", os.Getenv("HOSTNAME"), "unique consumer identity")
	flag.DurationVar(&reclaim, "reclaim-idle", 30*time.Second, "reclaim idle timeout")
	flag.DurationVar(&grace, "shutdown-grace", 25*time.Second, "grace to finish current job")
	flag.IntVar(&maxAttempts, "max-attempts", 3, "attempt limit before dead-letter")
	flag.Parse()
	if reclaim < 3*time.Second || maxAttempts < 1 || grace < 0 {
		panic("invalid worker settings")
	}
	if consumer == "" {
		consumer = fmt.Sprintf("worker-%d", os.Getpid())
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	client, e := queue.ClientFromEnv()
	if e != nil {
		panic(e)
	}
	defer func() { _ = client.Close() }()
	q := queue.Queue{Client: client, Name: name, Group: group}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	var ready atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Addr: ":8081", Handler: mux, ReadHeaderTimeout: 3 * time.Second}
	go func() {
		if e := server.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			slog.Error("health server failed")
		}
	}()
	defer func() {
		shutdown, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	for ctx.Err() == nil {
		op, cancel := context.WithTimeout(ctx, 5*time.Second)
		e = q.Ensure(op)
		cancel()
		if e != nil {
			ready.Store(false)
			slog.Warn("queue not ready")
			if !pause(ctx, time.Second) {
				break
			}
			continue
		}
		ready.Store(true)
		d, claimErr := q.Claim(ctx, consumer, reclaim)
		if claimErr != nil && d == nil {
			ready.Store(false)
			if ctx.Err() == nil {
				slog.Warn("claim failed")
				pause(ctx, time.Second)
			}
			continue
		}
		if d == nil {
			continue
		}
		// The delivery's context survives SIGTERM for a bounded grace period. No new
		// claims start after cancellation. Unfinished work stays in the PEL.
		jobCtx, cancelJob := context.WithCancel(context.Background())
		finished := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				timer := time.NewTimer(grace)
				defer timer.Stop()
				select {
				case <-timer.C:
					cancelJob()
				case <-finished:
				}
			case <-finished:
			}
		}()
		started := time.Now()
		attempt, e := q.Attempt(jobCtx, d.Message.ID)
		failed := claimErr != nil || d.Job.DurationMS < 1 || d.Job.DurationMS > 3600000 || d.Job.ID == ""
		if e == nil && !failed {
			ticker := time.NewTicker(reclaim / 3)
			heartbeatDone := make(chan struct{})
			go func() {
				defer close(heartbeatDone)
				for {
					select {
					case <-jobCtx.Done():
						return
					case <-ticker.C:
						ping, cancel := context.WithTimeout(jobCtx, 2*time.Second)
						err := q.Heartbeat(ping, consumer, d.Message.ID)
						cancel()
						if err != nil {
							cancelJob()
							return
						}
					}
				}
			}()
			e = execute(jobCtx, d.Job)
			ticker.Stop()
			cancelJob()
			<-heartbeatDone
			if e == nil && d.Job.Fail {
				e = fmt.Errorf("configured job failure")
			}
		}
		if (e == nil && !d.Job.Fail) || failed || attempt >= maxAttempts {
			ack, ackCancel := context.WithTimeout(context.Background(), 3*time.Second)
			err := q.Finish(ack, consumer, *d, time.Since(started), failed || e != nil)
			ackCancel()
			if err != nil {
				slog.Warn("acknowledgement deferred; delivery remains recoverable")
			}
		}
		cancelJob()
		close(finished)
	}
	ready.Store(false)
	slog.Info("worker stopped; unfinished jobs remain recoverable")
}
func execute(ctx context.Context, j queue.Job) error {
	duration := time.Duration(j.DurationMS) * time.Millisecond
	if j.Kind == "cpu" {
		end := time.Now().Add(duration)
		b := []byte(j.ID)
		for time.Now().Before(end) {
			for range 1000 {
				h := sha256.Sum256(b)
				b = h[:]
			}
			if e := ctx.Err(); e != nil {
				return e
			}
		}
		return nil
	}
	switch j.Kind {
	case "fast", "medium", "long", "io", "":
		return wait(ctx, duration)
	default:
		return fmt.Errorf("unsupported job kind")
	}
}
func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func pause(ctx context.Context, d time.Duration) bool { return wait(ctx, d) == nil }
