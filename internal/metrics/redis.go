package metrics

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	api "github.com/scalepilot/scalepilot-operator/api/v1alpha1"
	"github.com/scalepilot/scalepilot-operator/internal/queue"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sync"
	"time"
)

type Provider interface {
	Collect(context.Context, *api.WorkloadScalingPolicy) (*api.QueueStatus, error)
}
type connection struct {
	client      *redis.Client
	fingerprint [32]byte
	used        time.Time
}
type RedisProvider struct {
	Reader      client.Reader
	mu          sync.Mutex
	connections map[types.NamespacedName]connection
}

// Clients are shared by Secret identity, rotated on Secret contents and evicted
// after ten idle minutes. The map is bounded even under policy churn.
func (p *RedisProvider) Collect(ctx context.Context, policy *api.WorkloadScalingPolicy) (*api.QueueStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, policy.Spec.Metrics.Redis.Timeout.Duration)
	defer cancel()
	key := types.NamespacedName{Namespace: policy.Namespace, Name: policy.Spec.Metrics.Redis.SecretRef.Name}
	var secret corev1.Secret
	if e := p.Reader.Get(ctx, key, &secret); e != nil {
		return nil, fmt.Errorf("redis Secret unavailable")
	}
	config := policy.Spec.Metrics.Redis
	encoded, _ := json.Marshal(struct {
		Data   map[string][]byte
		TLS    bool
		Server string
	}{secret.Data, config.TLS, config.ServerName})
	fingerprint := sha256.Sum256(encoded)
	p.mu.Lock()
	if p.connections == nil {
		p.connections = map[types.NamespacedName]connection{}
	}
	for k, c := range p.connections {
		if time.Since(c.used) > 10*time.Minute {
			_ = c.client.Close()
			delete(p.connections, k)
		}
	}
	c, ok := p.connections[key]
	if !ok || c.fingerprint != fingerprint {
		if ok {
			_ = c.client.Close()
		}
		r, e := queue.NewClient(secret.Data, config.TLS, config.ServerName)
		if e != nil {
			p.mu.Unlock()
			return nil, e
		}
		c = connection{client: r, fingerprint: fingerprint}
	}
	c.used = time.Now()
	p.connections[key] = c
	p.mu.Unlock()
	sample, e := (queue.Queue{Client: c.client, Name: config.QueueName, Group: config.ConsumerGroup}).Snapshot(ctx)
	if e != nil {
		return nil, fmt.Errorf("redis metric collection failed")
	}
	return sample, nil
}
func (p *RedisProvider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, c := range p.connections {
		_ = c.client.Close()
		delete(p.connections, k)
	}
}
