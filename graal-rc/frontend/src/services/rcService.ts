// rcService is the single adapter between the React app and the Wails v3 Go
// service. All backend calls go through here, so feature components never import
// the generated bindings directly (Dependency Inversion + single change point).
import {App} from "../../bindings/graal-rc"
import type {AccountSummary, LoginRequest, NCStatus, Player, Server} from "@/types"

// The v3 bindings resolve to null on the "no result" path and reject on error;
// callers treat null as "empty/none" and rely on try/catch for real errors.
export interface RcService {
  listAccounts(): Promise<AccountSummary[] | null>
  loginWithAccount(accountName: string): Promise<Server[] | null>
  addAccount(req: LoginRequest): Promise<Server[] | null>
  removeAccount(accountName: string): Promise<void>
  getServers(): Promise<Server[] | null>
  connectToServer(index: number): Promise<void>
  setNewProtocol(enable: boolean): Promise<void>
  logout(): Promise<void>
  status(): Promise<unknown>
  connectToNCServer(): Promise<void>
  disconnectNC(): Promise<void>
  ncStatus(): Promise<NCStatus | null>
  ircLogin(): Promise<void>
  sendIrcText(command: string, p1: string, p2: string, p3: string): Promise<void>
  execute(message: string): Promise<void>
  getPlayers(): Promise<Player[] | null>
  setChatLogConfig(enabled: boolean, dir: string): Promise<void>
  appendChatLog(line: string): Promise<void>
  chooseDirectory(): Promise<string>
  openPlayerList(): Promise<void>
}

// Default implementation backed by the generated Wails v3 bindings (App service).
export const rcService: RcService = {
  listAccounts: () => App.ListAccounts(),
  loginWithAccount: (accountName) => App.LoginWithAccount(accountName),
  addAccount: (req) => App.AddAccount(req),
  removeAccount: (accountName) => App.RemoveAccount(accountName),
  getServers: () => App.GetServers(),
  connectToServer: (index) => App.ConnectToServer(index),
  setNewProtocol: (enable) => App.SetNewProtocol(enable),
  logout: () => App.Logout(),
  status: () => App.Status(),
  connectToNCServer: () => App.ConnectToNCServer(),
  disconnectNC: () => App.DisconnectNC(),
  ncStatus: () => App.NCStatus(),
  ircLogin: () => App.IrcLogin(),
  sendIrcText: (command, p1, p2, p3) => App.SendIrcText(command, p1, p2, p3),
  execute: (message) => App.Execute(message),
  getPlayers: () => App.GetPlayers(),
  setChatLogConfig: (enabled, dir) => App.SetChatLogConfig(enabled, dir),
  appendChatLog: (line) => App.AppendChatLog(line),
  chooseDirectory: () => App.ChooseDirectory(),
  openPlayerList: () => App.OpenPlayerList(),
}
