// SPDX-License-Identifier: MIT
// Copyright (C) 2026 SukramJ.

package litefake

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Heating groups (/api/system/v1/groups): each group is a virtual device
// on the VirtualDevices interface ("INT000000N").

// Group is one group as the list and detail answers show it. A box
// numbers its groups and answers the id as a JSON number.
type Group struct {
	ID                    int      `json:"id"`
	Name                  string   `json:"name"`
	Type                  string   `json:"type"`
	TypeLabel             string   `json:"type_label"`
	Device                string   `json:"device"`
	Ref                   string   `json:"ref"`
	Members               []string `json:"-"`
	ForbidSingleOperation bool     `json:"-"`
}

// GroupMember is a channel as the groups API names it: a candidate of a
// group type, a member of a group, a device still to configure. A box
// answers the channel address as id and as serial, and the channel type.
type GroupMember struct {
	ID     string `json:"id"`
	Serial string `json:"serial"`
	Type   string `json:"type"`
}

// groupStore holds the groups and, per group type, the channels that
// type can take. The fake does not derive candidates from its devices:
// which channels a type takes is the box's group service's decision,
// which the contract does not describe, so a test seeds them
// ([Options.GroupCandidates]).
type groupStore struct {
	mu         sync.Mutex
	next       int
	groups     map[int]*Group
	candidates map[string][]GroupMember
}

func newGroupStore(candidates map[string][]GroupMember) *groupStore {
	c := make(map[string][]GroupMember, len(candidates))
	for typeID, members := range candidates {
		c[typeID] = append([]GroupMember{}, members...)
	}
	return &groupStore{groups: map[int]*Group{}, candidates: c}
}

// groupID reads the {id} path segment; a segment that is not a number
// names no group.
func groupID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil
}

// groupTypes are the group types the fake offers, with the labels a box
// answers for them.
func groupTypes() map[string]string {
	return map[string]string{
		"HomeMatic.heating":  "Heating_Control",
		"hmip.heating.group": "HmIP-Heizungssteuerung",
	}
}

func (f *Fake) groupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/system/v1/groups", f.sysRead(f.handleGroups))
	mux.HandleFunc("GET /api/system/v1/groups/types", f.sysRead(f.handleGroupTypes))
	mux.HandleFunc("GET /api/system/v1/groups/{id}", f.sysRead(f.handleGroup))
	mux.HandleFunc("POST /api/system/v1/groups", f.sysScope(scopeSystemWrite, f.handleGroupCreate))
	mux.HandleFunc("PUT /api/system/v1/groups/{id}", f.sysScope(scopeSystemWrite, f.handleGroupUpdate))
	mux.HandleFunc("DELETE /api/system/v1/groups/{id}", f.sysScope(scopeSystemWrite, f.handleGroupDelete))
}

type groupsAnswer struct {
	Groups             []Group       `json:"groups"`
	DevicesToConfigure []GroupMember `json:"devices_to_configure"`
}

func (s *groupStore) list() []Group {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Group, 0, len(s.groups))
	for _, g := range s.groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

func (f *Fake) handleGroups(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, groupsAnswer{Groups: f.system.groups.list(), DevicesToConfigure: []GroupMember{}})
}

// member resolves a member id of a group type to the channel the box names
// for it; false for an id that type cannot take. Callers hold s.mu.
func (s *groupStore) member(typeID, id string) (GroupMember, bool) {
	for _, c := range s.candidates[typeID] {
		if c.ID == id {
			return c, true
		}
	}
	return GroupMember{}, false
}

// members are a group's members as the box answers them. Callers hold
// s.mu.
func (s *groupStore) members(g *Group) []GroupMember {
	out := make([]GroupMember, 0, len(g.Members))
	for _, id := range g.Members {
		if m, ok := s.member(g.Type, id); ok {
			out = append(out, m)
		}
	}
	return out
}

// split sorts a type's candidates the way a box does: a channel that
// belongs to a group of that type is leftover, the rest are assignable.
// The members of the group named by own appear in neither list — that
// group's detail shows them as members. Observed on a box for groups of
// one type; a box with candidates for two types was not at hand, so a
// membership in a group of another type is not counted. Callers hold
// s.mu.
func (s *groupStore) split(typeID string, own *Group) (assignable, leftover []GroupMember) {
	taken := map[string]bool{}
	mine := map[string]bool{}
	for _, g := range s.groups {
		if g.Type != typeID {
			continue
		}
		for _, id := range g.Members {
			if g == own {
				mine[id] = true
			} else {
				taken[id] = true
			}
		}
	}
	assignable, leftover = []GroupMember{}, []GroupMember{}
	for _, c := range s.candidates[typeID] {
		switch {
		case mine[c.ID]:
		case taken[c.ID]:
			leftover = append(leftover, c)
		default:
			assignable = append(assignable, c)
		}
	}
	return assignable, leftover
}

type groupType struct {
	ID         string        `json:"id"`
	Label      string        `json:"label"`
	Assignable []GroupMember `json:"assignable"`
	Leftover   []GroupMember `json:"leftover"`
}

// sortedGroupTypeIDs are the offered type ids in the order a box lists
// them.
func sortedGroupTypeIDs() []string {
	types := groupTypes()
	ids := make([]string, 0, len(types))
	for id := range types {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (f *Fake) handleGroupTypes(w http.ResponseWriter, _ *http.Request) {
	types := groupTypes()
	s := f.system.groups
	s.mu.Lock()
	out := make([]groupType, 0, len(types))
	for _, id := range sortedGroupTypeIDs() {
		assignable, leftover := s.split(id, nil)
		out = append(out, groupType{ID: id, Label: types[id], Assignable: assignable, Leftover: leftover})
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, struct {
		Types []groupType `json:"types"`
	}{Types: out})
}

// groupTypeRef is a group type as a group's detail lists the offered
// ones: id and label, without candidates.
type groupTypeRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// groupDetail is one group as the box answers it on GET /groups/{id} and
// after a create or an update. Unlike the list entry it carries no
// type_label; Types are all offered types, not the group's own.
type groupDetail struct {
	ID                    int            `json:"id"`
	Name                  string         `json:"name"`
	Type                  string         `json:"type"`
	Device                string         `json:"device"`
	Ref                   string         `json:"ref"`
	DeviceName            string         `json:"device_name"`
	ForbidSingleOperation bool           `json:"forbid_single_operation"`
	Members               []GroupMember  `json:"members"`
	Assignable            []GroupMember  `json:"assignable"`
	Leftover              []GroupMember  `json:"leftover"`
	Types                 []groupTypeRef `json:"types"`
}

// detailOf renders a stored group. Callers hold s.mu.
func (s *groupStore) detailOf(g *Group) groupDetail {
	assignable, leftover := s.split(g.Type, g)
	types := groupTypes()
	refs := make([]groupTypeRef, 0, len(types))
	for _, id := range sortedGroupTypeIDs() {
		refs = append(refs, groupTypeRef{ID: id, Label: types[id]})
	}
	return groupDetail{
		ID: g.ID, Name: g.Name, Type: g.Type, Device: g.Device, Ref: g.Ref,
		// A box names the group device after the group and its address.
		DeviceName:            g.Name + " " + g.Device,
		ForbidSingleOperation: g.ForbidSingleOperation,
		Members:               s.members(g),
		Assignable:            assignable,
		Leftover:              leftover,
		Types:                 refs,
	}
}

// groupWritten is the answer to a create and to an update: the detail,
// and the group's members as the devices whose configuration is pending.
type groupWritten struct {
	groupDetail
	DevicesToConfigure []GroupMember `json:"devices_to_configure"`
}

// writtenOf renders the answer to a write. Callers hold s.mu.
func (s *groupStore) writtenOf(g *Group) groupWritten {
	d := s.detailOf(g)
	return groupWritten{groupDetail: d, DevicesToConfigure: append([]GroupMember{}, d.Members...)}
}

// take keeps the member ids the group's type can take, in the order
// given. A box drops any other id without an error: the write answers
// 200 and the group simply does not hold it. Callers hold s.mu.
func (s *groupStore) take(typeID string, ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := s.member(typeID, id); ok {
			out = append(out, id)
		}
	}
	return out
}

func (f *Fake) handleGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := groupID(r)
	s := f.system.groups
	s.mu.Lock()
	g, found := s.groups[id]
	var d groupDetail
	if ok && found {
		d = s.detailOf(g)
	}
	s.mu.Unlock()
	if !ok || !found {
		writeError(w, http.StatusNotFound, "unknown-group", "no such group")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

// invalidField is the 422 answer naming the offending field.
type invalidField struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Field   string `json:"field"`
}

func writeInvalid(w http.ResponseWriter, field, msg string) {
	writeJSON(w, http.StatusUnprocessableEntity, invalidField{Error: "invalid", Message: msg, Field: field})
}

// validGroupName allows 1-64 characters on one line.
func validGroupName(name string) bool {
	n := len([]rune(name))
	return n >= 1 && n <= 64 && !strings.ContainsAny(name, "\r\n")
}

type groupBody struct {
	Name                  *string   `json:"name"`
	Type                  string    `json:"type"`
	Members               *[]string `json:"members"`
	ForbidSingleOperation *bool     `json:"forbid_single_operation"`
}

func decodeGroupBody(w http.ResponseWriter, r *http.Request) (groupBody, bool) {
	var body groupBody
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeInvalid(w, "body", "invalid JSON body")
		return body, false
	}
	if body.Name != nil && !validGroupName(*body.Name) {
		writeInvalid(w, "name", "a name is 1-64 characters on one line")
		return body, false
	}
	return body, true
}

func (f *Fake) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeGroupBody(w, r)
	if !ok {
		return
	}
	if body.Name == nil {
		writeInvalid(w, "name", "a name is required")
		return
	}
	label, known := groupTypes()[body.Type]
	if !known {
		writeInvalid(w, "type", "unknown group type")
		return
	}
	s := f.system.groups
	s.mu.Lock()
	s.next++
	id := s.next
	device := fmt.Sprintf("INT%07d", s.next)
	g := &Group{
		ID: id, Name: *body.Name, Type: body.Type, TypeLabel: label,
		Device: device, Ref: "VirtualDevices." + device,
	}
	if body.Members != nil {
		g.Members = s.take(g.Type, *body.Members)
	}
	if body.ForbidSingleOperation != nil {
		g.ForbidSingleOperation = *body.ForbidSingleOperation
	}
	s.groups[id] = g
	created := s.writtenOf(g)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, created)
}

// handleGroupUpdate applies {name?, members?, forbid_single_operation?};
// members replace the whole list.
func (f *Fake) handleGroupUpdate(w http.ResponseWriter, r *http.Request) {
	body, ok := decodeGroupBody(w, r)
	if !ok {
		return
	}
	id, _ := groupID(r)
	s := f.system.groups
	s.mu.Lock()
	g, found := s.groups[id]
	if !found {
		s.mu.Unlock()
		writeError(w, http.StatusNotFound, "unknown-group", "no such group")
		return
	}
	if body.Name != nil {
		g.Name = *body.Name
	}
	if body.Members != nil {
		g.Members = s.take(g.Type, *body.Members)
	}
	if body.ForbidSingleOperation != nil {
		g.ForbidSingleOperation = *body.ForbidSingleOperation
	}
	d := s.writtenOf(g)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, d)
}

// groupDeleted is the DELETE answer; a box answers the deleted group's id
// as "deleted" and the members the group held.
type groupDeleted struct {
	Deleted       int           `json:"deleted"`
	FormerMembers []GroupMember `json:"former_members"`
}

func (f *Fake) handleGroupDelete(w http.ResponseWriter, r *http.Request) {
	id, _ := groupID(r)
	s := f.system.groups
	s.mu.Lock()
	g, found := s.groups[id]
	var former []GroupMember
	if found {
		former = s.members(g)
		delete(s.groups, g.ID)
	}
	s.mu.Unlock()
	if !found {
		writeError(w, http.StatusNotFound, "unknown-group", "no such group")
		return
	}
	writeJSON(w, http.StatusOK, groupDeleted{Deleted: g.ID, FormerMembers: former})
}

// Groups returns the groups the fake holds.
func (f *Fake) Groups() []Group { return f.system.groups.list() }
