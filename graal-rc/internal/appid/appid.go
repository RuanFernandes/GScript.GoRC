// Package appid holds the identity of this app. It installs and runs beside the
// official Graal RC, so nothing it names on the machine may collide with it.
package appid

// OfficialMCPAddress is where the official app serves MCP.
const OfficialMCPAddress = "127.0.0.1:8765"

// SelfUpdates is off: the release service only publishes the official app,
// which has no Alpaxis support.
const SelfUpdates = false

// ID names the data folder and the identifiers registered with the OS.
func ID() string { return "alpaxis-rc" }

// DisplayName is the product name shown to the user.
func DisplayName() string { return "Alpaxis Remote Control" }

// MCPAddress is the default address of the local MCP server.
func MCPAddress() string { return "127.0.0.1:8767" }
