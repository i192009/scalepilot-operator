//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"github.com/scalepilot/scalepilot-operator/internal/queue"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"testing"
	"time"
)

func TestRedisStreamsRecoveryAndMetrics(t *testing.T) {
	ctx := context.Background()
	container, e := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: testcontainers.ContainerRequest{Image: "redis:7.4.7-alpine", ExposedPorts: []string{"6379/tcp"}, WaitingFor: wait.ForLog("Ready to accept connections")}, Started: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := container.Terminate(ctx); err != nil {
			t.Error(err)
		}
	})
	host, e := container.Host(ctx)
	if e != nil {
		t.Fatal(e)
	}
	port, e := container.MappedPort(ctx, "6379/tcp")
	if e != nil {
		t.Fatal(e)
	}
	client, e := queue.NewClient(map[string][]byte{"address": []byte(fmt.Sprintf("%s:%s", host, port.Port()))}, false, "")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = client.Close() }()
	q := queue.Queue{Client: client, Name: "integration", Group: "workers"}
	if e = q.Ensure(ctx); e != nil {
		t.Fatal(e)
	}
	for i := range 3 {
		if _, e = q.Enqueue(ctx, queue.Job{ID: fmt.Sprint(i), Kind: "io", DurationMS: 10}); e != nil {
			t.Fatal(e)
		}
	}
	sample, e := q.Snapshot(ctx)
	if e != nil || sample.Pending != 3 || sample.InFlight != 0 {
		t.Fatal(sample, e)
	}
	d, e := q.Claim(ctx, "crashing-worker", time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	sample, e = q.Snapshot(ctx)
	if e != nil || sample.Pending != 2 || sample.InFlight != 1 {
		t.Fatal("lag/PEL confused", sample, e)
	}
	// XGROUP SETID without ENTRIESREAD invalidates Redis's lag estimate. The
	// one-group atomic ack+delete invariant still yields exact unread depth.
	if e = client.XGroupSetID(ctx, q.Key("jobs"), q.Group, d.Message.ID).Err(); e != nil {
		t.Fatal(e)
	}
	sample, e = q.Snapshot(ctx)
	if e != nil || sample.Pending != 2 || sample.InFlight != 1 {
		t.Fatal("unknown lag fallback lost unread/PEL distinction", sample, e)
	}
	time.Sleep(25 * time.Millisecond)
	recovered, e := q.Claim(ctx, "replacement", time.Millisecond)
	if e != nil || recovered.Message.ID != d.Message.ID {
		t.Fatal("crashed worker delivery not reclaimed", e)
	}
	if e = q.Finish(ctx, "crashing-worker", *d, 10*time.Millisecond, false); e == nil {
		t.Fatal("stale owner acknowledged recovered delivery")
	}
	if e = q.Finish(ctx, "replacement", *recovered, 10*time.Millisecond, false); e != nil {
		t.Fatal(e)
	}
	// Same logical job is delivered again: effect is idempotent, delivery is not exactly once.
	if _, e = q.Enqueue(ctx, recovered.Job); e != nil {
		t.Fatal(e)
	}
	for range 3 {
		d, e = q.Claim(ctx, "replacement", time.Second)
		if e != nil || d == nil {
			t.Fatal(e)
		}
		if e = q.Finish(ctx, "replacement", *d, 20*time.Millisecond, false); e != nil {
			t.Fatal(e)
		}
	}
	sample, e = q.Snapshot(ctx)
	if e != nil || sample.Pending != 0 || sample.InFlight != 0 || sample.ExecutionSamples != 3 || sample.P95ProcessingSeconds <= 0 || sample.ArrivalRate <= 0 {
		t.Fatal("incorrect real worker metrics", sample, e)
	}
	if _, e = q.Enqueue(ctx, queue.Job{ID: "poison", Kind: "io", DurationMS: 1, Fail: true}); e != nil {
		t.Fatal(e)
	}
	d, e = q.Claim(ctx, "replacement", time.Second)
	if e != nil {
		t.Fatal(e)
	}
	for want := 1; want <= 3; want++ {
		n, e := q.Attempt(ctx, d.Message.ID)
		if e != nil || n != want {
			t.Fatal("attempt persistence", n, e)
		}
	}
	if e = q.Finish(ctx, "replacement", *d, time.Millisecond, true); e != nil {
		t.Fatal(e)
	}
	n, e := client.XLen(ctx, q.Key("failed")).Result()
	if e != nil || n != 1 {
		t.Fatal("dead letter missing", n, e)
	}
	if e = container.Stop(ctx, nil); e != nil {
		t.Fatal(e)
	}
	op, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, e = q.Snapshot(op); e == nil {
		t.Fatal("Redis outage did not fail")
	}
}
