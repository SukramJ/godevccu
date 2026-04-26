// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package devicelogic

import (
	"context"
	"math/rand/v2"
	"time"
)

// hmSenMDIRWM55 simulates the HM-Sen-MDIR-WM55 motion detector
// (VCU0000274). Mirrors pydevccu/device_logic/HM_Sen_MDIR_WM55.py.
type hmSenMDIRWM55 struct {
	*runner
	rpc     RPC
	address string
	counter int
	lowBat  bool
}

func newHMSenMDIRWM55(rpc RPC, startupDelay, interval time.Duration) Device {
	d := &hmSenMDIRWM55{
		runner:  newRunner("HM-Sen-MDIR-WM55", Config{StartupDelay: startupDelay, Interval: interval}),
		rpc:     rpc,
		address: "VCU0000274",
		counter: 1,
	}
	d.start(d.work)
	return d
}

func (d *hmSenMDIRWM55) work(ctx context.Context) {
	if !sleepWithCancel(ctx, randomDelay(d.cfg.StartupDelay)) {
		return
	}
	for {
		if d.rpc.Active() {
			cur, err := d.rpc.GetValue(d.address+":3", "MOTION")
			if err == nil {
				if d.counter%5 == 0 {
					d.lowBat = !d.lowBat
					d.rpc.FireEvent(d.Name(), d.address+":0", "LOWBAT", d.lowBat)
				}
				_ = d.rpc.SetValue(d.address+":3", "MOTION", !truthy(cur), true)
				_ = d.rpc.SetValue(d.address+":3", "BRIGHTNESS", randomBrightness(), true)
				d.rpc.FireEvent(d.Name(), d.address+":1", "PRESS_SHORT", true)
				d.counter++
			}
		}
		if !sleepWithCancel(ctx, d.cfg.Interval) {
			return
		}
	}
}

// randomBrightness picks a value in [60, 90] mirroring the Python
// implementation's random.randint(60, 90).
func randomBrightness() int {
	return 60 + rand.IntN(31)
}
