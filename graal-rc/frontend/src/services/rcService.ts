// rcService is the single adapter between the React app and the Wails-bound Go
// backend. All backend calls go through here, so feature components never import
// the generated bindings directly (Dependency Inversion + single change point).
import {
  AddAccount,
  ConnectToServer,
  GetServers,
  ListAccounts,
  LoginWithAccount,
  Logout,
  RemoveAccount,
  SetNewProtocol,
  Status,
} from "../../wailsjs/go/main/App"
import type {AccountSummary, LoginRequest, Server, SessionStatus} from "@/types"

export interface RcService {
  listAccounts(): Promise<AccountSummary[]>
  loginWithAccount(accountName: string): Promise<Server[]>
  addAccount(req: LoginRequest): Promise<Server[]>
  removeAccount(accountName: string): Promise<void>
  getServers(): Promise<Server[]>
  connectToServer(index: number): Promise<void>
  setNewProtocol(enable: boolean): Promise<void>
  logout(): Promise<void>
  status(): Promise<SessionStatus>
}

// Default implementation backed by the generated Wails bindings.
export const rcService: RcService = {
  listAccounts: () => ListAccounts(),
  loginWithAccount: (accountName) => LoginWithAccount(accountName),
  addAccount: (req) => AddAccount(req),
  removeAccount: (accountName) => RemoveAccount(accountName),
  getServers: () => GetServers(),
  connectToServer: (index) => ConnectToServer(index),
  setNewProtocol: (enable) => SetNewProtocol(enable),
  logout: () => Logout(),
  status: () => Status(),
}
