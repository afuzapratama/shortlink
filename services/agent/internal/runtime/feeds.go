package runtime

import (
	"context"
	"errors"
	"log/slog"
	"shortlink.local/risk"
	"time"
)

func (a *Agent) RunFeeds(ctx context.Context) {
	delay := time.Duration(0)
	failures := 0
	for {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if a.blocked.Load() {
			return
		}
		if e := a.feeds.Update(ctx); e != nil {
			if errors.Is(e, risk.ErrCredential) {
				_ = a.rejectCredential()
				return
			}
			if ctx.Err() != nil {
				return
			}
			slog.Warn("risk feed unavailable")
			failures++
			if failures > 5 {
				failures = 5
			}
			delay = time.Duration(1<<failures) * 15 * time.Second
		} else {
			failures = 0
			delay = 15 * time.Minute
		}
	}
}
