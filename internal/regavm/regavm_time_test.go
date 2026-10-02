// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package regavm_test

import (
	"strconv"
	"testing"
	"testing/synctest"
	"time"
)

// TestLocaltimeRendersTheCurrentTime pins the localtime namespace: Format
// renders the current time through the strftime subset, the bare call uses
// "%F %T", and ToInteger yields the Unix seconds. The bubble clock makes
// "now" a single exact instant, so the expectations are computed from it
// rather than racing a second boundary.
func TestLocaltimeRendersTheCurrentTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Off midnight, so hour and minute verbs are distinguishable.
		time.Sleep(13*time.Hour + 4*time.Minute + 5*time.Second)
		now := time.Now()

		for _, tc := range []struct {
			script string
			want   string
		}{
			{`Write(localtime.Format("%Y-%m-%d %H:%M:%S"));`, now.Format("2006-01-02 15:04:05")},
			{`Write(localtime.Format());`, now.Format("2006-01-02 15:04:05")},
			{`Write(localtime.ToInteger());`, strconv.FormatInt(now.Unix(), 10)},
		} {
			res, err := newInterpreter(newSource()).Run(tc.script)
			if err != nil {
				t.Fatalf("%s: Run: %v", tc.script, err)
			}
			if res.Output != tc.want {
				t.Errorf("%s = %q, want %q", tc.script, res.Output, tc.want)
			}
		}
	})
}
