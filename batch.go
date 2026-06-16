package webdavds

import (
	"context"
	"fmt"
	"strings"
	"sync"

	ds "github.com/ipfs/go-datastore"
)

// Batch accumulates Put/Delete operations and applies them concurrently on
// Commit, bounded by the client's concurrency semaphore.
func (d *Datastore) Batch(ctx context.Context) (ds.Batch, error) {
	return &batch{d: d, ops: make(map[string]batchOp)}, nil
}

type batchOp struct {
	value  []byte
	delete bool
}

type batch struct {
	d   *Datastore
	mu  sync.Mutex
	ops map[string]batchOp
}

func (b *batch) Put(ctx context.Context, k ds.Key, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ops[k.String()] = batchOp{value: value}
	return nil
}

func (b *batch) Delete(ctx context.Context, k ds.Key) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.ops[k.String()] = batchOp{delete: true}
	return nil
}

func (b *batch) Commit(ctx context.Context) error {
	b.mu.Lock()
	ops := b.ops
	b.ops = make(map[string]batchOp)
	b.mu.Unlock()

	conc := b.d.c.cfg.Concurrency
	if conc < 1 {
		conc = 1
	}
	type job struct {
		key string
		op  batchOp
	}
	jobs := make(chan job)
	errs := make(chan error, len(ops))

	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				k := ds.NewKey(j.key)
				var err error
				if j.op.delete {
					err = b.d.Delete(ctx, k)
				} else {
					err = b.d.Put(ctx, k, j.op.value)
				}
				if err != nil {
					errs <- fmt.Errorf("%s: %w", j.key, err)
				}
			}
		}()
	}
	for k, op := range ops {
		jobs <- job{key: k, op: op}
	}
	close(jobs)
	wg.Wait()
	close(errs)

	var msgs []string
	for err := range errs {
		msgs = append(msgs, err.Error())
	}
	if len(msgs) > 0 {
		return fmt.Errorf("webdavds: batch commit failed:\n%s", strings.Join(msgs, "\n"))
	}
	return nil
}
