// rcService is the single adapter between the React app and the Wails v3 Go
// service. All backend calls go through here, so feature components never import
// the generated bindings directly (Dependency Inversion + single change point).
import {App} from "../../bindings/graal-rc"
import type {
  AccountSummary,
  Class,
  CodingSettings,
  FileBrowserConfig,
  FileBrowserEntry,
  FileBrowserFolder,
  LoginRequest,
  NCStatus,
  NPC,
  Player,
  RemoteTheme,
  ScriptReply,
  Server,
  RightsData,
  AttrsData,
  BanData,
  CommentsData,
  SqliteInfo,
  SqliteResult,
  SqliteSchema,
  SqliteChanges,
  Weapon,
  SyncConfig,
  SyncStatus,
  SyncScriptPair,
} from "@/types"

// The v3 bindings resolve to null on the "no result" path and reject on error;
// callers treat null as "empty/none" and rely on try/catch for real errors.
export interface RcService {
  listAccounts(): Promise<AccountSummary[] | null>
  loginWithAccount(accountName: string): Promise<Server[] | null>
  addAccount(req: LoginRequest): Promise<Server[] | null>
  removeAccount(accountName: string): Promise<void>
  renameAccount(accountName: string, displayName: string): Promise<void>
  setAccountPhoto(accountName: string, dataURL: string): Promise<void>
  getAccount(accountName: string): Promise<AccountSummary | null>
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
  // Private + admin messaging (main server socket).
  sendPrivateMessage(playerID: number, message: string): Promise<void>
  sendMassPM(playerIDs: number[], message: string): Promise<void>
  sendAdminMessage(playerID: number, message: string): Promise<void>
  sendAdminMessageAll(message: string): Promise<void>
  // Player admin editors (rights / attributes / ban) — main server socket.
  openRights(account: string): Promise<RightsData | null>
  setRights(account: string, rights: number, ipRange: string, folderAccess: string): Promise<void>
  openAttrs(account: string): Promise<AttrsData | null>
  setAttrs(account: string, propertiesJson: string): Promise<void>
  parseAttrsText(text: string): Promise<string>
  openBan(account: string): Promise<BanData | null>
  openComments(account: string): Promise<CommentsData | null>
  setComments(account: string, comments: string): Promise<void>
  openRightsWindow(account: string): Promise<void>
  openAttrsWindow(account: string): Promise<void>
  openBanWindow(account: string): Promise<void>
  openCommentsWindow(account: string): Promise<void>
  openBanHistoryWindow(account: string): Promise<void>
  openStaffActivityWindow(account: string): Promise<void>
  setBan(target: string, world: string, banned: boolean, banType: string, releaseTime: string, reason: string): Promise<void>
  getBanTypes(): Promise<string>
  requestBanHistory(account: string): Promise<string>
  requestStaffActivity(account: string): Promise<string>
  setChatLogConfig(enabled: boolean, dir: string): Promise<void>
  appendChatLog(line: string): Promise<void>
  setPmLogConfig(enabled: boolean, dir: string): Promise<void>
  appendPmLog(otherAccount: string, line: string): Promise<void>
  chooseDirectory(): Promise<string>
  openPlayerList(): Promise<void>
  // Script management (NC server).
  getWeapons(): Promise<Weapon[] | null>
  getClasses(): Promise<Class[] | null>
  getNPCs(): Promise<NPC[] | null>
  addWeapon(name: string): Promise<void>
  deleteWeapon(name: string): Promise<void>
  addClass(name: string): Promise<void>
  deleteClass(name: string): Promise<void>
  deleteNPC(id: number): Promise<void>
  createNPC(name: string, id: number, type: string, scripter: string, level: string, x: string, y: string): Promise<void>
  openScript(scriptType: string, key: string): Promise<ScriptReply | null>
  saveWeapon(name: string, script: string): Promise<void>
  saveClass(name: string, script: string): Promise<void>
  saveNPC(id: number, script: string): Promise<void>
  resetNPC(id: number): Promise<void>
  openNPCFlags(id: number): Promise<ScriptReply | null>
  openNPCAttributes(id: number): Promise<ScriptReply | null>
  saveNPCFlags(id: number, flags: string): Promise<void>
  // Server-side text configs (options/folder_config/flags) — main socket, not NC.
  uploadServerText(kind: string, content: string): Promise<void>
  warpNPC(id: number, x: number, y: number, level: string): Promise<void>
  refreshWeapons(): Promise<void>
  openScriptManager(): Promise<void>
  openScriptEditor(scriptType: string, key: string): Promise<void>
  getLoadedScript(scriptType: string, key: string): Promise<ScriptReply | null>
  setEditorDirty(scriptType: string, key: string, dirty: boolean): Promise<void>
  closeScriptEditor(scriptType: string, key: string): Promise<void>
  listFonts(): Promise<string[] | null>
  getCodingSettings(): Promise<CodingSettings>
  setCodingSettings(theme: string, fontFamily: string, fontSize: number): Promise<void>
  getRemoteTheme(): Promise<RemoteTheme | null>
  saveRemoteTheme(name: string, definition: string): Promise<void>
  openSettings(): Promise<void>
  // File browser (main server socket).
  openFileBrowser(): Promise<void>
  fileBrowserStart(): Promise<void>
  fileBrowserCd(folder: string): Promise<void>
  fileBrowserDelete(path: string): Promise<void>
  fileBrowserRename(oldPath: string, newPath: string): Promise<void>
  fileBrowserMove(destFolder: string, filePath: string): Promise<void>
  getFileBrowserFolders(): Promise<FileBrowserFolder[] | null>
  getFileBrowserFiles(): Promise<FileBrowserEntry[] | null>
  fileBrowserMaxUploadSize(): Promise<number>
  downloadFile(path: string, saveAs: boolean): Promise<string>
  uploadFileViaDialog(): Promise<void>
  uploadFileBytes(path: string, b64: string): Promise<void>
  getFileBrowserConfig(): Promise<FileBrowserConfig>
  setFileBrowserConfig(downloadDir: string): Promise<void>
  // Type-aware file open (double-click).
  openRemoteFile(path: string): Promise<string>
  openRemoteFileAsText(path: string): Promise<void>
  getTextFile(path: string): Promise<string>
  saveTextFile(path: string, content: string): Promise<void>
  // SQLite explorer.
  getSqliteInfo(path: string): Promise<SqliteInfo>
  getSqliteSchema(path: string): Promise<SqliteSchema[] | null>
  sqliteQuery(path: string, sql: string, args: unknown[]): Promise<SqliteResult>
  commitSqlite(path: string, changes: SqliteChanges): Promise<void>
  saveSqliteFile(path: string): Promise<void>
  // Local Sync.
  getSyncConfig(): Promise<SyncConfig>
  setSyncConfig(
    enabled: boolean,
    outputDir: string,
    pollingMinutes: number,
    autoPush: boolean,
    autoPull: boolean,
  ): Promise<void>
  syncNow(): Promise<void>
  getSyncStatus(): Promise<SyncStatus>
  getSyncScriptPair(kind: string, key: string): Promise<SyncScriptPair>
  resolveConflict(kind: string, key: string, choice: "local" | "server"): Promise<void>
  pauseSync(): Promise<void>
  resumeSync(): Promise<void>
  openSyncReview(): Promise<void>
}

// Default implementation backed by the generated Wails v3 bindings (App service).
export const rcService: RcService = {
  listAccounts: () => App.ListAccounts(),
  loginWithAccount: (accountName) => App.LoginWithAccount(accountName),
  addAccount: (req) => App.AddAccount(req),
  removeAccount: (accountName) => App.RemoveAccount(accountName),
  renameAccount: (accountName, displayName) => App.RenameAccount(accountName, displayName),
  setAccountPhoto: (accountName, dataURL) => App.SetAccountPhoto(accountName, dataURL),
  getAccount: (accountName) => App.GetAccount(accountName),
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
  // Private + admin messaging (main server socket).
  sendPrivateMessage: (playerID, message) => App.SendPrivateMessage(playerID, message),
  sendMassPM: (playerIDs, message) => App.SendMassPM(playerIDs, message),
  sendAdminMessage: (playerID, message) => App.SendAdminMessage(playerID, message),
  sendAdminMessageAll: (message) => App.SendAdminMessageAll(message),
  // Player admin editors (rights / attributes / ban) — main server socket.
  openRights: (account) => App.OpenRights(account),
  setRights: (account, rights, ipRange, folderAccess) => App.SetRights(account, rights, ipRange, folderAccess),
  openAttrs: (account) => App.OpenAttrs(account),
  setAttrs: (account, propertiesJson) => App.SetAttrs(account, propertiesJson),
  parseAttrsText: (text) => App.ParseAttrsText(text),
  openBan: (account) => App.OpenBan(account),
  openComments: (account) => App.OpenComments(account),
  setComments: (account, comments) => App.SetComments(account, comments),
  openRightsWindow: (account) => App.OpenRightsWindow(account),
  openAttrsWindow: (account) => App.OpenAttrsWindow(account),
  openBanWindow: (account) => App.OpenBanWindow(account),
  openCommentsWindow: (account) => App.OpenCommentsWindow(account),
  openBanHistoryWindow: (account) => App.OpenBanHistoryWindow(account),
  openStaffActivityWindow: (account) => App.OpenStaffActivityWindow(account),
  setBan: (target, world, banned, banType, releaseTime, reason) =>
    App.SetBan(target, world, banned, banType, releaseTime, reason),
  getBanTypes: () => App.GetBanTypes(),
  requestBanHistory: (account) => App.RequestBanHistory(account),
  requestStaffActivity: (account) => App.RequestStaffActivity(account),
  setChatLogConfig: (enabled, dir) => App.SetChatLogConfig(enabled, dir),
  appendChatLog: (line) => App.AppendChatLog(line),
  setPmLogConfig: (enabled, dir) => App.SetPmLogConfig(enabled, dir),
  appendPmLog: (otherAccount, line) => App.AppendPmLog(otherAccount, line),
  chooseDirectory: () => App.ChooseDirectory(),
  openPlayerList: () => App.OpenPlayerList(),
  // Script management (NC server).
  getWeapons: () => App.GetWeapons(),
  getClasses: () => App.GetClasses(),
  getNPCs: () => App.GetNPCs(),
  addWeapon: (name) => App.AddWeapon(name),
  deleteWeapon: (name) => App.DeleteWeapon(name),
  addClass: (name) => App.AddClass(name),
  deleteClass: (name) => App.DeleteClass(name),
  deleteNPC: (id) => App.DeleteNPC(id),
  createNPC: (name, id, type, scripter, level, x, y) =>
    App.CreateNPC(name, id, type, scripter, level, x, y),
  openScript: (scriptType, key) => App.OpenScript(scriptType, key),
  saveWeapon: (name, script) => App.SaveWeapon(name, script),
  saveClass: (name, script) => App.SaveClass(name, script),
  saveNPC: (id, script) => App.SaveNPC(id, script),
  resetNPC: (id) => App.ResetNPC(id),
  openNPCFlags: (id) => App.OpenNPCFlags(id),
  openNPCAttributes: (id) => App.OpenNPCAttributes(id),
  saveNPCFlags: (id, flags) => App.SaveNPCFlags(id, flags),
  uploadServerText: (kind, content) => App.SaveServerText(kind, content),
  warpNPC: (id, x, y, level) => App.WarpNPC(id, x, y, level),
  refreshWeapons: () => App.RefreshWeapons(),
  openScriptManager: () => App.OpenScriptManager(),
  openScriptEditor: (scriptType, key) => App.OpenScriptEditor(scriptType, key),
  getLoadedScript: (scriptType, key) => App.GetLoadedScript(scriptType, key),
  setEditorDirty: (scriptType, key, dirty) => App.SetEditorDirty(scriptType, key, dirty),
  closeScriptEditor: (scriptType, key) => App.CloseScriptEditor(scriptType, key),
  listFonts: () => App.ListFonts(),
  getCodingSettings: () => App.GetCodingSettings(),
  setCodingSettings: (theme, fontFamily, fontSize) => App.SetCodingSettings(theme, fontFamily, fontSize),
  getRemoteTheme: () => App.GetRemoteTheme(),
  saveRemoteTheme: (name, definition) => App.SaveRemoteTheme(name, definition),
  openSettings: () => App.OpenSettings(),
  // File browser (main server socket).
  openFileBrowser: () => App.OpenFileBrowser(),
  fileBrowserStart: () => App.FileBrowserStart(),
  fileBrowserCd: (folder) => App.FileBrowserCd(folder),
  fileBrowserDelete: (path) => App.FileBrowserDelete(path),
  fileBrowserRename: (oldPath, newPath) => App.FileBrowserRename(oldPath, newPath),
  fileBrowserMove: (destFolder, filePath) => App.FileBrowserMove(destFolder, filePath),
  getFileBrowserFolders: () => App.GetFileBrowserFolders(),
  getFileBrowserFiles: () => App.GetFileBrowserFiles(),
  fileBrowserMaxUploadSize: () => App.FileBrowserMaxUploadSize(),
  downloadFile: (path, saveAs) => App.DownloadFile(path, saveAs),
  uploadFileViaDialog: () => App.UploadFileViaDialog(),
  uploadFileBytes: (path, b64) => App.UploadFileBytes(path, b64),
  getFileBrowserConfig: () => App.GetFileBrowserConfig(),
  setFileBrowserConfig: (downloadDir) => App.SetFileBrowserConfig(downloadDir),
  // Type-aware file open (double-click).
  openRemoteFile: (path) => App.OpenRemoteFile(path),
  openRemoteFileAsText: (path) => App.OpenRemoteFileAsText(path),
  getTextFile: (path) => App.GetTextFile(path),
  saveTextFile: (path, content) => App.SaveTextFile(path, content),
  // SQLite explorer.
  getSqliteInfo: (path) => App.GetSqliteInfo(path),
  getSqliteSchema: (path) => App.GetSqliteSchema(path),
  sqliteQuery: (path, sql, args) => App.SqliteQuery(path, sql, args),
  commitSqlite: (path, changes) => App.CommitSqlite(path, changes),
  saveSqliteFile: (path) => App.SaveSqliteFile(path),
  // Local Sync.
  getSyncConfig: () => App.GetSyncConfig(),
  setSyncConfig: (enabled, outputDir, pollingMinutes, autoPush, autoPull) =>
    App.SetSyncConfig(enabled, outputDir, pollingMinutes, autoPush, autoPull),
  syncNow: () => App.SyncNow(),
  getSyncStatus: () => App.GetSyncStatus(),
  getSyncScriptPair: (kind, key) => App.GetSyncScriptPair(kind, key),
  resolveConflict: (kind, key, choice) => App.ResolveConflict(kind, key, choice),
  pauseSync: () => App.PauseSync(),
  resumeSync: () => App.ResumeSync(),
  openSyncReview: () => App.OpenSyncReview(),
}
