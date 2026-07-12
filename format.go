package powerlock

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Caller returns the first captured frame outside Powerlock, or "unavailable" when no such frame exists.
func (e LockEvent) Caller() string {
	frames := runtime.CallersFrames(e.Callers)
	for {
		frame, more := frames.Next()
		if frame.Function != "" && !strings.HasPrefix(frame.Function, "github.com/donomii/powerlock.") {
			return fmt.Sprintf("%s (%s:%d)", frame.Function, filepath.Base(frame.File), frame.Line)
		}
		if !more {
			return "unavailable"
		}
	}
}

// String returns an actionable single-line diagnostic containing timing, ownership, queue state, and caller.
func (e LockEvent) String() string {
	hold := "unmeasured"
	if e.HoldDurationKnown {
		hold = diagnosticDuration(e.HoldDuration)
	}
	return fmt.Sprintf(
		"powerlock event=%s lock=%q mode=%s result=%s contended=%t wait=%s hold=%s readers=%d writer=%t waiting_readers=%d waiting_writers=%d cancelled=%t caller=%s",
		e.Kind,
		e.Name,
		e.Mode,
		e.Result,
		e.Contended,
		diagnosticDuration(e.WaitDuration),
		hold,
		e.State.Readers,
		e.State.Writer,
		e.State.WaitingReaders,
		e.State.WaitingWriters,
		e.State.Cancelled,
		e.Caller(),
	)
}

func diagnosticDuration(duration time.Duration) string {
	if duration < 0 {
		return "invalid"
	}
	return duration.String()
}
