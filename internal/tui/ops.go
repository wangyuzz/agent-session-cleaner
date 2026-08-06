package tui

import (
	"context"
	"errors"
	"sync"

	tea "charm.land/bubbletea/v2"

	"github.com/haowang02/agent-session-cleaner/internal/i18n"
	"github.com/haowang02/agent-session-cleaner/internal/session"
)

type opKind uint8

const (
	opArchive opKind = iota
	opUnarchive
	opDelete
)

func (k opKind) verb() i18n.Key {
	return [...]i18n.Key{i18n.OpArchive, i18n.OpUnarchive, i18n.OpDelete}[k]
}

func (k opKind) progress() i18n.Key {
	return [...]i18n.Key{
		i18n.OpArchiveProgress, i18n.OpUnarchiveProgress, i18n.OpDeleteProgress,
	}[k]
}

func (k opKind) past() i18n.Key {
	return [...]i18n.Key{i18n.OpArchiveDone, i18n.OpUnarchiveDone, i18n.OpDeleteDone}[k]
}

// plan is what a keypress decided to do, before anything has been asked of the
// agent. It survives a confirmation dialog unchanged.
type plan struct {
	kind    opKind
	targets []session.Session
	// what names the category a batch swept, for the closing summary.
	what i18n.Key
	// single marks one session under the cursor with its sub-agents riding
	// along, which is worded differently from a batch and gives up at the
	// first refusal.
	single bool
	// subject is the row the cursor was on, for the single-session wording.
	subject session.Session
}

// opUpdate travels from the goroutine running an operation back to the model.
// The goroutine reports facts; the model does the wording it cannot know,
// such as what is still selected afterwards.
type opUpdate struct {
	progress string
	done     bool
	result   opResult
}

type opResult struct {
	plan     plan
	total    int
	settled  []string
	failures []error
	// stopped marks a cascade abandoned because a sub-agent refused, leaving
	// the session that spawned it untouched rather than newly orphaned.
	stopped bool
}

// errNoArchiver guards a path the interface should never offer: the keys for
// archiving do not exist unless the agent can.
var errNoArchiver = errors.New("this agent cannot archive")

// job is everything a running operation needs. It deliberately holds no
// reference to the model: once started, an operation reads nothing that another
// keypress could change underneath it.
type job struct {
	plan     plan
	apply    func(context.Context, opKind, session.Session) error
	print    *i18n.Printer
	parents  map[string]string
	branches [][]session.Session
	limit    int
}

func (m *Model) applier() func(context.Context, opKind, session.Session) error {
	target, archiver := m.agent, m.archiver
	return func(ctx context.Context, kind opKind, s session.Session) error {
		switch kind {
		case opArchive:
			if archiver == nil {
				return errNoArchiver
			}
			return archiver.Archive(ctx, s)
		case opUnarchive:
			if archiver == nil {
				return errNoArchiver
			}
			return archiver.Unarchive(ctx, s)
		default:
			return target.Delete(ctx, s)
		}
	}
}

func (m *Model) start(p plan) tea.Cmd {
	// Claimed here rather than in the goroutine: a worker does not begin until
	// the next event loop turn, which would let a double keypress fire twice.
	m.busy = true

	j := job{
		plan:    p,
		apply:   m.applier(),
		print:   m.print,
		parents: m.forest.Parents(),
		// How many at once is the agent's call: its sessions share one tree,
		// sometimes one file.
		limit: max(1, m.meta.BulkConcurrency),
	}
	if !p.single {
		j.branches = m.forest.Branches(p.targets)
	}

	updates := make(chan opUpdate, len(p.targets)+2)
	m.updates = updates
	ctx := m.ctx
	// Starting work is itself a Cmd side effect. Update only commits the busy
	// state and describes the immutable job; Bubble Tea owns when it begins.
	return func() tea.Msg {
		go func() {
			if p.single {
				runCascade(ctx, j, updates)
			} else {
				runBatch(ctx, j, updates)
			}
		}()
		update, ok := <-updates
		if !ok {
			return nil
		}
		return update
	}
}

func waitFor(updates <-chan opUpdate) tea.Cmd {
	return func() tea.Msg {
		update, ok := <-updates
		if !ok {
			return nil
		}
		return update
	}
}

// runCascade works one session and its sub-agents in order, deepest first.
//
// It gives up at the first refusal on purpose: a side thread exists only
// because of the conversation that spawned it, so removing that conversation
// while the side thread stays behind is exactly what strands one.
func runCascade(ctx context.Context, j job, updates chan<- opUpdate) {
	defer close(updates)

	result := opResult{plan: j.plan, total: len(j.plan.targets)}
	extra := len(j.plan.targets) - 1
	for i, target := range j.plan.targets {
		updates <- opUpdate{progress: j.cascadeProgress(target, i+1, extra)}

		if err := j.apply(ctx, j.plan.kind, target); err != nil {
			result.failures = append(result.failures, err)
			result.stopped = target.ID != j.plan.subject.ID
			break
		}
		result.settled = append(result.settled, target.ID)
	}
	updates <- opUpdate{done: true, result: result}
}

func (j job) cascadeProgress(target session.Session, done, extra int) string {
	if extra == 0 {
		return j.print.T(i18n.OperationProgressOne, i18n.Args{
			"operation": j.print.T(j.plan.kind.progress()),
			"title":     target.Title,
		})
	}
	return j.print.T(i18n.OperationProgress, i18n.Args{
		"operation": j.print.T(j.plan.kind.progress()),
		"done":      done,
		"total":     len(j.plan.targets),
		"title":     target.Title,
	})
}

// runBatch sweeps a whole set, several branches at a time.
//
// Unlike a cascade this does not give up at the first failure: a refusal says
// nothing about a session unrelated to it. What it does hold back is the chain
// the refusal hangs from, because removing a session whose own sub-agent stayed
// behind is what strands one. Siblings strand nothing and go ahead.
func runBatch(ctx context.Context, j job, updates chan<- opUpdate) {
	defer close(updates)

	var (
		mu       sync.Mutex
		settled  []string
		failures []error
	)
	total := len(j.plan.targets)
	limit := make(chan struct{}, j.limit)

	var running sync.WaitGroup
	for _, branch := range j.branches {
		running.Add(1)
		go func(branch []session.Session) {
			defer running.Done()

			// held lists what a failure further down has ruled out for this
			// run. A branch is ordered deepest first, so an ancestor is always
			// still ahead when the sub-agent blocking it refuses.
			held := make(map[string]bool)
			for _, target := range branch {
				if held[target.ID] {
					continue
				}

				limit <- struct{}{}
				err := j.apply(ctx, j.plan.kind, target)
				<-limit

				mu.Lock()
				if err != nil {
					failures = append(failures, err)
					for node := target.ID; ; {
						parent, known := j.parents[node]
						if !known || parent == "" || held[parent] {
							break
						}
						held[parent] = true
						node = parent
					}
					mu.Unlock()
					continue
				}
				settled = append(settled, target.ID)
				progress := j.print.T(i18n.BulkProgress, i18n.Args{
					"operation": j.print.T(j.plan.kind.progress()),
					"done":      len(settled),
					"total":     total,
				})
				mu.Unlock()

				updates <- opUpdate{progress: progress}
			}
		}(branch)
	}
	running.Wait()

	updates <- opUpdate{done: true, result: opResult{
		plan:     j.plan,
		total:    total,
		settled:  settled,
		failures: failures,
	}}
}

func (m *Model) summarise(r opResult) statusLine {
	if r.plan.single {
		return m.summariseCascade(r)
	}
	if len(r.failures) > 0 {
		return statusLine{tone: toneError, text: m.print.T(i18n.BulkFailed, i18n.Args{
			"operation": m.print.T(r.plan.kind.past()),
			"done":      len(r.settled),
			"total":     r.total,
			// Whatever is still there, whether it refused or was never reached
			// because its own sub-agent refused first. The numbers have to add
			// up to what the list still shows.
			"remaining": r.total - len(r.settled),
			"message":   m.print.Err(r.failures[0]),
		})}
	}
	return statusLine{tone: toneOK, text: m.print.T(i18n.BulkDone, i18n.Args{
		"operation": m.print.T(r.plan.kind.past()),
		"count":     r.total,
		"what":      m.print.Label(r.plan.what, r.total),
	})}
}

func (m *Model) summariseCascade(r opResult) statusLine {
	if len(r.failures) == 0 {
		extra := r.total - 1
		tail := ""
		if extra > 0 {
			tail = m.print.N(i18n.OperationTailOne, i18n.OperationTailMany, extra)
		}
		return statusLine{tone: toneOK, text: m.print.T(i18n.OperationDone, i18n.Args{
			"operation": m.print.T(r.plan.kind.past()),
			"title":     titleOf(m.print, r.plan.subject),
			"tail":      tail,
		})}
	}
	message := m.print.Err(r.failures[0])
	if r.stopped {
		return statusLine{tone: toneError, text: m.print.T(i18n.SubagentOperationFailed, i18n.Args{
			"operation": m.print.T(r.plan.kind.verb()),
			"message":   message,
		})}
	}
	return statusLine{tone: toneError, text: message}
}
