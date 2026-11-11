// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package litefake

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Add-on notices.
//
// An add-on posts notices that stand beside the system's warnings on the
// box's Status page. The routes are open: the credential is the add-on's
// control token as a Bearer, which names the add-on and reaches that
// add-on's notices alone.

const (
	// DefaultAddonID is the add-on the fake holds a control token for.
	DefaultAddonID = "openccu-loom"
	// DefaultAddonControlToken is that add-on's control token until
	// [Fake.SetAddonControlToken] replaces it.
	DefaultAddonControlToken = "litefake-addon-control-token"

	maxAddonNotices   = 20
	maxNoticeTitleLen = 160
	maxNoticeTextLen  = 1000
	maxNoticeTTL      = 366 * 24 * 60 * 60
)

// noticeIDPattern is the shape of a notice id.
var noticeIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// NoticeText is a notice's title or text in both languages.
type NoticeText struct {
	DE string `json:"de"`
	EN string `json:"en"`
}

// AddonNotice is one notice as the box holds and answers it.
type AddonNotice struct {
	ID       string      `json:"id"`
	Severity string      `json:"severity"`
	Title    NoticeText  `json:"title"`
	Text     *NoticeText `json:"text,omitempty"`
	Link     string      `json:"link,omitempty"`
	Expires  string      `json:"expires,omitempty"`
	Posted   string      `json:"posted"`
	Updated  string      `json:"updated"`
}

// noticeStore holds the default add-on's notices in posting order.
type noticeStore struct {
	mu      sync.Mutex
	token   string
	notices []AddonNotice
	// puts counts the notice writes the box accepted, new and replacing.
	puts int
}

func newNoticeStore() *noticeStore { return &noticeStore{token: DefaultAddonControlToken} }

func (f *Fake) noticeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system/v1/addon-notices", f.handleNoticeList)
	mux.HandleFunc("DELETE /api/system/v1/addon-notices", f.handleNoticeClear)
	mux.HandleFunc("PUT /api/system/v1/addon-notices/{id}", f.handleNoticePut)
	mux.HandleFunc("DELETE /api/system/v1/addon-notices/{id}", f.handleNoticeDelete)
}

// noticeAddon checks the control token and answers 401 without a valid
// one.
func (f *Fake) noticeAddon(w http.ResponseWriter, r *http.Request) bool {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "the addon's control token is required")
		return false
	}
	s := f.notices
	s.mu.Lock()
	valid := token == s.token
	s.mu.Unlock()
	if !valid {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "unknown addon control token")
		return false
	}
	return true
}

type noticeListAnswer struct {
	Addon   string        `json:"addon"`
	Max     int           `json:"max"`
	Notices []AddonNotice `json:"notices"`
}

func (f *Fake) handleNoticeList(w http.ResponseWriter, r *http.Request) {
	if !f.noticeAddon(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, noticeListAnswer{Addon: DefaultAddonID, Max: maxAddonNotices, Notices: f.AddonNotices()})
}

type noticeBody struct {
	Severity string      `json:"severity"`
	Title    NoticeText  `json:"title"`
	Text     *NoticeText `json:"text"`
	Link     string      `json:"link"`
	TTL      int         `json:"ttl"`
}

// invalidNotice names the first field of a notice the box refuses, or
// "" for a notice it takes.
func invalidNotice(id string, b noticeBody) string {
	switch {
	case !noticeIDPattern.MatchString(id):
		return "id"
	case b.Severity != "error" && b.Severity != "warning" && b.Severity != "info":
		return "severity"
	case b.Title.DE == "" || b.Title.EN == "" ||
		utf8.RuneCountInString(b.Title.DE) > maxNoticeTitleLen || utf8.RuneCountInString(b.Title.EN) > maxNoticeTitleLen:
		return "title"
	case b.Text != nil && (b.Text.DE == "" || b.Text.EN == "" ||
		utf8.RuneCountInString(b.Text.DE) > maxNoticeTextLen || utf8.RuneCountInString(b.Text.EN) > maxNoticeTextLen):
		return "text"
	case b.Link != "" && (!strings.HasPrefix(b.Link, "/") || strings.Contains(b.Link, "//")):
		return "link"
	case b.TTL < 0 || b.TTL > maxNoticeTTL:
		return "ttl"
	}
	return ""
}

// handleNoticePut posts a notice or replaces the one of that id: 201 for
// a new one, 200 for a replaced one, whose first posting time stays.
func (f *Fake) handleNoticePut(w http.ResponseWriter, r *http.Request) {
	if !f.noticeAddon(w, r) {
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad-request", "unreadable body")
		return
	}
	var body noticeBody
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad-request", err.Error())
		return
	}
	id := r.PathValue("id")
	if field := invalidNotice(id, body); field != "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid", field)
		return
	}
	now := time.Now().UTC()
	n := AddonNotice{
		ID: id, Severity: body.Severity, Title: body.Title, Text: body.Text, Link: body.Link,
		Posted: now.Format(time.RFC3339), Updated: now.Format(time.RFC3339),
	}
	if body.TTL > 0 {
		n.Expires = now.Add(time.Duration(body.TTL) * time.Second).Format(time.RFC3339)
	}
	s := f.notices
	s.mu.Lock()
	status := http.StatusCreated
	replaced := false
	for i := range s.notices {
		if s.notices[i].ID == id {
			n.Posted = s.notices[i].Posted
			s.notices[i] = n
			status, replaced = http.StatusOK, true
			break
		}
	}
	if !replaced {
		if len(s.notices) >= maxAddonNotices {
			s.mu.Unlock()
			writeError(w, http.StatusConflict, "too-many", "an addon holds at most 20 notices")
			return
		}
		s.notices = append(s.notices, n)
	}
	s.puts++
	s.mu.Unlock()
	writeJSON(w, status, n)
}

type noticeOK struct {
	OK bool `json:"ok"`
}

func (f *Fake) handleNoticeDelete(w http.ResponseWriter, r *http.Request) {
	if !f.noticeAddon(w, r) {
		return
	}
	id := r.PathValue("id")
	s := f.notices
	s.mu.Lock()
	found := false
	for i := range s.notices {
		if s.notices[i].ID == id {
			s.notices = append(s.notices[:i], s.notices[i+1:]...)
			found = true
			break
		}
	}
	s.mu.Unlock()
	if !found {
		writeError(w, http.StatusNotFound, "not_found", "no such notice")
		return
	}
	writeJSON(w, http.StatusOK, noticeOK{OK: true})
}

type noticeCleared struct {
	OK      bool `json:"ok"`
	Cleared int  `json:"cleared"`
}

func (f *Fake) handleNoticeClear(w http.ResponseWriter, r *http.Request) {
	if !f.noticeAddon(w, r) {
		return
	}
	writeJSON(w, http.StatusOK, noticeCleared{OK: true, Cleared: f.DropAddonNotices()})
}

// AddonNotices returns the notices the box holds, in posting order.
func (f *Fake) AddonNotices() []AddonNotice {
	s := f.notices
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AddonNotice{}, s.notices...)
}

// AddonNoticePuts returns how many notice writes the box accepted, new
// and replacing, so a test can tell an unchanged notice left alone from
// one written again.
func (f *Fake) AddonNoticePuts() int {
	s := f.notices
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.puts
}

// DropAddonNotices removes every notice, as the box does when it loses
// them, and returns how many there were.
func (f *Fake) DropAddonNotices() int {
	s := f.notices
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.notices)
	s.notices = nil
	return n
}

// SetAddonControlToken replaces the add-on's control token, as a restart
// of the box's service does; the previous one is then unknown.
func (f *Fake) SetAddonControlToken(token string) {
	s := f.notices
	s.mu.Lock()
	s.token = token
	s.mu.Unlock()
}
