package localstate

import (
	"context"
	"sync"
	"time"

	"github.com/Danidani23/agents-rein/internal/ports"
)

// Queue is the machine lock of the commands one process runs for its
// callers (D56): they take it one at a time in the order they ask (R2),
// and the first in line takes the store's lock file against another rein
// process. A command identical to one in line waits for that one and ends
// as it did, instead of running twice (R3). Reads take no lock (R1).
type Queue struct {
	Store Store
	// Clock stamps when each command asked; nil means the wall clock.
	Clock func() time.Time

	mutex sync.Mutex
	line  []*ticket
	// changed is closed and replaced each time the line changes.
	changed chan struct{}
}

var _ ports.MachineLock = (*Queue)(nil)

// ticket is one command in line. outcome and held are set before done is
// closed, when the command leaves the line.
type ticket struct {
	holder  ports.LockHolder
	done    chan struct{}
	held    bool
	outcome error
}

// queues are this process's queues, one per rein home.
var queues = struct {
	mutex  sync.Mutex
	byHome map[string]*Queue
}{byHome: map[string]*Queue{}}

// QueueOf is this process's queue for the home, the same for every
// command, so they take turns.
func QueueOf(home string) *Queue {
	queues.mutex.Lock()
	defer queues.mutex.Unlock()
	queue, found := queues.byHome[home]
	if !found {
		queue = &Queue{Store: Store{Home: home}}
		queues.byHome[home] = queue
	}
	return queue
}

// Acquire takes the lock for holder after the commands ahead of it, or
// joins an identical one in line; see ports.MachineLock.
func (queue *Queue) Acquire(ctx context.Context, holder ports.LockHolder, waiting func(ports.LockWait)) (release func(outcome error), err error) {
	if waiting == nil {
		waiting = func(ports.LockWait) {}
	}
	for {
		own, identical := queue.join(holder)
		if identical == nil {
			return queue.wait(ctx, own, waiting)
		}
		waiting(ports.LockWait{Holder: identical.holder, Joining: true})
		select {
		case <-identical.done:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		if identical.held {
			return nil, ports.Joined{Holder: identical.holder, Outcome: identical.outcome}
		}
		// the identical one left the line before its turn: this one takes its place
	}
}

// join puts holder at the end of the line, unless an identical command is
// in it already, which it returns instead.
func (queue *Queue) join(holder ports.LockHolder) (own *ticket, identical *ticket) {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	for _, other := range queue.line {
		if other.holder.Command == holder.Command && other.holder.Folder == holder.Folder {
			return nil, other
		}
	}
	holder.Since = queue.now()
	own = &ticket{holder: holder, done: make(chan struct{})}
	queue.line = append(queue.line, own)
	queue.announce()
	return own, nil
}

// wait waits for own's turn, telling waiting of the first in line each time
// it or the number ahead changes, then takes the lock file.
func (queue *Queue) wait(ctx context.Context, own *ticket, waiting func(ports.LockWait)) (release func(outcome error), err error) {
	told := ports.LockWait{}
	for {
		queue.mutex.Lock()
		ahead := queue.positionOf(own)
		first := queue.line[0].holder
		changed := queue.changed
		queue.mutex.Unlock()
		if ahead == 0 {
			break
		}
		wait := ports.LockWait{Holder: first, Ahead: ahead}
		if wait != told {
			waiting(wait)
			told = wait
		}
		select {
		case <-changed:
		case <-ctx.Done():
			queue.leave(own, ctx.Err())
			return nil, ctx.Err()
		}
	}
	releaseFile, err := queue.Store.acquireFile(ctx, waiting)
	if err != nil {
		queue.leave(own, err)
		return nil, err
	}
	queue.mutex.Lock()
	own.held = true
	queue.mutex.Unlock()
	var once sync.Once
	return func(outcome error) {
		once.Do(func() {
			releaseFile()
			queue.leave(own, outcome)
		})
	}, nil
}

// leave takes own out of the line with its outcome and wakes those behind.
func (queue *Queue) leave(own *ticket, outcome error) {
	queue.mutex.Lock()
	defer queue.mutex.Unlock()
	position := queue.positionOf(own)
	if position < 0 {
		return
	}
	queue.line = append(queue.line[:position], queue.line[position+1:]...)
	own.outcome = outcome
	close(own.done)
	queue.announce()
}

// positionOf is how many commands are ahead of own, -1 when it is not in
// line. The caller holds the mutex.
func (queue *Queue) positionOf(own *ticket) int {
	for index, other := range queue.line {
		if other == own {
			return index
		}
	}
	return -1
}

// announce wakes every waiting command. The caller holds the mutex.
func (queue *Queue) announce() {
	if queue.changed != nil {
		close(queue.changed)
	}
	queue.changed = make(chan struct{})
}

func (queue *Queue) now() time.Time {
	if queue.Clock == nil {
		return time.Now()
	}
	return queue.Clock()
}
