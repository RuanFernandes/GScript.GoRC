// rcService is the single adapter between the React app and the Wails v3 Go
// service. All backend calls go through here, so feature components never import
// the generated bindings directly (Dependency Inversion + single change point).
import {App} from "../../bindings/graal-rc"
import type {CommandMacro as BoundCommandMacro} from "../../bindings/graal-rc/models"
import {openOfficialPluginDocumentation} from "@/lib/pluginDocumentation"
import type {
  AccountSummary,
  Class,
  CommandMacro,
  CommandMacroParameter,
  CommandMacroStore,
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
  ScriptLists,
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
  PMState,
  CustomTheme,
  RemoteChangelog,
  UpdateInfo,
  MCPAgentStatus,
  MCPSetupResult,
  AuditEntry,
  AppTheme,
  AppThemeStore,
  DeploymentBackup,
  DeploymentBackupDiff,
  ChangeRetentionSettings,
  ChatSettings,
  ChatSettingsState,
} from "@/types"
import type {PluginBuildResult, PluginFile, PluginInfo, PluginLogEntry, PluginMonacoLanguage, PluginUIWindowInfo} from "@/plugins/types"

function toCommandMacro(macro: BoundCommandMacro): CommandMacro {
  return {
    id: macro.id,
    name: macro.name,
    command: macro.command,
    parameters: (macro.parameters ?? []).map((parameter) => ({name: parameter.name, type: parameter.type as CommandMacroParameter["type"]})),
    createdAt: macro.createdAt,
    updatedAt: macro.updatedAt,
  }
}

// The v3 bindings resolve to null on the "no result" path and reject on error;
// callers treat null as "empty/none" and rely on try/catch for real errors.
export interface RcService {
  listAccounts(): Promise<AccountSummary[] | null>
  loginWithAccount(accountName: string, nickname: string): Promise<Server[] | null>
  addAccount(req: LoginRequest, nickname: string): Promise<Server[] | null>
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
  getPMState(): Promise<PMState>
  markPMRead(playerID: number): Promise<void>
  recordOutgoingPM(playerID: number, account: string, nick: string, message: string): Promise<void>
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
  openPlayerListPM(playerID: number): Promise<void>
  // Script management (NC server).
  getWeapons(): Promise<Weapon[] | null>
  getClasses(): Promise<Class[] | null>
  getNPCs(): Promise<NPC[] | null>
  getScriptLists(onlyReadable: boolean): Promise<ScriptLists | null>
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
  graalScriptLspRequest(message: string): Promise<string>
  refreshGraalScriptDocApi(): Promise<void>
  listFonts(): Promise<string[] | null>
  getCodingSettings(): Promise<CodingSettings>
  setCodingSettings(theme: string, fontFamily: string, fontSize: number, tabSize: number): Promise<void>
  getChatSettings(): Promise<ChatSettingsState>
  setChatSettings(settings: ChatSettings): Promise<void>
  setExternalEditor(editor: string): Promise<void>
  getAppThemeStore(): Promise<AppThemeStore | null>
  saveAppTheme(theme: AppTheme): Promise<void>
  deleteAppTheme(key: string): Promise<void>
  setActiveAppTheme(key: string): Promise<void>
  getCommandMacros(serverName: string): Promise<CommandMacroStore>
  setCommandMacros(serverName: string, macros: CommandMacro[]): Promise<void>
  saveCommandMacro(serverName: string, name: string, command: string, parameters: CommandMacroParameter[]): Promise<CommandMacro>
  deleteCommandMacro(serverName: string, id: string): Promise<void>
  getCustomThemes(): Promise<CustomTheme[] | null>
  saveCustomTheme(theme: CustomTheme): Promise<void>
  deleteCustomTheme(key: string): Promise<void>
  getLanguage(): Promise<string>
  setLanguage(language: string): Promise<void>
  getAppVersion(): Promise<string>
  checkForUpdates(): Promise<UpdateInfo>
  installUpdate(): Promise<void>
  saveUpdate(): Promise<string>
  fetchRemoteChangelog(): Promise<RemoteChangelog>
  getRemoteTheme(): Promise<RemoteTheme | null>
  saveRemoteTheme(name: string, definition: string): Promise<void>
  getMCPAgentStatuses(): Promise<MCPAgentStatus[] | null>
  setupMCP(name: string): Promise<MCPSetupResult>
  openMCPAgentFile(name: string): Promise<void>
  openSettings(): Promise<void>
  openPluginManager(): Promise<void>
  openPluginDocumentation(): Promise<void>
  openPluginsFolder(): Promise<void>
  openChatLink(url: string): Promise<void>
  createPluginTemplate(name: string): Promise<PluginInfo | null>
  getPluginFiles(id: string): Promise<string[] | null>
  readPluginFile(id: string, path: string): Promise<PluginFile | null>
  writePluginFile(id: string, path: string, content: string): Promise<void>
  buildPlugin(id: string): Promise<PluginBuildResult | null>
  reloadPlugin(id: string): Promise<void>
  getPluginLogs(id: string): Promise<PluginLogEntry[] | null>
  clearPluginLogs(id: string): Promise<void>
  appendPluginLog(id: string, level: string, message: string): Promise<void>
  exportPlugin(id: string): Promise<string>
  getPlugins(): Promise<PluginInfo[] | null>
  refreshPlugins(): Promise<void>
  setPluginEnabled(id: string, enabled: boolean): Promise<void>
  recordPluginFailure(id: string): Promise<boolean>
  recordPluginSuccess(id: string): Promise<void>
  approvePluginPermissions(id: string, permissions: {events?: string[]; apis?: string[]; network?: string[]; plugins?: string[]; files?: {read?: string[]; write?: string[]}}): Promise<void>
  removePlugin(id: string): Promise<void>
  getPluginDirectory(): Promise<string>
  getPluginBundle(id: string): Promise<string>
  pluginStorageGet(id: string, key: string): Promise<string | null>
  pluginStorageSet(id: string, key: string, value: string): Promise<void>
  pluginStorageDelete(id: string, key: string): Promise<void>
  pluginSecretGet(id: string, key: string): Promise<string | null>
  pluginSecretSet(id: string, key: string, value: string): Promise<void>
  pluginSecretDelete(id: string, key: string): Promise<void>
  pluginCall(id: string, method: string, args: unknown[]): Promise<unknown>
  pluginRequest(id: string, request: {url: string; method?: string; headers?: Record<string, string>; body?: string}): Promise<{status: number; headers: Record<string, string>; body: string}>
  pluginMonacoRequest(kind: "diagnostics" | "completions", language: string, context: unknown): Promise<unknown>
  pluginMonacoResult(requestId: string, pluginId: string, value: unknown, errorMessage?: string): Promise<void>
  getPluginMonacoLanguages(): Promise<PluginMonacoLanguage[] | null>
  getPluginUIWindow(pluginId: string, windowId: string): Promise<PluginUIWindowInfo>
  pluginUIAction(pluginId: string, windowId: string, action: string, value: unknown): Promise<void>
  // File browser (main server socket).
  openFileBrowser(): Promise<void>
  fileBrowserStart(): Promise<void>
  fileBrowserCd(folder: string): Promise<void>
  fileBrowserDelete(path: string): Promise<void>
  fileBrowserRename(oldPath: string, newPath: string): Promise<void>
  fileBrowserMove(destFolder: string, filePath: string): Promise<void>
  getFileBrowserFolders(): Promise<FileBrowserFolder[] | null>
  getFileBrowserFiles(): Promise<FileBrowserEntry[] | null>
  getFileBrowserImageThumbnail(path: string): Promise<string>
  fileBrowserMaxUploadSize(): Promise<number>
  downloadFile(path: string, saveAs: boolean): Promise<string>
  uploadFileViaDialog(): Promise<void>
  uploadFileBytes(path: string, b64: string): Promise<void>
  getFileBrowserConfig(): Promise<FileBrowserConfig>
  setFileBrowserConfig(downloadDir: string): Promise<void>
  setFileBrowserImageThumbnails(enabled: boolean): Promise<void>
  // Type-aware file open (double-click).
  openRemoteFile(path: string): Promise<string>
  openRemoteFileAsText(path: string): Promise<void>
  openLocalScriptInFileBrowser(kind: string, name: string): Promise<void>
  openLocalScriptInExternalEditor(kind: string, name: string): Promise<void>
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
  resolveConflict(kind: string, key: string, choice: "local" | "server" | "merge", mergeContent?: string): Promise<void>
  pauseSync(): Promise<void>
  resumeSync(): Promise<void>
  normalizeSync(): Promise<void>
  rebuildSync(): Promise<void>
  openSyncReview(): Promise<void>
  getAuditEntries(limit: number): Promise<AuditEntry[] | null>
  clearAuditEntries(): Promise<void>
  getDeploymentBackups(limit: number): Promise<DeploymentBackup[] | null>
  getDeploymentBackupDiff(backupID: string): Promise<DeploymentBackupDiff>
  deleteDeploymentBackup(backupID: string): Promise<void>
  rollbackDeployment(backupID: string): Promise<void>
  getChangeRetention(): Promise<ChangeRetentionSettings>
  setChangeRetention(settings: ChangeRetentionSettings): Promise<void>
  openDeploymentCenter(): Promise<void>
}

// Default implementation backed by the generated Wails v3 bindings (App service).
export const rcService: RcService = {
  listAccounts: () => App.ListAccounts(),
  loginWithAccount: (accountName, nickname) => App.LoginWithAccount(accountName, nickname),
  addAccount: (req, nickname) => App.AddAccount(req, nickname),
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
  getPMState: async () => {
    const state = await App.GetPMState()
    return {
      conversations: (state?.conversations ?? []).map((conversation) => ({
        playerId: conversation.playerId,
        account: conversation.account,
        nick: conversation.nick,
        unread: conversation.unread,
        lines: (conversation.lines ?? []).map((line) => ({
          direction: line.direction as "in" | "out",
          text: line.text,
          timestamp: line.timestamp,
        })),
      })),
      unreadTotal: state?.unreadTotal ?? 0,
    }
  },
  markPMRead: (playerID) => App.MarkPMRead(playerID),
  recordOutgoingPM: (playerID, account, nick, message) => App.RecordOutgoingPM(playerID, account, nick, message),
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
  openPlayerListPM: (playerID) => App.OpenPlayerListPM(playerID),
  // Script management (NC server).
  getWeapons: () => App.GetWeapons(),
  getClasses: () => App.GetClasses(),
  getNPCs: () => App.GetNPCs(),
  getScriptLists: (onlyReadable) => App.GetScriptLists(onlyReadable),
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
  graalScriptLspRequest: (message) => App.GraalScriptLSPRequest(message),
  refreshGraalScriptDocApi: () => App.RefreshGraalScriptDocAPI(),
  listFonts: () => App.ListFonts(),
  getCodingSettings: () => App.GetCodingSettings(),
  setCodingSettings: (theme, fontFamily, fontSize, tabSize) => App.SetCodingSettings(theme, fontFamily, fontSize, tabSize),
  getChatSettings: () => App.GetChatSettings(),
  setChatSettings: (settings) => App.SetChatSettings(settings),
  setExternalEditor: (editor) => App.SetExternalEditor(editor),
  getAppThemeStore: () => App.GetAppThemeStore(),
  saveAppTheme: (theme) => App.SaveAppTheme(theme),
  deleteAppTheme: (key) => App.DeleteAppTheme(key),
  setActiveAppTheme: (key) => App.SetActiveAppTheme(key),
  getCommandMacros: async (serverName) => {
    const store = await App.GetCommandMacros(serverName)
    return {
      macros: (store?.macros ?? []).map(toCommandMacro),
      exists: Boolean(store?.exists),
    }
  },
  setCommandMacros: (serverName, macros) => App.SetCommandMacros(serverName, macros),
  saveCommandMacro: async (serverName, name, command, parameters) => {
    const macro = await App.SaveCommandMacro(serverName, name, command, parameters)
    return toCommandMacro(macro)
  },
  deleteCommandMacro: (serverName, id) => App.DeleteCommandMacro(serverName, id),
  getCustomThemes: () => App.GetCustomThemes(),
  saveCustomTheme: (theme) => App.SaveCustomTheme(theme),
  deleteCustomTheme: (key) => App.DeleteCustomTheme(key),
  getLanguage: () => App.GetLanguage(),
  setLanguage: (language) => App.SetLanguage(language),
  getAppVersion: () => App.GetAppVersion(),
  checkForUpdates: () => App.CheckForUpdates(),
  installUpdate: () => App.InstallUpdate(),
  saveUpdate: () => App.SaveUpdate(),
  fetchRemoteChangelog: () => App.FetchRemoteChangelog(),
  getRemoteTheme: () => App.GetRemoteTheme(),
  saveRemoteTheme: (name, definition) => App.SaveRemoteTheme(name, definition),
  getMCPAgentStatuses: () => App.GetMCPAgentStatuses(),
  setupMCP: (name) => App.SetupMCP(name),
  openMCPAgentFile: (name) => App.OpenMCPAgentFile(name),
  openSettings: () => App.OpenSettings(),
  openPluginManager: () => App.OpenPluginManager(),
  openPluginDocumentation: openOfficialPluginDocumentation,
  openPluginsFolder: () => App.OpenPluginsFolder(),
  openChatLink: (url) => App.OpenChatLink(url),
  createPluginTemplate: async (name) => {
    const info = await App.CreatePluginTemplate(name)
    if (!info) return null
    return {
      manifest: {
        id: info.manifest?.id ?? "",
        name: info.manifest?.name ?? "",
        version: info.manifest?.version ?? "",
        apiVersion: info.manifest?.apiVersion ?? 0,
        main: info.manifest?.main ?? "",
        description: info.manifest?.description ?? undefined,
        permissions: info.manifest?.permissions ? {
          events: info.manifest.permissions.events ?? undefined,
          apis: info.manifest.permissions.apis ?? undefined,
          network: info.manifest.permissions.network ?? undefined,
          plugins: info.manifest.permissions.plugins ?? undefined,
          files: info.manifest.permissions.files ? {read: info.manifest.permissions.files.read ?? undefined, write: info.manifest.permissions.files.write ?? undefined} : undefined,
        } : undefined,
      },
      directory: info.directory ?? "",
      enabled: info.enabled,
      status: info.status,
      error: info.error ?? undefined,
      approvedEvents: info.approvedEvents ?? undefined,
      approvedApis: info.approvedApis ?? undefined,
      approvedHosts: info.approvedHosts ?? undefined,
      approvedPlugins: info.approvedPlugins ?? undefined,
      approvedFileRead: info.approvedFileRead ?? undefined,
      approvedFileWrite: info.approvedFileWrite ?? undefined,
      failureCount: info.failureCount,
    }
  },
  getPluginFiles: (id) => App.GetPluginFiles(id),
  readPluginFile: async (id, path) => {
    const file = await App.ReadPluginFile(id, path)
    return file ? {path: file.path, content: file.content} : null
  },
  writePluginFile: (id, path, content) => App.WritePluginFile(id, path, content),
  buildPlugin: async (id) => {
    const result = await App.BuildPlugin(id)
    if (!result) return null
    return {
      plugin: result.plugin as unknown as PluginInfo,
      success: result.success,
      message: result.message,
    }
  },
  reloadPlugin: (id) => App.ReloadPlugin(id),
  getPluginLogs: async (id) => {
    const logs = await App.GetPluginLogs(id)
    return (logs ?? []).map((entry): PluginLogEntry => ({timestamp: entry.timestamp, level: entry.level, message: entry.message}))
  },
  clearPluginLogs: (id) => App.ClearPluginLogs(id),
  appendPluginLog: (id, level, message) => App.AppendPluginLog(id, level, message),
  exportPlugin: (id) => App.ExportPlugin(id),
  getPlugins: async () => {
    const list = await App.GetPlugins()
    return (list ?? []).map((item) => ({
      manifest: {
        id: item.manifest?.id ?? "",
        name: item.manifest?.name ?? "",
        version: item.manifest?.version ?? "",
        apiVersion: item.manifest?.apiVersion ?? 0,
        main: item.manifest?.main ?? "",
        description: item.manifest?.description ?? undefined,
        permissions: item.manifest?.permissions ? {
          events: item.manifest.permissions.events ?? undefined,
          apis: item.manifest.permissions.apis ?? undefined,
          network: item.manifest.permissions.network ?? undefined,
          plugins: item.manifest.permissions.plugins ?? undefined,
          files: item.manifest.permissions.files ? {read: item.manifest.permissions.files.read ?? undefined, write: item.manifest.permissions.files.write ?? undefined} : undefined,
        } : undefined,
      },
      directory: item.directory ?? "",
      enabled: item.enabled,
      status: item.status,
      error: item.error ?? undefined,
      approvedEvents: item.approvedEvents ?? undefined,
      approvedApis: item.approvedApis ?? undefined,
      approvedHosts: item.approvedHosts ?? undefined,
      approvedPlugins: item.approvedPlugins ?? undefined,
      approvedFileRead: item.approvedFileRead ?? undefined,
      approvedFileWrite: item.approvedFileWrite ?? undefined,
      failureCount: item.failureCount,
    }))
  },
  refreshPlugins: () => App.RefreshPlugins(),
  setPluginEnabled: (id, enabled) => App.SetPluginEnabled(id, enabled),
  recordPluginFailure: async (id) => {
    const result = await App.RecordPluginFailure(id)
    return result ?? false
  },
  recordPluginSuccess: (id) => App.RecordPluginSuccess(id),
  approvePluginPermissions: (id, permissions) => App.ApprovePluginPermissions(id, permissions),
  removePlugin: (id) => App.RemovePlugin(id),
  getPluginDirectory: () => App.GetPluginDirectory(),
  getPluginBundle: (id) => App.GetPluginBundle(id),
  pluginStorageGet: async (id, key) => {
    const result = await App.PluginStorageGet(id, key)
    return result?.[1] ? result[0] : null
  },
  pluginStorageSet: (id, key, value) => App.PluginStorageSet(id, key, value),
  pluginStorageDelete: (id, key) => App.PluginStorageDelete(id, key),
  pluginSecretGet: async (id, key) => {
    const result = await App.PluginSecretGet(id, key)
    return result?.[1] ? result[0] : null
  },
  pluginSecretSet: (id, key, value) => App.PluginSecretSet(id, key, value),
  pluginSecretDelete: (id, key) => App.PluginSecretDelete(id, key),
  pluginCall: (id, method, args) => App.PluginCall(id, method, args),
  pluginRequest: async (id, request) => {
    const response = await App.PluginRequest(id, request)
    const headers: Record<string, string> = {}
    for (const [key, value] of Object.entries(response.headers ?? {})) if (value !== undefined) headers[key] = value
    return {status: response.status, headers, body: response.body ?? ""}
  },
  pluginMonacoRequest: (kind, language, context) => App.PluginMonacoRequest(kind, language, context),
  pluginMonacoResult: (requestId, pluginId, value, errorMessage = "") => App.PluginMonacoResult(requestId, pluginId, value, errorMessage),
  getPluginMonacoLanguages: async () => {
    const languages = await App.GetPluginMonacoLanguages()
    return (languages ?? []).map(language => ({id: language.id ?? "", extensions: language.extensions ?? undefined, aliases: language.aliases ?? undefined}))
  },
  getPluginUIWindow: async (pluginId, windowId) => App.GetPluginUIWindow(pluginId, windowId) as Promise<PluginUIWindowInfo>,
  pluginUIAction: (pluginId, windowId, action, value) => App.PluginUIAction(pluginId, windowId, action, value),
  // File browser (main server socket).
  openFileBrowser: () => App.OpenFileBrowser(),
  fileBrowserStart: () => App.FileBrowserStart(),
  fileBrowserCd: (folder) => App.FileBrowserCd(folder),
  fileBrowserDelete: (path) => App.FileBrowserDelete(path),
  fileBrowserRename: (oldPath, newPath) => App.FileBrowserRename(oldPath, newPath),
  fileBrowserMove: (destFolder, filePath) => App.FileBrowserMove(destFolder, filePath),
  getFileBrowserFolders: () => App.GetFileBrowserFolders(),
  getFileBrowserFiles: () => App.GetFileBrowserFiles(),
  getFileBrowserImageThumbnail: (path) => App.GetFileBrowserImageThumbnail(path),
  fileBrowserMaxUploadSize: () => App.FileBrowserMaxUploadSize(),
  downloadFile: (path, saveAs) => App.DownloadFile(path, saveAs),
  uploadFileViaDialog: () => App.UploadFileViaDialog(),
  uploadFileBytes: (path, b64) => App.UploadFileBytes(path, b64),
  getFileBrowserConfig: () => App.GetFileBrowserConfig(),
  setFileBrowserConfig: (downloadDir) => App.SetFileBrowserConfig(downloadDir),
  setFileBrowserImageThumbnails: (enabled) => App.SetFileBrowserImageThumbnails(enabled),
  // Type-aware file open (double-click).
  openRemoteFile: (path) => App.OpenRemoteFile(path),
  openRemoteFileAsText: (path) => App.OpenRemoteFileAsText(path),
  openLocalScriptInFileBrowser: (kind, name) => App.OpenLocalScriptInFileBrowser(kind, name),
  openLocalScriptInExternalEditor: (kind, name) => App.OpenLocalScriptInExternalEditor(kind, name),
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
  resolveConflict: (kind, key, choice, mergeContent = "") => App.ResolveConflict(kind, key, choice, mergeContent),
  pauseSync: () => App.PauseSync(),
  resumeSync: () => App.ResumeSync(),
  normalizeSync: () => App.NormalizeSync(),
  rebuildSync: () => App.RebuildSync(),
  openSyncReview: () => App.OpenSyncReview(),
  getAuditEntries: (limit) => App.GetAuditEntries(limit),
  clearAuditEntries: () => App.ClearAuditEntries(),
  getDeploymentBackups: (limit) => App.GetDeploymentBackups(limit),
  getDeploymentBackupDiff: (backupID) => App.GetDeploymentBackupDiff(backupID),
  deleteDeploymentBackup: (backupID) => App.DeleteDeploymentBackup(backupID),
  rollbackDeployment: (backupID) => App.RollbackDeployment(backupID),
  getChangeRetention: () => App.GetChangeRetention(),
  setChangeRetention: (settings) => App.SetChangeRetention(settings),
  openDeploymentCenter: () => App.OpenDeploymentCenter(),
}
