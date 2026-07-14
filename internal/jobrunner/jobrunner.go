// Package jobrunner runs a single processor job under a bounded
// graceful-then-forced shutdown policy, streaming processor.Result values to
// a caller-supplied callback per processor.Request.OnResult's documented
// contract, except that Run may stop forwarding results (see Run's doc) if
// the job does not respect cancellation within the configured policy.
package jobrunner

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/mtn-man/mintmedia/internal/processor"
	"github.com/mtn-man/mintmedia/internal/shutdown"
)

// ErrAbandoned is returned by Run when a job is abandoned after
// policy.Grace+policy.Force elapses without the job returning. Callers should
// check for it with errors.Is rather than inspecting drain.TimedOut.
var ErrAbandoned = errors.New("jobrunner: job abandoned after shutdown timeout")

// Run executes proc against path, forwarding each processor.Result to
// onResult as it is produced, until either:
//   - the job completes normally (err is the job's error, drain is the
//     zero shutdown.Result), or
//   - shutdownCtx is canceled, in which case Run applies policy via
//     shutdown.Drain: it first waits up to policy.Grace for the in-flight
//     job to finish on its own, then cancels the job's own (internal,
//     detached) context and waits up to policy.Force more.
//
// If the job does not return within policy.Grace+policy.Force after
// shutdownCtx is canceled, Run gives up on it: err is ErrAbandoned (check via
// errors.Is), and any onResult call the job makes AFTER that point is dropped
// rather than delivered late. This preserves the OnResult contract from the
// caller's point of view: once Run has returned, no further onResult calls
// will occur, even though the abandoned goroutine may still be running (and
// is allowed to finish or leak on its own; Run does not wait for it past the
// force timeout). drain is still returned in this case for its timing
// metadata (e.g. drain.GraceElapsed); callers should use errors.Is(err,
// ErrAbandoned), not drain.TimedOut, to detect abandonment.
//
// onResult is invoked synchronously and in order, on the goroutine that
// called Run, matching processor.Request.OnResult's documented contract, up
// until the point (if any) where Run gives up.
func Run(
	shutdownCtx context.Context,
	policy shutdown.Policy,
	hooks shutdown.Hooks,
	proc processor.Processor,
	path string,
	onResult func(processor.Result),
) (err error, drain shutdown.Result) {
	itemCtx, cancelItem := context.WithCancel(context.Background())
	defer cancelItem()

	done := make(chan error, 1)
	resultEvents := make(chan processor.Result)
	itemClosed := make(chan struct{})
	var closeOnce sync.Once
	closeItemClosed := func() {
		closeOnce.Do(func() { close(itemClosed) })
	}

	go func() {
		runErr := processor.ProcessEach(itemCtx, proc, processor.Request{InputPath: path},
			func(r processor.Result) {
				// itemClosed guards this send so an abandoned job (one Run
				// has given up waiting for) can't block forever trying to
				// deliver a result nobody will read anymore.
				select {
				case resultEvents <- r:
				case <-itemClosed:
				}
			})
		done <- runErr
	}()

	var (
		runErr   error
		gotFinal bool
	)

	waitForResult := func(timeout time.Duration) bool {
		if gotFinal {
			return true
		}
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		for !gotFinal {
			select {
			case r := <-resultEvents:
				onResult(r)
			case runErr = <-done:
				gotFinal = true
				return true
			case <-timer.C:
				return false
			}
		}
		return true
	}

	for !gotFinal {
		select {
		case r := <-resultEvents:
			onResult(r)
		case runErr = <-done:
			gotFinal = true
		case <-shutdownCtx.Done():
			drain = shutdown.Drain(policy, true, waitForResult, cancelItem, hooks)
			if drain.TimedOut {
				closeItemClosed()
				return ErrAbandoned, drain
			}
			// Grace or force wait succeeded: waitForResult already set
			// runErr/gotFinal via the "done" case above.
		}
	}

	closeItemClosed()
	return runErr, drain
}
