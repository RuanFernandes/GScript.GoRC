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
  SqliteInfo,
  SqliteResult,
  SqliteSchema,
  SqliteChanges,
  Weapon,
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
  setChatLogConfig(enabled: boolean, dir: string): Promise<void>
  appendChatLog(line: string): Promise<void>
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
  setChatLogConfig: (enabled, dir) => App.SetChatLogConfig(enabled, dir),
  appendChatLog: (line) => App.AppendChatLog(line),
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
}
