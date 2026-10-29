package localstate

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// asking is one command asking the queue for the lock in a goroutine: what
// it was told while waiting, and how Acquire returned.
type asking struct {
	mutex    sync.Mutex
	told     []ports.LockWait
	release  func(outcome error)
	err      error
	returned chan struct{}
}

func ask(ctx context.Context, queue *Queue, command string, folder string) *asking {
	request := &asking{returned: make(chan struct{})}
	go func() {
		defer close(request.returned)
		request.release, request.err = queue.Acquire(ctx, ports.LockHolder{Command: command, Caller: "a terminal", Folder: folder}, func(wait ports.LockWait) {
			request.mutex.Lock()
			defer request.mutex.Unlock()
			request.told = append(request.told, wait)
		})
	}()
	return request
}

// waitUntilTold waits until the request has been told count times.
func (request *asking) waitUntilTold(t *testing.T, count int) []ports.LockWait {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		request.mutex.Lock()
		told := append([]ports.LockWait(nil), request.told...)
		request.mutex.Unlock()
		if len(told) >= count {
			return told
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("not told %d times", count)
	return nil
}

func (request *asking) waitReturned(t *testing.T) {
	t.Helper()
	select {
	case <-request.returned:
	case <-time.After(5 * time.Second):
		t.Fatalf("Acquire did not return")
	}
}

func (request *asking) hasReturned() bool {
	select {
	case <-request.returned:
		return true
	default:
		return false
	}
}

func TestQueue(t *testing.T) {
	since := time.Date(2026, 10, 29, 14, 2, 7, 0, time.Local)
	newQueue := func(t *testing.T) *Queue {
		return &Queue{Store: Store{Home: t.TempDir()}, Clock: func() time.Time { return since }}
	}
	t.Run("changes take the lock one at a time in the order they ask, each told what it waits for", func(t *testing.T) {
		queue := newQueue(t)
		first := ask(context.Background(), queue, "rein sync --all", "/work")
		first.waitReturned(t)
		second := ask(context.Background(), queue, "rein org use acme", "/work")
		second.waitUntilTold(t, 1)
		third := ask(context.Background(), queue, "rein login", "/home")
		told := third.waitUntilTold(t, 1)
		if told[0].Ahead != 2 || told[0].Holder.Command != "rein sync --all" || told[0].Holder.String() != "rein sync --all in /work, run from a terminal since 14:02:07" {
			t.Fatalf("third was told %+v", told)
		}
		if first.err != nil || second.hasReturned() || third.hasReturned() {
			t.Fatalf("first %v, second returned %v, third returned %v", first.err, second.hasReturned(), third.hasReturned())
		}
		first.release(nil)
		second.waitReturned(t)
		told = third.waitUntilTold(t, 2)
		if told[1].Ahead != 1 || told[1].Holder.Command != "rein org use acme" || third.hasReturned() {
			t.Fatalf("after the first, third was told %+v", told)
		}
		second.release(nil)
		third.waitReturned(t)
		if third.err != nil {
			t.Fatalf("third: %v", third.err)
		}
		third.release(nil)
	})
	t.Run("an identical command waits for the one in line and ends as it did", func(t *testing.T) {
		queue := newQueue(t)
		first := ask(context.Background(), queue, "rein sync", "/work/shop")
		first.waitReturned(t)
		elsewhere := ask(context.Background(), queue, "rein sync", "/work/other")
		identical := ask(context.Background(), queue, "rein sync", "/work/shop")
		told := identical.waitUntilTold(t, 1)
		if !told[0].Joining || told[0].Holder.Folder != "/work/shop" {
			t.Fatalf("told %+v", told)
		}
		refused := errors.New("refused: not logged in")
		first.release(refused)
		identical.waitReturned(t)
		var joined ports.Joined
		if !errors.As(identical.err, &joined) || !errors.Is(identical.err, refused) || joined.Holder.Command != "rein sync" {
			t.Fatalf("identical: %v", identical.err)
		}
		elsewhere.waitReturned(t)
		if elsewhere.err != nil {
			t.Fatalf("the same command in another folder is not identical: %v", elsewhere.err)
		}
		elsewhere.release(nil)
	})
	t.Run("an identical command takes the place of one that left the line before its turn", func(t *testing.T) {
		queue := newQueue(t)
		first := ask(context.Background(), queue, "rein sync --all", "/work")
		first.waitReturned(t)
		ctx, cancel := context.WithCancel(context.Background())
		leaving := ask(ctx, queue, "rein sync", "/work/shop")
		leaving.waitUntilTold(t, 1)
		identical := ask(context.Background(), queue, "rein sync", "/work/shop")
		identical.waitUntilTold(t, 1)
		cancel()
		leaving.waitReturned(t)
		if !errors.Is(leaving.err, context.Canceled) {
			t.Fatalf("leaving: %v", leaving.err)
		}
		told := identical.waitUntilTold(t, 2)
		if told[1].Joining || told[1].Holder.Command != "rein sync --all" || identical.hasReturned() {
			t.Fatalf("identical was told %+v", told)
		}
		first.release(nil)
		identical.waitReturned(t)
		if identical.err != nil {
			t.Fatalf("identical: %v", identical.err)
		}
		identical.release(nil)
	})
	t.Run("the first in line waits for another rein process that holds the lock file", func(t *testing.T) {
		queue := newQueue(t)
		other := Store{Home: queue.Store.Home}
		lock := other.Home + "/" + lockFile
		err := os.WriteFile(lock, []byte("1"), 0o644)
		if err != nil {
			t.Fatalf("write: %v", err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		defer cancel()
		first := ask(ctx, queue, "rein sync", "/work/shop")
		told := first.waitUntilTold(t, 1)
		if told[0].Holder.Process != 1 {
			t.Fatalf("told %+v", told)
		}
		first.waitReturned(t)
		if first.err == nil {
			t.Fatalf("the lock of a live process was taken")
		}
		queue.mutex.Lock()
		inLine := len(queue.line)
		queue.mutex.Unlock()
		if inLine != 0 {
			t.Fatalf("%d left in line", inLine)
		}
	})
}
