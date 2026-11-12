// Package queue implements an at-least-once Redis Streams queue with atomic,
// idempotent example effects. One consumer group is supported per queue.
package queue

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Job struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	DurationMS int64  `json:"durationMs"`
	Fail       bool   `json:"fail,omitempty"`
	Tenant     string `json:"tenant,omitempty"`
}
type Queue struct {
	Client      *redis.Client
	Name, Group string
	claimCursor string
}

func (q Queue) Key(s string) string { return "scalepilot:{" + q.Name + "}:" + s }
func NewClient(data map[string][]byte, enabledTLS bool, serverName string) (*redis.Client, error) {
	address := string(data["address"])
	if address == "" || strings.Contains(address, "@") || strings.Contains(address, "://") {
		return nil, fmt.Errorf("redis address must be host:port without credentials")
	}
	opts := &redis.Options{Addr: address, Username: string(data["username"]), Password: string(data["password"]), DialTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second, MaxRetries: 2, MinRetryBackoff: 50 * time.Millisecond, MaxRetryBackoff: 200 * time.Millisecond, PoolSize: 4, MinIdleConns: 0, ConnMaxIdleTime: time.Minute, ContextTimeoutEnabled: true}
	if enabledTLS {
		config := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}
		if ca := data["ca.crt"]; len(ca) > 0 {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(ca) {
				return nil, fmt.Errorf("invalid Redis CA")
			}
			config.RootCAs = pool
		}
		if len(data["tls.crt"]) > 0 || len(data["tls.key"]) > 0 {
			cert, err := tls.X509KeyPair(data["tls.crt"], data["tls.key"])
			if err != nil {
				return nil, fmt.Errorf("invalid Redis client certificate")
			}
			config.Certificates = []tls.Certificate{cert}
		}
		opts.TLSConfig = config
	}
	return redis.NewClient(opts), nil
}
func ClientFromEnv() (*redis.Client, error) {
	data := map[string][]byte{"address": []byte(os.Getenv("REDIS_ADDRESS")), "username": []byte(os.Getenv("REDIS_USERNAME")), "password": []byte(os.Getenv("REDIS_PASSWORD"))}
	if len(data["address"]) == 0 {
		data["address"] = []byte("localhost:6379")
	}
	for _, v := range []struct{ env, key string }{{"REDIS_CA_FILE", "ca.crt"}, {"REDIS_CERT_FILE", "tls.crt"}, {"REDIS_KEY_FILE", "tls.key"}} {
		if path := os.Getenv(v.env); path != "" {
			b, e := os.ReadFile(path)
			if e != nil {
				return nil, e
			}
			data[v.key] = b
		}
	}
	return NewClient(data, os.Getenv("REDIS_TLS") == "true", os.Getenv("REDIS_SERVER_NAME"))
}
func (q Queue) Ensure(ctx context.Context) error {
	err := q.Client.XGroupCreateMkStream(ctx, q.Key("jobs"), q.Group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	groups, err := q.Client.XInfoGroups(ctx, q.Key("jobs")).Result()
	if err != nil {
		return err
	}
	if len(groups) != 1 || groups[0].Name != q.Group {
		return fmt.Errorf("exactly one consumer group per queue is required")
	}
	return nil
}

var enqueueScript = redis.NewScript(`
local t=redis.call('TIME');local sec=tonumber(t[1]);local b=KEYS[2]..sec
local id=redis.call('XADD',KEYS[1],'*','job',ARGV[1])
redis.call('HINCRBY',b,'arrived',1);redis.call('EXPIRE',b,180)
return id`)

func (q Queue) Enqueue(ctx context.Context, j Job) (string, error) {
	if j.ID == "" || j.DurationMS < 1 || j.DurationMS > 3600000 {
		return "", fmt.Errorf("invalid job ID or duration (1ms..1h)")
	}
	b, e := json.Marshal(j)
	if e != nil {
		return "", e
	}
	return enqueueScript.Run(ctx, q.Client, []string{q.Key("jobs"), q.Key("stats:")}, string(b)).Text()
}

type Delivery struct {
	Message redis.XMessage
	Job     Job
}

func (q *Queue) Claim(ctx context.Context, consumer string, idle time.Duration) (*Delivery, error) {
	if q.claimCursor == "" {
		q.claimCursor = "0-0"
	}
	msgs, next, err := q.Client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: q.Key("jobs"), Group: q.Group, Consumer: consumer, MinIdle: idle, Start: q.claimCursor, Count: 1}).Result()
	if err != nil {
		return nil, err
	}
	q.claimCursor = next
	if len(msgs) == 0 {
		streams, e := q.Client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: q.Group, Consumer: consumer, Streams: []string{q.Key("jobs"), ">"}, Count: 1, Block: time.Second}).Result()
		if errors.Is(e, redis.Nil) {
			return nil, nil
		}
		if e != nil {
			return nil, e
		}
		if len(streams) == 0 || len(streams[0].Messages) == 0 {
			return nil, nil
		}
		msgs = streams[0].Messages
	}
	d := &Delivery{Message: msgs[0]}
	s, ok := d.Message.Values["job"].(string)
	if !ok {
		return d, fmt.Errorf("malformed job")
	}
	if e := json.Unmarshal([]byte(s), &d.Job); e != nil {
		return d, fmt.Errorf("malformed job JSON")
	}
	return d, nil
}

var ownershipScript = redis.NewScript(`
local p=redis.call('XPENDING',KEYS[1],ARGV[1],ARGV[2],ARGV[2],1)
if #p==0 or p[1][2]~=ARGV[3] then return 0 end
redis.call('XCLAIM',KEYS[1],ARGV[1],ARGV[3],0,ARGV[2],'JUSTID')
return 1`)

func (q Queue) Heartbeat(ctx context.Context, consumer, id string) error {
	n, e := ownershipScript.Run(ctx, q.Client, []string{q.Key("jobs")}, q.Group, id, consumer).Int()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("delivery ownership lost")
	}
	return nil
}

var attemptScript = redis.NewScript(`local n=redis.call('HINCRBY',KEYS[1],ARGV[1],1);return n`)

func (q Queue) Attempt(ctx context.Context, id string) (int, error) {
	return attemptScript.Run(ctx, q.Client, []string{q.Key("attempts")}, id).Int()
}

var finishScript = redis.NewScript(`
local p=redis.call('XPENDING',KEYS[1],ARGV[1],ARGV[2],ARGV[2],1)
if #p==0 or p[1][2]~=ARGV[3] then return 0 end
local t=redis.call('TIME');local sec=tonumber(t[1]);local b=KEYS[2]..sec
if ARGV[5]=='failed' then
 redis.call('XADD',KEYS[4],'MAXLEN','~',10000,'*','originalID',ARGV[2],'job',ARGV[7])
 redis.call('HINCRBY',b,'failed',1)
 redis.call('XADD',KEYS[6],'MAXLEN','~',10000,'*','state','failed','duration',ARGV[4],'wait',ARGV[6])
elseif redis.call('SET',KEYS[3],'committed','NX','EX',86400) then
 -- The committed marker is the example's idempotent effect. A real external
 -- effect needs its own transactional idempotency boundary.
 redis.call('HINCRBY',b,'completed',1)
 redis.call('HINCRBYFLOAT',b,'duration',ARGV[4])
 redis.call('HINCRBYFLOAT',b,'wait',ARGV[6])
 redis.call('XADD',KEYS[6],'MAXLEN','~',10000,'*','state','completed','duration',ARGV[4],'wait',ARGV[6])
 local d=tonumber(ARGV[4]);local w=tonumber(ARGV[6]);local buckets={0.01,0.05,0.1,0.25,0.5,1,2,5,10,30,60,300,3600}
 for _,v in ipairs(buckets) do if d<=v then redis.call('HINCRBY',b,'le:'..v,1) end;if w<=v then redis.call('HINCRBY',b,'waitle:'..v,1) end end
end
redis.call('EXPIRE',b,180)
redis.call('XACK',KEYS[1],ARGV[1],ARGV[2]);redis.call('XDEL',KEYS[1],ARGV[2]);redis.call('HDEL',KEYS[5],ARGV[2])
return 1`)

func (q Queue) Finish(ctx context.Context, consumer string, d Delivery, elapsed time.Duration, failed bool) error {
	state := "completed"
	if failed {
		state = "failed"
	}
	j, _ := json.Marshal(d.Job)
	ms, _ := strconv.ParseInt(strings.Split(d.Message.ID, "-")[0], 10, 64)
	wait := max(0, time.Since(time.UnixMilli(ms)).Seconds()-elapsed.Seconds())
	n, e := finishScript.Run(ctx, q.Client, []string{q.Key("jobs"), q.Key("stats:"), q.Key("done:" + d.Job.ID), q.Key("failed"), q.Key("attempts"), q.Key("results")}, q.Group, d.Message.ID, consumer, max(.000001, elapsed.Seconds()), state, wait, string(j)).Int()
	if e != nil {
		return e
	}
	if n != 1 {
		return fmt.Errorf("delivery ownership lost before acknowledgement")
	}
	return nil
}

// Snapshot is atomic relative to enqueue/ack. XINFO lag is unread work, XPENDING
// is in-flight work. Stream length is deliberately not used as backlog.
var snapshotScript = redis.NewScript(`
local groups=redis.call('XINFO','GROUPS',KEYS[1]);local lag=nil;local last=nil
if #groups~=1 then return redis.error_reply('expected exactly one consumer group') end
local g=groups[1];local name=nil
for i=1,#g,2 do if g[i]=='name' then name=g[i+1] elseif g[i]=='lag' then lag=g[i+1] elseif g[i]=='last-delivered-id' then last=g[i+1] end end
if name~=ARGV[1] then return redis.error_reply('unexpected consumer group') end
local p=redis.call('XPENDING',KEYS[1],ARGV[1]);local oldest=nil
-- Redis can invalidate entries-read/lag around deletions. This protocol has one
-- group and atomically XACK+XDELs every completed/dead-lettered entry, so retained
-- stream entries are exactly unread + PEL. Only under that invariant is this
-- fallback exact. XLEN alone is never treated as unread backlog.
if type(lag)~='number' or lag<0 then
 lag=redis.call('XLEN',KEYS[1])-p[1]
 if lag<0 then return redis.error_reply('queue retention invariant violated') end
end
local unread=redis.call('XRANGE',KEYS[1],'('..last,'+','COUNT',1)
if #unread>0 then oldest=tonumber(string.match(unread[1][1],'^(%d+)')) end
if p[1]>0 then local v=tonumber(string.match(p[2],'^(%d+)'));if not oldest or v<oldest then oldest=v end end
local t=redis.call('TIME');local sec=tonumber(t[1]);local now=sec+tonumber(t[2])/1000000
local sums={};for i=0,59 do local b=redis.call('HGETALL',KEYS[2]..(sec-i));for j=1,#b,2 do sums[b[j]]=(sums[b[j]] or 0)+tonumber(b[j+1]) end end
local count=sums.completed or 0;local p95=0;local buckets={0.01,0.05,0.1,0.25,0.5,1,2,5,10,30,60,300,3600}
if count>0 then for _,v in ipairs(buckets) do if (sums['le:'..v] or 0)>=count*0.95 then p95=v;break end end end
return {tostring(now),tostring(lag),tostring(p[1]),tostring(oldest and math.max(0,now-oldest/1000) or 0),tostring((sums.arrived or 0)/60),tostring(count/60),tostring((sums.failed or 0)/60),tostring(count>0 and sums.duration/count or 0),tostring(p95),tostring(count)}`)

func (q Queue) Snapshot(ctx context.Context) (*api.QueueStatus, error) {
	values, e := snapshotScript.Run(ctx, q.Client, []string{q.Key("jobs"), q.Key("stats:")}, q.Group).StringSlice()
	if e != nil {
		return nil, e
	}
	if len(values) != 10 {
		return nil, fmt.Errorf("invalid Redis snapshot")
	}
	nums := make([]float64, 10)
	for i, s := range values {
		nums[i], e = strconv.ParseFloat(s, 64)
		if e != nil {
			return nil, fmt.Errorf("invalid Redis snapshot value")
		}
	}
	return &api.QueueStatus{CollectedAt: metav1.NewTime(time.Unix(0, int64(nums[0]*1e9))), Pending: int64(nums[1]), InFlight: int64(nums[2]), OldestAgeSeconds: nums[3], ArrivalRate: nums[4], CompletionRate: nums[5], FailureRate: nums[6], AverageProcessingSeconds: nums[7], P95ProcessingSeconds: nums[8], ExecutionSamples: int64(nums[9])}, nil
}
