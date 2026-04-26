// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package ccu hosts the simulator's XML-RPC server. It manages the
// device universe, paramset state, callback proxies and event firing —
// the Go counterpart of pydevccu/ccu.py.
package ccu

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	emb "github.com/SukramJ/godevccu/internal/embed"
)

// loadedDeviceSet groups everything we get from a single
// device_descriptions/<TYPE>.json + paramset_descriptions/<TYPE>.json
// pair.
type loadedDeviceSet struct {
	devices        []map[string]any
	paramsetByAddr map[string]map[string]any
	rootDeviceAddr string
	deviceTypeKey  string // e.g. "HM-Sec-SC-2", filename-derived
}

// loadAllDevices walks the embedded data set and returns one entry per
// device type. When restrict is non-empty only the named device types
// are loaded.
func loadAllDevices(restrict []string) ([]loadedDeviceSet, error) {
	dd := emb.DeviceDescriptions()
	pd := emb.ParamsetDescriptions()

	allowed := map[string]struct{}{}
	for _, name := range restrict {
		allowed[name] = struct{}{}
	}

	out := make([]loadedDeviceSet, 0)
	err := fs.WalkDir(dd, ".", func(path string, dEntry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if dEntry.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		// pydevccu derives the device-type name from the filename:
		// "HM-Sec-SC-2.json" → "HM-Sec-SC-2".
		base := strings.TrimSuffix(path, ".json")
		devName := strings.ReplaceAll(base, "_", " ")

		if len(allowed) > 0 {
			if _, ok := allowed[devName]; !ok {
				return nil
			}
		}

		devs, err := readJSONArray(dd, path)
		if err != nil {
			return fmt.Errorf("device descriptions %s: %w", path, err)
		}
		ps, err := readJSONObject(pd, path)
		if err != nil {
			return fmt.Errorf("paramset descriptions %s: %w", path, err)
		}

		set := loadedDeviceSet{deviceTypeKey: devName}
		set.devices = devs
		set.paramsetByAddr = make(map[string]map[string]any, len(ps))
		for addr, raw := range ps {
			obj, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			set.paramsetByAddr[addr] = obj
		}
		// pydevccu records the root device (no ":" in ADDRESS) as the
		// "supported device" representative.
		for _, d := range devs {
			addr, _ := d["ADDRESS"].(string)
			if addr != "" && !strings.Contains(addr, ":") {
				set.rootDeviceAddr = addr
				break
			}
		}
		out = append(out, set)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(allowed) > 0 && len(out) == 0 {
		return nil, errors.New("ccu: no device descriptions matched the restrict list")
	}
	return out, nil
}

// readJSONArray decodes a JSON file holding a top-level array of
// objects.
func readJSONArray(f fs.FS, path string) ([]map[string]any, error) {
	raw, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// readJSONObject decodes a JSON file holding a top-level object.
func readJSONObject(f fs.FS, path string) (map[string]any, error) {
	raw, err := fs.ReadFile(f, path)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
