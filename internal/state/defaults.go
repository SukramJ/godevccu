// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package state

// SetupDefaultPrograms seeds the manager with the same fixture set as
// pydevccu/state/defaults.setup_default_programs.
func SetupDefaultPrograms(m *Manager) {
	m.AddProgram("Heating Morning", "Start heating at 6:00", true, 0)
	m.AddProgram("Lights Off", "Turn off all lights at 23:00", true, 0)
	m.AddProgram("Vacation Mode", "Simulate presence when away", false, 0)
	m.AddProgram("Security Alert", "Triggered on intrusion detection", true, 0)
}

// SetupDefaultSysvars seeds the manager with the same fixture set as
// pydevccu's setup_default_sysvars.
func SetupDefaultSysvars(m *Manager) {
	m.AddSystemVariable("Presence", "BOOL", true, AddSystemVariableOpts{Description: "Someone is home"})
	m.AddSystemVariable("AlarmLevel", "ENUM", 0, AddSystemVariableOpts{
		Description: "Current alarm level",
		ValueList:   "Off;Armed;Triggered",
		MinValue:    0,
		MaxValue:    2,
	})
	m.AddSystemVariable("TargetTemperature", "FLOAT", 21.5, AddSystemVariableOpts{
		Description: "Target room temperature",
		Unit:        "°C",
		MinValue:    5.0,
		MaxValue:    30.0,
	})
	m.AddSystemVariable("LastMotion", "STRING", "", AddSystemVariableOpts{
		Description: "Last motion sensor triggered",
	})
	m.AddSystemVariable("EnergyToday", "FLOAT", 0.0, AddSystemVariableOpts{
		Description: "Energy consumed today",
		Unit:        "kWh",
		MinValue:    0.0,
		MaxValue:    1000.0,
	})
}

// SetupDefaultRooms seeds rooms.
func SetupDefaultRooms(m *Manager) {
	m.AddRoom("Living Room", "Main living area", nil, 0)
	m.AddRoom("Bedroom", "Master bedroom", nil, 0)
	m.AddRoom("Kitchen", "Kitchen area", nil, 0)
	m.AddRoom("Bathroom", "Main bathroom", nil, 0)
	m.AddRoom("Office", "Home office", nil, 0)
}

// SetupDefaultFunctions seeds functions (Gewerke).
func SetupDefaultFunctions(m *Manager) {
	m.AddFunction("Lights", "All lighting devices", nil, 0)
	m.AddFunction("Heating", "All heating devices", nil, 0)
	m.AddFunction("Security", "Security devices", nil, 0)
	m.AddFunction("Shutters", "Shutter and blind controls", nil, 0)
	m.AddFunction("Weather", "Weather sensors", nil, 0)
}

// SetupDefaults runs every setup helper above. Invoke after creating a
// Manager when you want a populated test environment.
func SetupDefaults(m *Manager) {
	SetupDefaultPrograms(m)
	SetupDefaultSysvars(m)
	SetupDefaultRooms(m)
	SetupDefaultFunctions(m)
}
