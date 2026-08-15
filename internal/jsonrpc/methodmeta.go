// SPDX-License-Identifier: MIT
// Copyright (C) 2026 godevccu authors.

package jsonrpc

// methodMeta is the privilege level and description a CCU reports for a
// JSON-RPC method. system.listMethods returns these alongside the name;
// answering with the bare name left clients without the level
// information the real API publishes.
//
// Levels and texts are taken verbatim from the CCU's own methods.conf
// (LEVEL / INFO). Four methods carry a godevccu-specific note because
// the real CCU does not define them at all.
type methodMeta struct {
	level string
	info  string
}

// methodLevels covers every method in [Handlers.Methods]. Unknown
// methods fall back to defaultMethodLevel.
var methodLevels = map[string]methodMeta{
	"CCU.getAuthEnabled":                     {level: "ADMIN", info: "Prüft, ob die Datei /etc/config/authEnabled vorhanden ist"},
	"CCU.getHttpsRedirectEnabled":            {level: "ADMIN", info: "Prüft, ob die Datei automatische Weiterleitung von HTTP auf HTTPS aktiv ist."},
	"CCU.getSerial":                          {level: "ADMIN", info: "Liefert die Seriennummer der HomeMatic Zentrale"},
	"CCU.getVersion":                         {level: "ADMIN", info: "Liefert die Firmware-Version der HomeMatic Zentrale"},
	"Channel.hasProgramIds":                  {level: "GUEST", info: "Prüft, ob der Kanal in Programmen verwendet wird"},
	"Channel.setName":                        {level: "ADMIN", info: "Legt den Namen des Kanals fest"},
	"Device.get":                             {level: "GUEST", info: "Liefert Detailinformationen zu einem Gerät"},
	"Device.listAllDetail":                   {level: "GUEST", info: "Liefert die Detailinformationen aller fertig konfigurierten Geräte"},
	"Device.setName":                         {level: "ADMIN", info: "Legt den Namen des Geräts fest"},
	"Interface.determineParameter":           {level: "ADMIN", info: "Bestimmt den Wert eines Patameters"},
	"Interface.getDeviceDescription":         {level: "GUEST", info: "Liefert die Beschreibung eines Geräts"},
	"Interface.getInstallMode":               {level: "ADMIN", info: "Liefert die Restzeit, für die der Anlernmodus noch aktiv ist"},
	"Interface.getLinkInfo":                  {level: "GUEST", info: "Liefert den Namen und die Beschreibung einer direkten Verknüpfung"},
	"Interface.getMasterValue":               {level: "GUEST", info: "Liefert den Wert eines Parameters aus dem Parameterset \"MASTER\""},
	"Interface.getParamset":                  {level: "GUEST", info: "Liefert ein komplettes Parameterset"},
	"Interface.getParamsetDescription":       {level: "GUEST", info: "Liefert die Beschreibung eines Parametersets"},
	"Interface.getParamsetId":                {level: "GUEST", info: "Liefert die Id eines Parametersets"},
	"Interface.getSuppressedServiceMessages": {level: "USER", info: "Liefert ein Array mit Kanalparametern, deren Service Nachrichten unterdrückt werden."},
	"Interface.suppressServiceMessages":      {level: "ADMIN", info: "Unterdrückt Service Nachrichten für einen Kanal oder Kanalparameter. Wird für parameterId ein leerer String übergeben, gilt der Aufruf für alle Service Parameter."},
	"system.describe":                        {level: "NONE", info: "Liefert eine detailierte Beschreibung der HomeMatic JSON API."},
	"system.methodHelp":                      {level: "NONE", info: "Liefert die Kurzbeschreibung einer Methode"},
	"Interface.getValue":                     {level: "GUEST", info: "Liefert den Wert eines Parameters aus dem Parameterset \"Values\""},
	"Interface.init":                         {level: "ADMIN", info: "Meldet eine Logikschicht bei einer Schnittstelle an"},
	"Interface.isPresent":                    {level: "NONE", info: "Prüft, ob der Dienst der betreffenden Schnittstelle läuft)"},
	"Interface.listBidcosInterfaces":         {level: "USER", info: "Listet die verfügbaren BidCoS-RF Interfaces auf"},
	"Interface.listDevices":                  {level: "GUEST", info: "Liefert eine Liste aller angelernten Geräte"},
	"Interface.listInterfaces":               {level: "GUEST", info: "Liefert eine Liste der verfügbaren Schnittstellen"},
	"Interface.ping":                         {level: "GUEST", info: "Prüft die Erreichbarkeit der Schnittstelle (godevccu-Erweiterung)"},
	"Interface.putParamset":                  {level: "USER", info: "Schreibt ein komplettes Parameterset für ein Gerät"},
	"Interface.rssiInfo":                     {level: "GUEST", info: "Liefert die Empfangsfeldstärken der angeschlossenen Geräte"},
	"Interface.setInstallMode":               {level: "ADMIN", info: "Aktiviert oder deaktiviert den Anlernmodus (godevccu-Erweiterung)"},
	"Interface.setInstallModeHMIP":           {level: "ADMIN", info: "Aktiviert oder dekativiert den Anlernmodus"},
	"Interface.setLinkInfo":                  {level: "ADMIN", info: "Legt den Namen und die Beschreibung einer direkten Verknüpfung fest"},
	"Interface.setValue":                     {level: "GUEST", info: "Setzt einen einzelnen Wert im Parameterset \"Values\""},
	"Program.execute":                        {level: "USER", info: "Führt ein Programm auf der HomeMatic Zentrale aus"},
	"Program.getAll":                         {level: "USER", info: "Liefert Detailinformationen zu allen Programmen auf der HomeMatic Zentrale"},
	"Program.setActive":                      {level: "USER", info: "Aktiviert oder deaktiviert ein Programm (godevccu-Erweiterung)"},
	"ReGa.runScript":                         {level: "ADMIN", info: "Führt ein HomeMatic Script aus"},
	"Room.getAll":                            {level: "GUEST", info: "Liefert Detailinformationen zu allen Räumen"},
	"Room.listAll":                           {level: "GUEST", info: "Liefert eine Liste aller Räume"},
	"Session.login":                          {level: "NONE", info: "Führt die Benutzeranmeldung durch"},
	"Session.logout":                         {level: "NONE", info: "Beendet eine Sitzung"},
	"Session.renew":                          {level: "GUEST", info: "Erneuert die Sitzung; Falls eine Sitzung nicht rechtzeitig erneuert wird, läuft diese ab"},
	"Subsection.getAll":                      {level: "GUEST", info: "Liefert Detailinformationen zu allen Gewerken"},
	"SysVar.createBool":                      {level: "USER", info: "Erzeugt eine Systemvariable vom Typ bool"},
	"SysVar.createEnum":                      {level: "USER", info: "Erzeugt eine Systemvariable vom Typ enum"},
	"SysVar.createFloat":                     {level: "USER", info: "Erzeugt eine Systemvariable vom Typ Number"},
	"SysVar.deleteSysVarByName":              {level: "USER", info: "Entfernt eine Systemvariable mit bestimmten Namen"},
	"SysVar.get":                             {level: "USER", info: "Liefert Detailinformationen zu einer Systemvariable auf der HomeMatic Zentrale"},
	"SysVar.getAll":                          {level: "USER", info: "Liefert Detailinformationen zu allen Systemvariablen auf der HomeMatic Zentrale"},
	"SysVar.getValueByName":                  {level: "USER", info: "Liefert den aktuellen Wert einer Systemvariable"},
	"SysVar.setBool":                         {level: "USER", info: "Setzt den Wert einer Systemvariable vom Type bool"},
	"SysVar.setEnum":                         {level: "USER", info: "Setzt den Wert einer Systemvariable vom Typ enum"},
	"SysVar.setFloat":                        {level: "USER", info: "Setzt den Wert einer Systemvariable vom Type float"},
	"SysVar.setString":                       {level: "USER", info: "Setzt den Wert einer Systemvariable vom Typ Zeichenkette (godevccu-Erweiterung)"},
	"system.listMethods":                     {level: "NONE", info: "Liefert eine Liste der verfügbaren Methoden"}}

// defaultMethodLevel is what an unlisted method reports; USER is the
// CCU's most common level.
const defaultMethodLevel = "USER"

// metaFor returns the level and description for a method name.
func metaFor(name string) methodMeta {
	if m, ok := methodLevels[name]; ok {
		return m
	}
	return methodMeta{level: defaultMethodLevel}
}
