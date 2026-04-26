// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

// Package embed exposes the device and paramset description JSON files
// shipped from pydevccu. The files are embedded at build time so that
// the resulting binary is self-contained.
//
// To refresh the data set, run script/copy_data.sh and rebuild.
package embed

import (
	"embed"
	"io/fs"
)

//go:embed all:data/device_descriptions
var deviceDescriptionsFS embed.FS

//go:embed all:data/paramset_descriptions
var paramsetDescriptionsFS embed.FS

// DeviceDescriptions returns the embedded device description files,
// rooted at "device_descriptions" so the caller iterates filenames
// directly.
func DeviceDescriptions() fs.FS {
	sub, err := fs.Sub(deviceDescriptionsFS, "data/device_descriptions")
	if err != nil {
		// Impossible: the path is fixed by the //go:embed directive.
		panic(err)
	}
	return sub
}

// ParamsetDescriptions returns the embedded paramset description files,
// rooted at "paramset_descriptions".
func ParamsetDescriptions() fs.FS {
	sub, err := fs.Sub(paramsetDescriptionsFS, "data/paramset_descriptions")
	if err != nil {
		panic(err)
	}
	return sub
}
