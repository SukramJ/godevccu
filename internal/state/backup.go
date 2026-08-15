// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package state

import (
	"fmt"
	"time"
)

// Backup lifecycle.
//
// A CCU backup runs in the background: the start script forks
// createBackup.sh and returns a pid, and the status script reports
// "running" until the process is gone and the .sbk file exists, then
// "completed" with the file name, path and size.
//
// The simulator models the same progression but resolves it lazily, on
// read: [Manager.BackupStatus] promotes a running backup to completed
// once the configured duration has elapsed. That keeps the state
// machine deterministic and avoids a background goroutine, which would
// need a stop path it has nowhere to hang off.
//
// With the delay left at zero a backup stays "running" forever, which
// is the established behaviour.

// SetBackupCompletionDelay makes a started backup complete after d.
// Pass 0 to disable the automation. Set from
// virtualccu.Realism.BackupAPI.
func (m *Manager) SetBackupCompletionDelay(d time.Duration) {
	m.mu.Lock()
	m.backupDelay = d
	m.mu.Unlock()
}

// backupFilename builds the name a CCU gives its archive:
// <hostname>-<firmware>-<date>-<time>.sbk.
func (m *Manager) backupFilename(at time.Time) string {
	return fmt.Sprintf("%s-%s-%s.sbk",
		m.backendInfo.Hostname,
		m.backendInfo.Version,
		at.Format("2006-01-02-1504"),
	)
}

// promoteBackupLocked completes a running backup whose time is up. The
// caller must hold the write lock.
func (m *Manager) promoteBackupLocked() {
	if m.backupDelay <= 0 || m.backupStatus.Status != "running" {
		return
	}
	if time.Since(m.backupStartedAt) < m.backupDelay {
		return
	}
	// The archive content is a placeholder: the lifecycle is what a
	// client tests, the bytes of a .sbk are not observable through any
	// API the simulator serves.
	name := m.backupFilename(time.Now())
	data := []byte("godevccu backup placeholder\n")
	m.backupData = data
	m.backupStatus = BackupStatus{
		Status:   "completed",
		PID:      m.backupStatus.PID,
		Filename: name,
		Filepath: "/usr/local/tmp/last_backup.sbk",
		Size:     len(data),
	}
}
