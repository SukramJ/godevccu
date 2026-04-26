// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package ccu

import "time"

// nowFunc is overridable in tests so timestamp-bearing methods produce
// deterministic output.
var nowFunc = time.Now
