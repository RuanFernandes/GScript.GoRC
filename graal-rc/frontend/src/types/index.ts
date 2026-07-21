// Domain types shared across the frontend. We re-export the generated Wails
// models so feature code depends on our own barrel, not on the wailsjs path.

import type {rclib, main, connection, credentials} from "../../wailsjs/go/models"

export type Server = rclib.Server
export type LoginRequest = main.LoginRequest
export type AccountSummary = credentials.AccountSummary
export type SessionStatus = connection.Status

// Finite set of top-level views the shell can render. Centralized so the
// router (App) is the only place that decides screen transitions.
export type AppView = "select" | "add" | "serverlist"
