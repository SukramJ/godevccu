// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package devicelogic

import (
	"context"
	"time"
)

// hmSecSC2 simulates the HM-Sec-SC-2 shutter contact (VCU0000240). It
// toggles STATE on every interval and flips LOWBAT every 5 events,
// matching pydevccu/device_logic/HM_Sec_SC_2.py.
type hmSecSC2 struct {
	*runner
	rpc     RPC
	address string
	counter int
	lowBat  bool
}

func newHMSecSC2(rpc RPC, startupDelay, interval time.Duration) Device {
	d := &hmSecSC2{
		runner:  newRunner("HM-Sec-SC-2", Config{StartupDelay: startupDelay, Interval: interval}),
		rpc:     rpc,
		address: "VCU0000240:1",
		counter: 1,
	}
	d.start(d.work)
	return d
}

func (d *hmSecSC2) work(ctx context.Context) {
	if !sleepWithCancel(ctx, randomDelay(d.cfg.StartupDelay)) {
		return
	}
	for {
		if d.rpc.Active() {
			cur, err := d.rpc.GetValue(d.address, "STATE")
			if err == nil {
				if d.counter%5 == 0 {
					d.lowBat = !d.lowBat
					d.rpc.FireEvent(d.Name(), d.address, "LOWBAT", d.lowBat)
				}
				_ = d.rpc.SetValue(d.address, "STATE", !truthy(cur), true)
				d.counter++
			}
		}
		if !sleepWithCancel(ctx, d.cfg.Interval) {
			return
		}
	}
}

func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case int:
		return x != 0
	case int32:
		return x != 0
	case int64:
		return x != 0
	case float32:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return false
}
