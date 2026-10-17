package handler

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const replicaPingTimeout = 500 * time.Millisecond

type replicaPool struct {
	master   *redis.Client
	replicas []*redis.Client
	next     atomic.Uint64
}

func newReplicaPool(master *redis.Client, addrs []string) *replicaPool {
	pool := &replicaPool{master: master}
	for _, addr := range addrs {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		pool.replicas = append(pool.replicas,
			redis.NewClient(&redis.Options{Addr: addr, ClientName: "go-back-read"}))
	}

	return pool
}

func (p *replicaPool) reader(ctx context.Context) redis.Cmdable {
	for i := 0; i < len(p.replicas); i++ {
		cli := p.replicas[int(p.next.Add(1)-1)%len(p.replicas)]

		pingCtx, cancel := context.WithTimeout(ctx, replicaPingTimeout)
		healthy := cli.Ping(pingCtx).Err() == nil
		cancel()
		if healthy {
			return cli
		}
	}
	return p.master
}

func (p *replicaPool) close() {
	for _, cli := range p.replicas {
		_ = cli.Close()
	}
}
