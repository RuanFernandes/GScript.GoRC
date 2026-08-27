import {Events} from "@wailsio/runtime"
import {rcService} from "@/services/rcService"
import type {PluginCommand, PluginEvent, PluginFileEditor, PluginHttpRequestEvent, PluginNotification, PluginPanel, PluginUITabInfo, PluginUITabOptions, PluginUIWindowOptions, PluginUIView, PluginUIPrimitive} from "./types"
import type {PluginInfo} from "./types"

type RuntimeMessage =
  | {type: "rpc"; requestId: string; method: string; args: unknown[]}
  | {type: "subscribe"; name: string}
  | {type: "pluginSubscribe"; channel: string}
  | {type: "pluginUnsubscribe"; channel: string}
  | {type: "command"; command: PluginCommand}
  | {type: "commandRemoved"; id: string}
  | {type: "panel"; panel: PluginPanel}
  | {type: "tab"; tab: PluginUITabOptions}
  | {type: "tabUpdate"; tabId: string; view: PluginUIView}
  | {type: "tabRemoved"; tabId: string}
  | {type: "tabOpen"; tabId: string}
  | {type: "notification"; notification: PluginNotification}
  | {type: "fileEditor"; editor: PluginFileEditor}
  | {type: "fileEditorRemoved"; id: string}
  | {type: "uiActionSubscribe"}
  | {type: "uiWindowOpen"; options: PluginUIWindowOptions}
  | {type: "uiWindowUpdate"; windowId: string; view: PluginUIView}
  | {type: "uiWindowClose"; windowId: string}
  | {type: "pluginExpose"; method: string}
  | {type: "pluginExposeRemoved"; method: string}
  | {type: "fileOpenResult"; requestId: string; handled: boolean}
  | {type: "pluginResponse"; requestId: string; ok: boolean; value?: unknown; error?: string}
  | {type: "monacoProvider"; kind: "diagnostics" | "completions"; language: string}
  | {type: "monacoProviderRemoved"; kind: "diagnostics" | "completions"; language: string}
  | {type: "monacoResponse"; requestId: string; ok: boolean; value?: unknown; error?: string}
  | {type: "log"; level: string; message: string}
  | {type: "error"; message: string}
  | {type: "ready"}
  | {type: "httpResponse"; requestId: string; status: number; headers?: Record<string, string>; body?: string}
  | {type: "lifecycleResult"; requestId: string; ok: boolean; error?: string}

type PluginSocketEvent = {
  pluginId: string
  socketId: string
  type: "open" | "message" | "error" | "close"
  data?: string
  binary?: boolean
  code?: number
  reason?: string
  error?: string
}

type LifecycleWaiter = {resolve: () => void; timer: number}
type ReadyWaiter = {resolve: () => void; reject: (error: Error) => void; timer: number}

type PluginFrame = {
  info: PluginInfo
  frame: HTMLIFrameElement
  subscriptions: Set<string>
  pluginSubscriptions: Set<string>
  commandKeys: Set<string>
  panelKeys: Set<string>
  tabKeys: Set<string>
  socketIds: Set<string>
  expressRouteIds: Set<string>
  httpRequestIds: Set<string>
  fileEditorKeys: Set<string>
  uiWindowIds: Set<string>
  exposedMethods: Set<string>
  monacoProviderKeys: Set<string>
  redactionTokens: Set<string>
  lifecycleWaiters: Map<string, LifecycleWaiter>
  readyWaiter?: ReadyWaiter
  ready: boolean
  dispose: () => Promise<void>
}

const MAX_FAILURES = 3

export class PluginRuntime {
  private readonly frames = new Map<string, PluginFrame>()
  private readonly commands = new Map<string, PluginCommand>()
  private readonly commandOwners = new Map<string, PluginFrame>()
  private readonly panels = new Map<string, PluginPanel>()
  private readonly tabs = new Map<string, {plugin: PluginFrame; tab: PluginUITabInfo}>()
  private readonly fileEditors = new Map<string, {plugin: PluginFrame; editor: PluginFileEditor}>()
  private readonly pluginRPCWaiters = new Map<string, {resolve: (value: unknown) => void; reject: (error: Error) => void; timer: number}>()
  private readonly monacoProviders = new Map<string, {plugin: PluginFrame; kind: "diagnostics" | "completions"; language: string}>()
  private offEvent?: () => void
  private offList?: () => void
  private offReload?: () => void
  private offSocket?: () => void
  private offHTTP?: () => void
  private offFileOpening?: () => void
  private offUIAction?: () => void
  private offUIClosed?: () => void
  private offMonacoRequest?: () => void
  private started = false
  private lifecycleGeneration = 0
  private reloadState?: {generation: number; promise: Promise<void>}

  async start(): Promise<void> {
    if (this.started) return
    const generation = ++this.lifecycleGeneration
    this.started = true
    this.offEvent = Events.On("plugin:event", event => {
      try {
        const payload = JSON.parse(event.data) as PluginEvent
        this.dispatch(payload)
      } catch {
        // Plugin events are best effort and must not affect the application.
      }
    })
    this.offList = Events.On("plugin:list", () => void this.reload(generation))
    this.offReload = Events.On("plugin:reload", event => void this.reloadOne(String(event.data), generation))
    this.offSocket = Events.On("plugin:socket", event => {
      try { this.dispatchSocket(JSON.parse(event.data) as PluginSocketEvent) } catch { /* host events are best effort */ }
    })
    this.offHTTP = Events.On("plugin:http", event => {
      try { this.dispatchHTTP(JSON.parse(event.data) as PluginHttpRequestEvent) } catch { /* host events are best effort */ }
    })
    this.offFileOpening = Events.On("plugin:file-opening", event => {
      try { void this.dispatchFileOpening(JSON.parse(event.data) as {requestId: string; pluginId: string; editorId: string; file: {path: string; name: string; extension: string}}) } catch { /* host events are best effort */ }
    })
    this.offUIAction = Events.On("plugin:ui-action", event => {
      try { this.dispatchUIAction(JSON.parse(event.data) as {pluginId: string; windowId: string; action: string; value?: unknown}) } catch { /* host events are best effort */ }
    })
    this.offUIClosed = Events.On("plugin:ui-closed", event => {
      try { this.dispatchUIClosed(JSON.parse(event.data) as {pluginId: string; windowId: string}) } catch { /* host events are best effort */ }
    })
    this.offMonacoRequest = Events.On("plugin:monaco-request", event => {
      try { void this.dispatchMonacoRequest(JSON.parse(event.data) as {requestId: string; kind: "diagnostics" | "completions"; language: string; context: unknown}) } catch { /* host events are best effort */ }
    })
    await this.reload(generation)
  }

  /**
   * Refreshes the host-side plugin registry. This is intentionally public so
   * surfaces that can be opened after the main window (for example the RC
   * chat) do not depend on having received the original Wails event.
   */
  async refresh(): Promise<void> {
    if (!this.started) {
      await this.start()
      return
    }
    await this.reload()
  }

  async stop(): Promise<void> {
    if (!this.started && this.frames.size === 0) return
    this.lifecycleGeneration++
    this.started = false
    this.offEvent?.(); this.offList?.(); this.offReload?.(); this.offSocket?.(); this.offHTTP?.(); this.offFileOpening?.(); this.offUIAction?.(); this.offUIClosed?.(); this.offMonacoRequest?.()
    this.offEvent = undefined
    this.offList = undefined
    this.offReload = undefined
    this.offSocket = undefined
    this.offHTTP = undefined
    this.offFileOpening = undefined
    this.offUIAction = undefined
    this.offUIClosed = undefined
    this.offMonacoRequest = undefined
    const frames = [...this.frames.values()]
    this.frames.clear()
    this.commands.clear()
    this.commandOwners.clear()
    this.panels.clear()
    this.tabs.clear()
    this.fileEditors.clear()
    this.monacoProviders.clear()
    for (const waiter of this.pluginRPCWaiters.values()) {
      window.clearTimeout(waiter.timer)
      waiter.reject(new Error("plugin runtime stopped"))
    }
    this.pluginRPCWaiters.clear()
    this.notifyCommandChange()
    this.notifyTabChange()
    await Promise.all(frames.map(frame => frame.dispose()))
  }

  getCommands(): PluginCommand[] { return [...this.commands.values()] }
  getPanels(): PluginPanel[] { return [...this.panels.values()] }
  getTabs(): PluginUITabInfo[] {
    return [...this.tabs.values()]
      .map(({tab}) => tab)
      .sort((left, right) => (left.order ?? 0) - (right.order ?? 0) || left.title.localeCompare(right.title) || `${left.pluginId}:${left.id}`.localeCompare(`${right.pluginId}:${right.id}`))
  }

  sendTabAction(pluginId: string, tabId: string, action: string, value: PluginUIPrimitive): boolean {
    const key = `${pluginId}:${tabId}`
    const registration = this.tabs.get(key)
    if (!registration?.plugin.ready || !registration.plugin.frame.contentWindow) return false
    registration.plugin.frame.contentWindow.postMessage({type: "tabAction", tabId, action, value}, "*")
    return true
  }

  executeCommand(name: string, args: string[] = []): boolean {
    const query = name.trim().replace(/^\/+/, "").toLowerCase()
    if (!query) return false
    const matches = [...this.commands.entries()].filter(([key, command]) => {
      return command.id.toLowerCase() === query || key.toLowerCase() === query
    })
    if (matches.length !== 1) return false
    const [key, command] = matches[0]
    const plugin = this.commandOwners.get(key)
    if (!plugin?.ready || !plugin.frame.contentWindow) return false
    plugin.frame.contentWindow.postMessage({type: "commandInvoke", id: command.id, args}, "*")
    return true
  }

  private reload(generation = this.lifecycleGeneration): Promise<void> {
    if (this.reloadState?.generation === generation) return this.reloadState.promise
    const promise = this.reloadInternal(generation)
    this.reloadState = {generation, promise}
    void promise.then(
      () => this.clearReloadState(promise),
      () => this.clearReloadState(promise),
    )
    return promise
  }

  private clearReloadState(promise: Promise<void>): void {
    if (this.reloadState?.promise === promise) this.reloadState = undefined
  }

  private isActive(generation: number): boolean {
    return this.started && this.lifecycleGeneration === generation
  }

  private async reloadInternal(generation: number): Promise<void> {
    if (!this.isActive(generation)) return
    const plugins = (await rcService.getPlugins()) ?? []
    if (!this.isActive(generation)) return
    const active = new Set(plugins.filter(plugin => plugin.enabled && plugin.status === "ready").map(plugin => plugin.manifest.id))
    for (const [id, frame] of this.frames) {
      if (!this.isActive(generation)) return
      if (!active.has(id)) {
        await frame.dispose()
        this.frames.delete(id)
      }
    }
    for (const plugin of plugins) {
      if (!this.isActive(generation)) return
      if (!active.has(plugin.manifest.id) || this.frames.has(plugin.manifest.id)) continue
      try {
        await this.load(plugin, generation)
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error)
        void this.log(plugin.manifest.id, "error", `Failed to load plugin: ${message}`).catch(() => {})
      }
    }
  }

  private async reloadOne(id: string, generation = this.lifecycleGeneration): Promise<void> {
    if (!this.isActive(generation)) return
    const current = this.frames.get(id)
    if (current) {
      await current.dispose()
      this.frames.delete(id)
    }
    if (!this.isActive(generation)) return
    const plugins = (await rcService.getPlugins()) ?? []
    if (!this.isActive(generation)) return
    const info = plugins.find(plugin => plugin.manifest.id === id)
    if (info?.enabled && info.status === "ready") {
      try {
        await this.load(info, generation)
      } catch {
        this.fail(id)
      }
    }
  }

  private async load(info: PluginInfo, generation: number): Promise<void> {
    if (!this.isActive(generation)) return
    const bundle = await rcService.getPluginBundle(info.manifest.id)
    if (!this.isActive(generation)) return
    const frame = document.createElement("iframe")
    frame.setAttribute("sandbox", "allow-scripts")
    frame.setAttribute("aria-hidden", "true")
    frame.style.display = "none"

    let removeMessageListener = () => {}
    const pluginFrame = {} as PluginFrame
    pluginFrame.info = info
    pluginFrame.frame = frame
    pluginFrame.subscriptions = new Set()
    pluginFrame.pluginSubscriptions = new Set()
    pluginFrame.commandKeys = new Set()
    pluginFrame.panelKeys = new Set()
    pluginFrame.tabKeys = new Set()
    pluginFrame.socketIds = new Set()
    pluginFrame.expressRouteIds = new Set()
    pluginFrame.httpRequestIds = new Set()
    pluginFrame.fileEditorKeys = new Set()
    pluginFrame.uiWindowIds = new Set()
    pluginFrame.exposedMethods = new Set()
    pluginFrame.monacoProviderKeys = new Set()
    pluginFrame.redactionTokens = new Set()
    pluginFrame.lifecycleWaiters = new Map()
    pluginFrame.ready = false
    let resolveReady!: () => void
    let rejectReady!: (error: Error) => void
    const readyPromise = new Promise<void>((resolve, reject) => {
      resolveReady = resolve
      rejectReady = reject
    })
    pluginFrame.readyWaiter = {
      resolve: resolveReady,
      reject: rejectReady,
      timer: window.setTimeout(() => {
        if (!pluginFrame.readyWaiter) return
        pluginFrame.readyWaiter = undefined
        rejectReady(new Error("plugin did not finish loading within 5 seconds"))
      }, 5000),
    }
    let disposed = false
    let initTimer = 0
    let initAttempts = 0
    const postInit = () => {
      if (disposed || pluginFrame.ready) return
      frame.contentWindow?.postMessage({type: "init", id: info.manifest.id, bundle}, "*")
      initAttempts++
      if (initAttempts < 4) initTimer = window.setTimeout(postInit, 250)
    }
    pluginFrame.dispose = async () => {
      if (disposed) return
      disposed = true
      if (initTimer) window.clearTimeout(initTimer)
      if (pluginFrame.readyWaiter) {
        window.clearTimeout(pluginFrame.readyWaiter.timer)
        pluginFrame.readyWaiter = undefined
        resolveReady()
      }
      if (pluginFrame.ready && frame.contentWindow) {
        await new Promise<void>(resolve => {
          const requestId = crypto.randomUUID()
          const timer = window.setTimeout(() => {
            pluginFrame.lifecycleWaiters.delete(requestId)
            resolve()
          }, 2000)
          pluginFrame.lifecycleWaiters.set(requestId, {resolve, timer})
          frame.contentWindow?.postMessage({type: "lifecycle", action: "unload", requestId}, "*")
        })
      }
      await Promise.all([
        rcService.pluginCall(info.manifest.id, "sockets.closeAll", []).catch(() => undefined),
        rcService.pluginCall(info.manifest.id, "express.close", []).catch(() => undefined),
        rcService.pluginCall(info.manifest.id, "filebrowser.editor.closeAll", []).catch(() => undefined),
        rcService.pluginCall(info.manifest.id, "ui.window.closeAll", []).catch(() => undefined),
        rcService.pluginCall(info.manifest.id, "ui.tabs.closeAll", []).catch(() => undefined),
        rcService.pluginCall(info.manifest.id, "monaco.language.closeAll", []).catch(() => undefined),
      ])
      for (const key of pluginFrame.commandKeys) {
        if (this.commandOwners.get(key) !== pluginFrame) continue
        this.commands.delete(key)
        this.commandOwners.delete(key)
      }
      for (const key of pluginFrame.panelKeys) {
        if (this.panels.get(key)) this.panels.delete(key)
      }
      for (const key of pluginFrame.tabKeys) {
        if (this.tabs.get(key)?.plugin !== pluginFrame) continue
        this.tabs.delete(key)
      }
      for (const key of pluginFrame.fileEditorKeys) {
        if (this.fileEditors.get(key)?.plugin !== pluginFrame) continue
        this.fileEditors.delete(key)
      }
      for (const key of pluginFrame.monacoProviderKeys) {
        if (this.monacoProviders.get(key)?.plugin === pluginFrame) this.monacoProviders.delete(key)
      }
      this.notifyCommandChange()
      this.notifyTabChange()
      for (const waiter of pluginFrame.lifecycleWaiters.values()) window.clearTimeout(waiter.timer)
      pluginFrame.lifecycleWaiters.clear()
      removeMessageListener()
      frame.remove()
    }

    frame.addEventListener("load", postInit, {once: true})
    frame.addEventListener("error", () => {
      const error = new Error("plugin sandbox failed to load")
      if (pluginFrame.readyWaiter) {
        window.clearTimeout(pluginFrame.readyWaiter.timer)
        pluginFrame.readyWaiter = undefined
        rejectReady(error)
      }
      this.fail(info.manifest.id)
    })
    const onMessage = (event: MessageEvent) => {
      if (event.source !== frame.contentWindow) return
      void this.handleMessage(pluginFrame, event.data as RuntimeMessage)
    }
    window.addEventListener("message", onMessage)
    removeMessageListener = () => window.removeEventListener("message", onMessage)
    frame.srcdoc = createRuntimeDocument()
    document.body.appendChild(frame)
    this.frames.set(info.manifest.id, pluginFrame)
    void this.log(info.manifest.id, "info", "Starting plugin sandbox").catch(() => {})
    initTimer = window.setTimeout(postInit, 250)
    try {
      await readyPromise
    } catch (error) {
      await pluginFrame.dispose()
      if (this.frames.get(info.manifest.id) === pluginFrame) this.frames.delete(info.manifest.id)
      throw error
    }
    if (!this.isActive(generation)) {
      await pluginFrame.dispose()
      if (this.frames.get(info.manifest.id) === pluginFrame) this.frames.delete(info.manifest.id)
    }
  }

  private async handleMessage(plugin: PluginFrame, message: RuntimeMessage): Promise<void> {
    if (!message || typeof message.type !== "string") return
    if (message.type === "ready") {
      plugin.ready = true
      if (plugin.readyWaiter) {
        window.clearTimeout(plugin.readyWaiter.timer)
        const waiter = plugin.readyWaiter
        plugin.readyWaiter = undefined
        waiter.resolve()
      }
      await rcService.recordPluginSuccess(plugin.info.manifest.id).catch(() => undefined)
      void this.log(plugin.info.manifest.id, "info", "Plugin sandbox ready").catch(() => {})
      return
    }
    if (message.type === "error") {
      const error = new Error(message.message)
      if (plugin.readyWaiter) {
        window.clearTimeout(plugin.readyWaiter.timer)
        const waiter = plugin.readyWaiter
        plugin.readyWaiter = undefined
        waiter.reject(error)
      }
      await this.log(plugin.info.manifest.id, "error", message.message)
      await this.fail(plugin.info.manifest.id)
      return
    }
    if (message.type === "notification") {
      if (!(plugin.info.approvedApis ?? []).includes("ui.notification")) {
        await this.log(plugin.info.manifest.id, "warn", "Notification ignored: ui.notification permission is not approved")
        return
      }
      const notification = message.notification
      if (!notification || typeof notification.message !== "string" || !notification.message.trim() || notification.message.length > 2000) return
      const title = typeof notification.title === "string" ? notification.title.trim().slice(0, 120) : ""
      const level = notification.level === "success" || notification.level === "warning" || notification.level === "error" ? notification.level : "info"
      const durationMs = typeof notification.durationMs === "number" && Number.isFinite(notification.durationMs)
        ? Math.max(1000, Math.min(15000, Math.round(notification.durationMs)))
        : undefined
      window.dispatchEvent(new CustomEvent("gorc:plugin-notification", {detail: {pluginId: plugin.info.manifest.id, title, message: notification.message, level, durationMs}}))
      return
    }
    if (message.type === "lifecycleResult") {
      const waiter = plugin.lifecycleWaiters.get(message.requestId)
      if (!waiter) return
      window.clearTimeout(waiter.timer)
      plugin.lifecycleWaiters.delete(message.requestId)
      waiter.resolve()
      if (!message.ok) this.log(plugin.info.manifest.id, "error", message.error ?? "plugin lifecycle failed")
      return
    }
    if (message.type === "subscribe") { plugin.subscriptions.add(message.name); return }
    if (message.type === "pluginSubscribe") { plugin.pluginSubscriptions.add(message.channel); return }
    if (message.type === "pluginUnsubscribe") { plugin.pluginSubscriptions.delete(message.channel); return }
    if (message.type === "command") {
      const key = `${plugin.info.manifest.id}:${message.command.id}`
      plugin.commandKeys.add(key)
      this.commands.set(key, message.command)
      this.commandOwners.set(key, plugin)
      this.notifyCommandChange()
      void this.log(plugin.info.manifest.id, "info", `Registered command /${message.command.id}`).catch(() => {})
      return
    }
    if (message.type === "commandRemoved") {
      const key = `${plugin.info.manifest.id}:${message.id}`
      if (this.commandOwners.get(key) !== plugin) return
      plugin.commandKeys.delete(key)
      this.commands.delete(key)
      this.commandOwners.delete(key)
      this.notifyCommandChange()
      return
    }
    if (message.type === "panel") {
      const key = `${plugin.info.manifest.id}:${message.panel.id}`
      plugin.panelKeys.add(key)
      this.panels.set(key, message.panel)
      return
    }
    if (message.type === "tab") {
      try {
        await rcService.pluginCall(plugin.info.manifest.id, "ui.tabs.register", [message.tab])
      } catch (error) {
        await this.log(plugin.info.manifest.id, "error", `Tab registration failed: ${error instanceof Error ? error.message : String(error)}`)
        return
      }
      const key = `${plugin.info.manifest.id}:${message.tab.id}`
      const current = this.tabs.get(key)
      if (current && current.plugin !== plugin) return
      const tab: PluginUITabInfo = {...message.tab, pluginId: plugin.info.manifest.id}
      plugin.tabKeys.add(key)
      this.tabs.set(key, {plugin, tab})
      this.notifyTabChange()
      if (message.tab.open) this.requestTabOpen(plugin.info.manifest.id, message.tab.id)
      void this.log(plugin.info.manifest.id, "info", `Registered RC tab ${message.tab.id}`).catch(() => {})
      return
    }
    if (message.type === "tabUpdate") {
      const key = `${plugin.info.manifest.id}:${message.tabId}`
      const registration = this.tabs.get(key)
      if (registration?.plugin !== plugin) return
      try {
        await rcService.pluginCall(plugin.info.manifest.id, "ui.tabs.update", [message.tabId, message.view])
      } catch (error) {
        await this.log(plugin.info.manifest.id, "error", `Tab update failed: ${error instanceof Error ? error.message : String(error)}`)
        return
      }
      registration.tab = {...registration.tab, view: message.view}
      this.notifyTabChange()
      return
    }
    if (message.type === "tabRemoved") {
      const key = `${plugin.info.manifest.id}:${message.tabId}`
      const registration = this.tabs.get(key)
      if (registration?.plugin !== plugin) return
      await rcService.pluginCall(plugin.info.manifest.id, "ui.tabs.close", [message.tabId]).catch(() => undefined)
      plugin.tabKeys.delete(key)
      this.tabs.delete(key)
      this.notifyTabChange()
      plugin.frame.contentWindow?.postMessage({type: "tabClosed", tabId: message.tabId}, "*")
      return
    }
    if (message.type === "tabOpen") {
      const key = `${plugin.info.manifest.id}:${message.tabId}`
      if (this.tabs.get(key)?.plugin === plugin) this.requestTabOpen(plugin.info.manifest.id, message.tabId)
      return
    }
    if (message.type === "fileEditor") {
      try {
        await rcService.pluginCall(plugin.info.manifest.id, "filebrowser.editor.register", [message.editor])
        const key = `${plugin.info.manifest.id}:${message.editor.id}`
        plugin.fileEditorKeys.add(key)
        this.fileEditors.set(key, {plugin, editor: message.editor})
      } catch (error) {
        await this.log(plugin.info.manifest.id, "error", `File editor registration failed: ${error instanceof Error ? error.message : String(error)}`)
      }
      return
    }
    if (message.type === "fileEditorRemoved") {
      const key = `${plugin.info.manifest.id}:${message.id}`
      await rcService.pluginCall(plugin.info.manifest.id, "filebrowser.editor.unregister", [message.id]).catch(() => undefined)
      plugin.fileEditorKeys.delete(key)
      if (this.fileEditors.get(key)?.plugin === plugin) this.fileEditors.delete(key)
      return
    }
    if (message.type === "pluginExpose") {
      if (message.method.trim() && message.method.length <= 128) plugin.exposedMethods.add(message.method.trim())
      return
    }
    if (message.type === "pluginExposeRemoved") {
      plugin.exposedMethods.delete(message.method)
      return
    }
    if (message.type === "pluginResponse") {
      const waiter = this.pluginRPCWaiters.get(message.requestId)
      if (!waiter) return
      window.clearTimeout(waiter.timer)
      this.pluginRPCWaiters.delete(message.requestId)
      if (message.ok) waiter.resolve(message.value)
      else waiter.reject(new Error(message.error ?? "plugin RPC failed"))
      return
    }
    if (message.type === "monacoProvider") {
      try {
        await rcService.pluginCall(plugin.info.manifest.id, "monaco.provider.register", [message.kind, message.language])
      } catch (error) {
        await this.log(plugin.info.manifest.id, "error", `Monaco provider registration failed: ${error instanceof Error ? error.message : String(error)}`)
        return
      }
      const key = `${plugin.info.manifest.id}:${message.kind}:${message.language}`
      plugin.monacoProviderKeys.add(key)
      this.monacoProviders.set(key, {plugin, kind: message.kind, language: message.language})
      return
    }
    if (message.type === "monacoProviderRemoved") {
      const key = `${plugin.info.manifest.id}:${message.kind}:${message.language}`
      await rcService.pluginCall(plugin.info.manifest.id, "monaco.provider.unregister", [message.kind, message.language]).catch(() => undefined)
      plugin.monacoProviderKeys.delete(key)
      if (this.monacoProviders.get(key)?.plugin === plugin) this.monacoProviders.delete(key)
      return
    }
    if (message.type === "monacoResponse") {
      await rcService.pluginMonacoResult(message.requestId, plugin.info.manifest.id, message.ok ? message.value : [], message.ok ? "" : (message.error ?? "Monaco provider failed")).catch(error => {
        void this.log(plugin.info.manifest.id, "error", `Monaco provider response failed: ${error instanceof Error ? error.message : String(error)}`)
      })
      return
    }
    if (message.type === "fileOpenResult") {
      await rcService.pluginCall(plugin.info.manifest.id, "filebrowser.openResult", [message.requestId, message.handled === true]).catch(error => this.log(plugin.info.manifest.id, "error", `File editor open failed: ${error instanceof Error ? error.message : String(error)}`))
      return
    }
    if (message.type === "uiActionSubscribe") return
    if (message.type === "uiWindowOpen") {
      const value = await rcService.pluginCall(plugin.info.manifest.id, "ui.window.open", [message.options])
      if (value && typeof value === "object" && typeof (value as {id?: unknown}).id === "string") plugin.uiWindowIds.add(String((value as {id: string}).id))
      plugin.frame.contentWindow?.postMessage({type: "rpcResult", requestId: "", ok: true, value}, "*")
      return
    }
    if (message.type === "uiWindowUpdate") {
      await rcService.pluginCall(plugin.info.manifest.id, "ui.window.update", [message.windowId, message.view])
      return
    }
    if (message.type === "uiWindowClose") {
      await rcService.pluginCall(plugin.info.manifest.id, "ui.window.close", [message.windowId])
      plugin.uiWindowIds.delete(message.windowId)
      return
    }
    if (message.type === "log") {
      await this.log(plugin.info.manifest.id, message.level, this.redact(plugin, message.message))
      return
    }
    if (message.type === "httpResponse") {
      if (!plugin.httpRequestIds.delete(message.requestId)) return
      try {
        await rcService.pluginCall(plugin.info.manifest.id, "express.respond", [message.requestId, {status: message.status, headers: message.headers ?? {}, body: message.body ?? ""}])
      } catch (error) {
        await this.log(plugin.info.manifest.id, "error", `Internal API response failed: ${error instanceof Error ? error.message : String(error)}`)
      }
      return
    }
    if (message.type !== "rpc") return
    try {
      let value: unknown
      switch (message.method) {
        case "storage.get": value = await rcService.pluginStorageGet(plugin.info.manifest.id, String(message.args[0])); break
        case "storage.set": value = await rcService.pluginStorageSet(plugin.info.manifest.id, String(message.args[0]), String(message.args[1])); break
        case "storage.delete": value = await rcService.pluginStorageDelete(plugin.info.manifest.id, String(message.args[0])); break
        case "secrets.get": value = await rcService.pluginSecretGet(plugin.info.manifest.id, String(message.args[0])); break
        case "secrets.set": value = await rcService.pluginSecretSet(plugin.info.manifest.id, String(message.args[0]), String(message.args[1])); break
        case "secrets.delete": value = await rcService.pluginSecretDelete(plugin.info.manifest.id, String(message.args[0])); break
        case "network.request": value = await rcService.pluginRequest(plugin.info.manifest.id, normalizePluginRequest(message.args[0])); break
        case "sockets.open": {
          value = await rcService.pluginCall(plugin.info.manifest.id, "sockets.open", message.args)
          if (value && typeof value === "object" && typeof (value as {id?: unknown}).id === "string") plugin.socketIds.add(String((value as {id: string}).id))
          break
        }
        case "sockets.send": value = await rcService.pluginCall(plugin.info.manifest.id, "sockets.send", message.args); break
        case "sockets.close": {
          value = await rcService.pluginCall(plugin.info.manifest.id, "sockets.close", message.args)
          if (typeof message.args[0] === "string") plugin.socketIds.delete(message.args[0])
          break
        }
        case "sockets.closeAll": value = await rcService.pluginCall(plugin.info.manifest.id, "sockets.closeAll", message.args); plugin.socketIds.clear(); break
        case "plugins.list": value = await rcService.pluginCall(plugin.info.manifest.id, "plugins.list", message.args); break
        case "plugins.send": value = await this.sendPluginMessage(plugin, message.args); break
        case "plugins.call": value = await this.callPlugin(plugin, message.args); break
        case "express.listen": {
          value = await rcService.pluginCall(plugin.info.manifest.id, "express.listen", message.args)
          this.rememberToken(plugin, value)
          break
        }
        case "express.register": {
          value = await rcService.pluginCall(plugin.info.manifest.id, "express.register", message.args)
          this.rememberToken(plugin, value)
          if (value && typeof value === "object" && typeof (value as {routeId?: unknown}).routeId === "string") plugin.expressRouteIds.add(String((value as {routeId: string}).routeId))
          break
        }
        case "express.unregister": {
          value = await rcService.pluginCall(plugin.info.manifest.id, "express.unregister", message.args)
          if (typeof message.args[0] === "string") plugin.expressRouteIds.delete(message.args[0])
          break
        }
        case "express.respond": value = await rcService.pluginCall(plugin.info.manifest.id, "express.respond", message.args); break
        case "express.close": value = await rcService.pluginCall(plugin.info.manifest.id, "express.close", message.args); plugin.expressRouteIds.clear(); break
        case "filebrowser.readText": value = await rcService.pluginCall(plugin.info.manifest.id, "filebrowser.readText", message.args); break
        case "filebrowser.writeText": value = await rcService.pluginCall(plugin.info.manifest.id, "filebrowser.writeText", message.args); break
        case "filebrowser.editor.closeAll": value = await rcService.pluginCall(plugin.info.manifest.id, "filebrowser.editor.closeAll", message.args); plugin.fileEditorKeys.clear(); break
        case "monaco.language.register": value = await rcService.pluginCall(plugin.info.manifest.id, "monaco.language.register", message.args); break
        case "monaco.language.unregister": value = await rcService.pluginCall(plugin.info.manifest.id, "monaco.language.unregister", message.args); break
        case "monaco.language.closeAll": value = await rcService.pluginCall(plugin.info.manifest.id, "monaco.language.closeAll", message.args); break
        case "ui.window.open": {
          value = await rcService.pluginCall(plugin.info.manifest.id, "ui.window.open", message.args)
          if (value && typeof value === "object" && typeof (value as {id?: unknown}).id === "string") plugin.uiWindowIds.add(String((value as {id: string}).id))
          break
        }
        case "ui.window.update": value = await rcService.pluginCall(plugin.info.manifest.id, "ui.window.update", message.args); break
        case "ui.window.close": {
          value = await rcService.pluginCall(plugin.info.manifest.id, "ui.window.close", message.args)
          if (typeof message.args[0] === "string") plugin.uiWindowIds.delete(message.args[0])
          break
        }
        case "ui.window.closeAll": value = await rcService.pluginCall(plugin.info.manifest.id, "ui.window.closeAll", message.args); plugin.uiWindowIds.clear(); break
        case "nc.readWeapon": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.readWeapon", message.args); break
        case "nc.readClass": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.readClass", message.args); break
        case "nc.readNPC": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.readNPC", message.args); break
        case "nc.readNPCFlags": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.readNPCFlags", message.args); break
        case "nc.readNPCAttributes": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.readNPCAttributes", message.args); break
        case "nc.list": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.list", message.args); break
        case "nc.saveWeapon": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.saveWeapon", message.args); break
        case "nc.saveClass": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.saveClass", message.args); break
        case "nc.saveNPC": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.saveNPC", message.args); break
        case "nc.saveNPCFlags": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.saveNPCFlags", message.args); break
        case "nc.createWeapon": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.createWeapon", message.args); break
        case "nc.deleteWeapon": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.deleteWeapon", message.args); break
        case "nc.createClass": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.createClass", message.args); break
        case "nc.deleteClass": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.deleteClass", message.args); break
        case "nc.createNPC": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.createNPC", message.args); break
        case "nc.deleteNPC": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.deleteNPC", message.args); break
        case "nc.resetNPC": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.resetNPC", message.args); break
        case "actions.pm.send": value = await rcService.pluginCall(plugin.info.manifest.id, "pm.send", message.args); break
        case "actions.admin.send": value = await rcService.pluginCall(plugin.info.manifest.id, "admin.send", message.args); break
        case "actions.rc.execute": value = await rcService.pluginCall(plugin.info.manifest.id, "rc.execute", message.args); break
        case "actions.nc.saveWeapon": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.saveWeapon", message.args); break
        case "actions.nc.saveClass": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.saveClass", message.args); break
        case "actions.nc.saveNPC": value = await rcService.pluginCall(plugin.info.manifest.id, "nc.saveNPC", message.args); break
        default: throw new Error(`unknown plugin RPC method: ${message.method}`)
      }
      plugin.frame.contentWindow?.postMessage({type: "rpcResult", requestId: message.requestId, ok: true, value}, "*")
    } catch (error) {
      plugin.frame.contentWindow?.postMessage({type: "rpcResult", requestId: message.requestId, ok: false, error: error instanceof Error ? error.message : String(error)}, "*")
    }
  }

  private async sendPluginMessage(sender: PluginFrame, args: unknown[]): Promise<void> {
    const targetId = typeof args[0] === "string" ? args[0].trim() : ""
    const channel = typeof args[1] === "string" ? args[1].trim() : ""
    if (!targetId || !channel || channel.length > 128 || /[\r\n]/.test(channel)) throw new Error("plugins.send requires a valid target and channel")
    let serialized: string
    try {
      serialized = JSON.stringify(args[2])
    } catch {
      throw new Error("plugin message data must be JSON serializable")
    }
    if (serialized === undefined || serialized.length > 64 * 1024) throw new Error("plugin message exceeds 64 KiB")
    await rcService.pluginCall(sender.info.manifest.id, "plugins.authorize", [targetId])
    const target = this.frames.get(targetId)
    if (!target?.ready || !target.frame.contentWindow) throw new Error("target plugin is not ready")
    if (!target.pluginSubscriptions.has(channel)) return
    target.frame.contentWindow.postMessage({type: "pluginMessage", from: sender.info.manifest.id, channel, data: JSON.parse(serialized)}, "*")
  }

  private async callPlugin(sender: PluginFrame, args: unknown[]): Promise<unknown> {
    const targetId = typeof args[0] === "string" ? args[0].trim() : ""
    const method = typeof args[1] === "string" ? args[1].trim() : ""
    if (!targetId || !method || method.length > 128 || /[\r\n]/.test(method)) throw new Error("plugins.call requires a valid target and method")
    let serialized: string
    try { serialized = JSON.stringify(args[2]) } catch { throw new Error("plugin RPC data must be JSON serializable") }
    if (serialized === undefined || serialized.length > 64 * 1024) throw new Error("plugin RPC payload exceeds 64 KiB")
    await rcService.pluginCall(sender.info.manifest.id, "plugins.authorize", [targetId])
    const target = this.frames.get(targetId)
    if (!target?.ready || !target.frame.contentWindow) throw new Error("target plugin is not ready")
    if (!target.exposedMethods.has(method)) throw new Error("target plugin has not exposed this method")
    const requestId = crypto.randomUUID()
    const promise = new Promise<unknown>((resolve, reject) => {
      const timer = window.setTimeout(() => {
        this.pluginRPCWaiters.delete(requestId)
        reject(new Error("plugin RPC timeout"))
      }, 15000)
      this.pluginRPCWaiters.set(requestId, {resolve, reject, timer})
    })
    target.frame.contentWindow.postMessage({type: "pluginRequest", requestId, from: sender.info.manifest.id, method, data: JSON.parse(serialized)}, "*")
    return promise
  }

  private async log(id: string, level: string, message: string): Promise<void> {
    console.info(`[plugin:${id}] ${level}: ${message}`)
    await rcService.appendPluginLog(id, level, message)
  }

  private rememberToken(plugin: PluginFrame, value: unknown): void {
    if (!value || typeof value !== "object") return
    const token = (value as {token?: unknown}).token
    if (typeof token === "string" && token.length >= 8) plugin.redactionTokens.add(token)
  }

  private redact(plugin: PluginFrame, message: string): string {
    let safe = message
    for (const token of plugin.redactionTokens) safe = safe.split(token).join("[redacted]")
    return safe
  }

  private dispatch(payload: PluginEvent): void {
    for (const plugin of this.frames.values()) {
      if (!(plugin.info.approvedEvents ?? []).includes(payload.name)) continue
      if (!plugin.subscriptions.has(payload.name)) continue
      plugin.frame.contentWindow?.postMessage({type: "event", name: payload.name, data: payload.data}, "*")
    }
  }

  private dispatchSocket(payload: PluginSocketEvent): void {
    const plugin = this.frames.get(payload.pluginId)
    if (!plugin?.ready) return
    plugin.frame.contentWindow?.postMessage({type: "socketEvent", socketId: payload.socketId, event: payload}, "*")
  }

  private dispatchHTTP(payload: PluginHttpRequestEvent): void {
    const plugin = this.frames.get(payload.pluginId)
    if (!plugin?.ready) return
    plugin.httpRequestIds.add(payload.requestId)
    plugin.frame.contentWindow?.postMessage({type: "httpRequest", request: payload}, "*")
  }

  private async dispatchFileOpening(payload: {requestId: string; pluginId: string; editorId: string; file: {path: string; name: string; extension: string}}): Promise<void> {
    const key = `${payload.pluginId}:${payload.editorId}`
    const registration = this.fileEditors.get(key)
    if (!registration?.plugin.ready || !registration.plugin.frame.contentWindow) return
    registration.plugin.frame.contentWindow.postMessage({type: "fileOpen", requestId: payload.requestId, editorId: payload.editorId, file: payload.file}, "*")
  }

  private async dispatchMonacoRequest(payload: {requestId: string; kind: "diagnostics" | "completions"; language: string; context: unknown}): Promise<void> {
    const language = payload.language.trim().toLowerCase()
    const provider = [...this.monacoProviders.values()].find(candidate => {
      return candidate.kind === payload.kind && (candidate.language.toLowerCase() === language || candidate.language === "*") && candidate.plugin.ready && !!candidate.plugin.frame.contentWindow
    })
    if (!provider?.plugin.frame.contentWindow) {
      await rcService.pluginMonacoResult(payload.requestId, "", [], "").catch(() => undefined)
      return
    }
    provider.plugin.frame.contentWindow.postMessage({type: "monacoRequest", requestId: payload.requestId, kind: payload.kind, language: payload.language, context: payload.context}, "*")
  }

  private dispatchUIAction(payload: {pluginId: string; windowId: string; action: string; value?: unknown}): void {
    const plugin = this.frames.get(payload.pluginId)
    if (!plugin?.ready) return
    plugin.frame.contentWindow?.postMessage({type: "uiAction", windowId: payload.windowId, action: payload.action, value: payload.value}, "*")
  }

  private dispatchUIClosed(payload: {pluginId: string; windowId: string}): void {
    const plugin = this.frames.get(payload.pluginId)
    if (!plugin?.ready) return
    plugin.uiWindowIds.delete(payload.windowId)
    plugin.frame.contentWindow?.postMessage({type: "uiClosed", windowId: payload.windowId}, "*")
  }

  private notifyCommandChange(): void {
    window.dispatchEvent(new Event("gorc:plugin-commands"))
  }

  private notifyTabChange(): void {
    window.dispatchEvent(new Event("gorc:plugin-tabs"))
  }

  private requestTabOpen(pluginId: string, tabId: string): void {
    window.dispatchEvent(new CustomEvent("gorc:plugin-tab-open", {detail: {pluginId, tabId}}))
  }

  private async fail(id: string): Promise<void> {
    const plugin = this.frames.get(id)
    if (!plugin) return
    const disabled = await rcService.recordPluginFailure(id).catch(() => false)
    if (disabled) {
      await this.log(id, "error", `Plugin disabled automatically after ${MAX_FAILURES} consecutive runtime failures`)
    }
  }
}

export const pluginRuntime = new PluginRuntime()

function normalizePluginRequest(value: unknown): Parameters<typeof rcService.pluginRequest>[1] {
  if (!value || typeof value !== "object") throw new Error("network.request requires a request object")
  const input = value as Record<string, unknown>
  if (typeof input.url !== "string" || !input.url.trim()) throw new Error("network.request requires a URL")

  const headers: Record<string, string> = {}
  if (input.headers !== undefined) {
    if (!input.headers || typeof input.headers !== "object" || Array.isArray(input.headers)) throw new Error("network.request headers must be an object")
    for (const [key, headerValue] of Object.entries(input.headers as Record<string, unknown>)) {
      if (typeof headerValue !== "string") throw new Error(`network.request header ${key} must be a string`)
      headers[key] = headerValue
    }
  }

  let body: string | undefined
  if (input.body !== undefined) {
    if (typeof input.body === "string") {
      body = input.body
    } else {
      try {
        body = JSON.stringify(input.body)
      } catch {
        throw new Error("network.request body must be serializable as JSON")
      }
      if (!Object.keys(headers).some(key => key.toLowerCase() === "content-type")) headers["Content-Type"] = "application/json"
    }
  }

  return {
    url: input.url,
    ...(typeof input.method === "string" && input.method.trim() ? {method: input.method} : {}),
    ...(Object.keys(headers).length ? {headers} : {}),
    ...(body !== undefined ? {body} : {})
  }
}

function createRuntimeDocument(): string {
  return `<!doctype html><html><body><script>
    let api;
    let pluginDefinition;
    let pluginInstance;
    let activeLifecycle;
    let initializing = false;
    let lifecycleStopped = false;
    const listeners = new Map();
    const commandHandlers = new Map();
    const socketHandlers = new Map();
    const sockets = new Map();
    const pendingSocketEvents = new Map();
    const pluginHandlers = new Map();
    const fileEditorHandlers = new Map();
    const pluginRpcHandlers = new Map();
    const uiActionHandlers = [];
    const uiClosedHandlers = [];
    const uiTabActionHandlers = [];
    const uiTabClosedHandlers = [];
    const monacoDiagnostics = new Map();
    const monacoCompletions = new Map();
    const automationCleanups = new Set();
    const expressRoutes = new Map();
    const reportError = error => {
      const message = error instanceof Error ? (error.stack || error.message) : String(error);
      parent.postMessage({type:'error',message},'*');
    };
    const rpc = (method, args) => new Promise((resolve, reject) => {
      const requestId = crypto.randomUUID();
      const timer = setTimeout(() => reject(new Error('plugin RPC timeout')), 15000);
      const handler = event => {
        if (event.data?.type !== 'rpcResult' || event.data.requestId !== requestId) return;
        clearTimeout(timer);
        window.removeEventListener('message', handler);
        event.data.ok ? resolve(event.data.value) : reject(new Error(event.data.error || 'plugin RPC failed'));
      };
      window.addEventListener('message', handler);
      parent.postMessage({type:'rpc', requestId, method, args}, '*');
    });
    const Plugin = class {
      constructor() { pluginInstance = this; if (api) this.__attach(api); }
      __attach(context) {
        this.api = context;
        this.events = context.events;
        this.commands = context.commands;
        this.panels = context.panels;
        this.storage = context.storage;
        this.secrets = context.secrets;
        this.network = context.network;
        this.sockets = context.sockets;
        this.plugins = context.plugins;
        this.express = context.express;
        this.fileBrowser = context.fileBrowser;
        this.ui = context.ui;
        this.notifications = context.notifications;
        this.monaco = context.monaco;
        this.nc = context.nc;
        this.automation = context.automation;
        this.actions = context.actions;
      }
    };
    globalThis.registerGorcPlugin = definition => { pluginDefinition = definition; };
    const emitSocket = (socketId, event) => {
      const socket = sockets.get(socketId);
      const handlers = socketHandlers.get(socketId)?.get(event.type) || [];
      if (!socket && handlers.length === 0) {
        const pending = pendingSocketEvents.get(socketId) || [];
        pending.push(event);
        pendingSocketEvents.set(socketId, pending);
        return;
      }
      handlers.forEach(fn => Promise.resolve(fn(event.type === 'message' ? {data:event.data || '', binary:!!event.binary} : event.type === 'close' ? {code:event.code || 1000, reason:event.reason || ''} : event.error ? new Error(event.error) : undefined)).catch(reportError));
      if (event.type === 'close') {
        const socket = sockets.get(socketId);
        if (socket) socket.readyState = 'CLOSED';
      }
    };
    const createSocket = info => {
      const handlers = new Map();
      socketHandlers.set(info.id, handlers);
      const socket = {
        id: info.id,
        url: info.url,
        readyState: info.readyState || 'OPEN',
        on: (event, fn) => {
          const list = handlers.get(event) || [];
          list.push(fn);
          handlers.set(event, list);
          if (event === 'open') Promise.resolve().then(() => fn()).catch(reportError);
          return () => {
            const next = (handlers.get(event) || []).filter(item => item !== fn);
            handlers.set(event, next);
          };
        },
        send: data => {
          if (typeof data !== 'string') return Promise.reject(new Error('socket.send accepts strings only'));
          return rpc('sockets.send', [info.id, data]);
        },
        close: () => rpc('sockets.close', [info.id]).finally(() => { socket.readyState = 'CLOSED'; })
      };
      sockets.set(info.id, socket);
      const pending = pendingSocketEvents.get(info.id) || [];
      pendingSocketEvents.delete(info.id);
      pending.forEach(event => emitSocket(info.id, event));
      return socket;
    };
    const createExpressRequest = request => ({
      method: request.method,
      path: request.path,
      query: request.query || {},
      headers: request.headers || {},
      body: request.body || '',
      json: () => {
        try { return JSON.parse(request.body || 'null'); } catch { return null; }
      }
    });
    const createExpressResponse = requestId => {
      let status = 200;
      let headers = {};
      let body = '';
      let completed = false;
      const submit = value => {
        if (completed) return response;
        completed = true;
        if (value !== undefined) body = typeof value === 'string' ? value : JSON.stringify(value);
        parent.postMessage({type:'httpResponse', requestId, status, headers, body}, '*');
        return response;
      };
      const response = {
        status: code => { status = Number(code) || 200; return response; },
        set: values => { if (values && typeof values === 'object') Object.assign(headers, values); return response; },
        json: value => { headers['Content-Type'] = 'application/json'; return submit(value); },
        send: value => submit(value),
        text: value => submit(String(value ?? '')),
        end: () => submit()
      };
      return response;
    };
    const registerExpressRoute = (server, method, path, handler) => rpc('express.register', [method, path]).then(route => {
      expressRoutes.set(route.routeId, handler);
      return async () => {
        expressRoutes.delete(route.routeId);
        await rpc('express.unregister', [route.routeId]);
      };
    });
    const createExpressServer = info => {
      const server = {baseUrl: info.baseUrl, token: info.token};
      ['get','post','put','patch','delete'].forEach(method => {
        server[method] = (path, handler) => registerExpressRoute(server, method.toUpperCase(), path, handler);
      });
      return server;
    };
    const createAutomation = () => ({
      timeout: (handler, delay) => {
        let active = true;
        let timer;
        const cancel = () => {
          if (!active) return;
          active = false;
          clearTimeout(timer);
          automationCleanups.delete(cancel);
        };
        automationCleanups.add(cancel);
        timer = setTimeout(() => {
          if (!active) return;
          cancel();
          Promise.resolve(handler()).catch(reportError);
        }, Math.max(0, Number(delay) || 0));
        return cancel;
      },
      interval: (handler, delay) => {
        let active = true;
        let running = false;
        const timer = setInterval(() => {
          if (!active || running) return;
          running = true;
          Promise.resolve(handler()).catch(reportError).finally(() => { running = false; });
        }, Math.max(25, Number(delay) || 25));
        const cancel = () => {
          if (!active) return;
          active = false;
          clearInterval(timer);
          automationCleanups.delete(cancel);
        };
        automationCleanups.add(cancel);
        return cancel;
      },
      schedule: (handler, options = {}) => {
        const delay = Math.max(0, Number(options.delayMs) || 0);
        const requestedInterval = Number(options.intervalMs);
        const interval = Number.isFinite(requestedInterval) && requestedInterval > 0 ? Math.max(25, requestedInterval) : 0;
        let active = true;
        let paused = false;
        let running = false;
        let initialTimer;
        let intervalTimer;
        const run = () => {
          if (!active || paused || running) return;
          running = true;
          Promise.resolve(handler()).catch(reportError).finally(() => { running = false; });
        };
        const startInterval = () => {
          if (interval > 0 && !intervalTimer) intervalTimer = setInterval(run, interval);
        };
        const cancel = () => {
          if (!active) return;
          active = false;
          clearTimeout(initialTimer);
          if (intervalTimer) clearInterval(intervalTimer);
          automationCleanups.delete(cancel);
        };
        const pause = () => {
          if (!active) return;
          paused = true;
        };
        const resume = () => {
          if (!active) return;
          paused = false;
          run();
        };
        automationCleanups.add(cancel);
        if (options.immediate) {
          run();
          startInterval();
        } else {
          initialTimer = setTimeout(() => {
            initialTimer = undefined;
            run();
            startInterval();
          }, delay);
        }
        return {cancel, pause, resume};
      },
      debounce: (handler, wait) => {
        let active = true;
        let timer;
        const cancel = () => {
          if (!active) return;
          active = false;
          if (timer) clearTimeout(timer);
          automationCleanups.delete(cancel);
        };
        automationCleanups.add(cancel);
        return (...args) => {
          if (!active) return;
          if (timer) clearTimeout(timer);
          timer = setTimeout(() => {
            timer = undefined;
            if (active) Promise.resolve(handler(...args)).catch(reportError);
          }, Math.max(0, Number(wait) || 0));
        };
      },
      retry: async (operation, options = {}) => {
        const attempts = Math.max(1, Math.min(8, Number(options.attempts) || 3));
        const baseDelay = Math.max(0, Math.min(30000, Number(options.delayMs) || 250));
        const backoff = Math.max(1, Math.min(5, Number(options.backoff) || 2));
        let lastError;
        for (let attempt = 0; attempt < attempts; attempt++) {
          try { return await operation(); }
          catch (error) {
            lastError = error;
            if (attempt + 1 < attempts) await new Promise(resolve => setTimeout(resolve, baseDelay * Math.pow(backoff, attempt)));
          }
        }
        throw lastError || new Error('automation retry failed');
      }
    });
    const createUIWindow = info => ({
      id: info.id,
      update: view => rpc('ui.window.update', [info.id, view]),
      close: () => rpc('ui.window.close', [info.id])
    });
    const createUITab = options => {
      const tab = {
        id: options.id,
        update: view => { parent.postMessage({type:'tabUpdate', tabId: options.id, view}, '*'); },
        open: () => { parent.postMessage({type:'tabOpen', tabId: options.id}, '*'); },
        close: () => { parent.postMessage({type:'tabRemoved', tabId: options.id}, '*'); }
      };
      parent.postMessage({type:'tab', tab: options}, '*');
      return tab;
    };
    const createFileBrowser = () => ({
      readText: path => rpc('filebrowser.readText', [path]),
      writeText: (path, content, options = {}) => rpc('filebrowser.writeText', [{path, content, expectedRevision: options.expectedRevision}]),
      editors: {
        register: (editor, handler) => {
          fileEditorHandlers.set(editor.id, handler);
          parent.postMessage({type:'fileEditor', editor}, '*');
          return async () => {
            fileEditorHandlers.delete(editor.id);
            parent.postMessage({type:'fileEditorRemoved', id:editor.id}, '*');
          };
        }
      }
    });
    const createUI = () => ({
      windows: {
        open: options => rpc('ui.window.open', [options]).then(createUIWindow)
      },
      tabs: {
        register: options => createUITab(options),
        onAction: handler => {
          uiTabActionHandlers.push(handler);
          return () => {
            const index = uiTabActionHandlers.indexOf(handler);
            if (index >= 0) uiTabActionHandlers.splice(index, 1);
          };
        },
        onClosed: handler => {
          uiTabClosedHandlers.push(handler);
          return () => {
            const index = uiTabClosedHandlers.indexOf(handler);
            if (index >= 0) uiTabClosedHandlers.splice(index, 1);
          };
        }
      },
      onAction: handler => {
        uiActionHandlers.push(handler);
        parent.postMessage({type:'uiActionSubscribe'}, '*');
        return () => {
          const index = uiActionHandlers.indexOf(handler);
          if (index >= 0) uiActionHandlers.splice(index, 1);
        };
      },
      onClosed: handler => {
        uiClosedHandlers.push(handler);
        return () => {
          const index = uiClosedHandlers.indexOf(handler);
          if (index >= 0) uiClosedHandlers.splice(index, 1);
        };
      }
    });
    const createMonaco = () => ({
      languages: {
        register: language => rpc('monaco.language.register', [language]).then(() => async () => rpc('monaco.language.unregister', [language.id]))
      },
      diagnostics: {
        register: (language, handler) => {
          monacoDiagnostics.set(language, handler);
          parent.postMessage({type:'monacoProvider', kind:'diagnostics', language}, '*');
          return async () => { monacoDiagnostics.delete(language); parent.postMessage({type:'monacoProviderRemoved', kind:'diagnostics', language}, '*'); };
        }
      },
      completions: {
        register: (language, handler) => {
          monacoCompletions.set(language, handler);
          parent.postMessage({type:'monacoProvider', kind:'completions', language}, '*');
          return async () => { monacoCompletions.delete(language); parent.postMessage({type:'monacoProviderRemoved', kind:'completions', language}, '*'); };
        }
      }
    });
    const createApi = id => ({
      id,
      events: {
        on: (name, fn) => {
          const list = listeners.get(name) || [];
          list.push(fn);
          listeners.set(name, list);
          parent.postMessage({type:'subscribe', name}, '*');
          return () => {
            const next = (listeners.get(name) || []).filter(item => item !== fn);
            listeners.set(name, next);
          };
        }
      },
      commands: {
        register: (command, handler) => {
          commandHandlers.set(command.id, handler);
          parent.postMessage({type:'command', command}, '*');
          return () => {
            commandHandlers.delete(command.id);
            parent.postMessage({type:'commandRemoved', id:command.id}, '*');
          };
        }
      },
      panels: {register: panel => { parent.postMessage({type:'panel', panel}, '*'); return () => {}; }},
      storage: {get: key => rpc('storage.get', [key]), set: (key, value) => rpc('storage.set', [key, value]), delete: key => rpc('storage.delete', [key])},
      secrets: {get: key => rpc('secrets.get', [key]), set: (key, value) => rpc('secrets.set', [key, value]), delete: key => rpc('secrets.delete', [key])},
      network: {request: request => rpc('network.request', [request])},
      sockets: {connect: request => rpc('sockets.open', [request]).then(createSocket)},
      plugins: {
        list: () => rpc('plugins.list', []),
        on: (channel, fn) => {
          const list = pluginHandlers.get(channel) || [];
          list.push(fn);
          pluginHandlers.set(channel, list);
          parent.postMessage({type:'pluginSubscribe', channel}, '*');
          return () => {
            const next = (pluginHandlers.get(channel) || []).filter(item => item !== fn);
            pluginHandlers.set(channel, next);
            if (next.length === 0) parent.postMessage({type:'pluginUnsubscribe', channel}, '*');
          };
        },
        send: (targetId, channel, data) => rpc('plugins.send', [targetId, channel, data]),
        call: (targetId, method, data) => rpc('plugins.call', [targetId, method, data]),
        expose: (method, handler) => {
          pluginRpcHandlers.set(method, handler);
          parent.postMessage({type:'pluginExpose', method}, '*');
          return () => { pluginRpcHandlers.delete(method); parent.postMessage({type:'pluginExposeRemoved', method}, '*'); };
        }
      },
      express: {listen: () => rpc('express.listen', []).then(createExpressServer)},
      fileBrowser: createFileBrowser(),
      ui: createUI(),
      notifications: {
        show: notification => {
          if (!notification || typeof notification.message !== 'string' || !notification.message.trim()) return;
          parent.postMessage({type:'notification', notification}, '*');
        },
        info: (message, title) => parent.postMessage({type:'notification', notification:{message, title, level:'info'}}, '*'),
        success: (message, title) => parent.postMessage({type:'notification', notification:{message, title, level:'success'}}, '*'),
        warning: (message, title) => parent.postMessage({type:'notification', notification:{message, title, level:'warning'}}, '*'),
        error: (message, title) => parent.postMessage({type:'notification', notification:{message, title, level:'error'}}, '*')
      },
      monaco: createMonaco(),
      nc: {
        readWeapon: name => rpc('nc.readWeapon', [name]),
        readClass: name => rpc('nc.readClass', [name]),
        readNPC: id => rpc('nc.readNPC', [id]),
        readNPCFlags: id => rpc('nc.readNPCFlags', [id]),
        readNPCAttributes: id => rpc('nc.readNPCAttributes', [id]),
        list: () => rpc('nc.list', []),
        saveWeapon: (name, script) => rpc('nc.saveWeapon', [name, script]),
        saveClass: (name, script) => rpc('nc.saveClass', [name, script]),
        saveNPC: (id, script) => rpc('nc.saveNPC', [id, script]),
        saveNPCFlags: (id, flags) => rpc('nc.saveNPCFlags', [id, flags]),
        createWeapon: name => rpc('nc.createWeapon', [name]),
        deleteWeapon: name => rpc('nc.deleteWeapon', [name]),
        createClass: name => rpc('nc.createClass', [name]),
        deleteClass: name => rpc('nc.deleteClass', [name]),
        createNPC: options => rpc('nc.createNPC', [options]),
        deleteNPC: id => rpc('nc.deleteNPC', [id]),
        resetNPC: id => rpc('nc.resetNPC', [id])
      },
      automation: createAutomation(),
      actions: {
        pm: {send: (playerId, message) => rpc('actions.pm.send', [playerId, message])},
        admin: {send: (playerId, message) => rpc('actions.admin.send', [playerId, message])},
        rc: {execute: message => rpc('actions.rc.execute', [message])},
        nc: {
          saveWeapon: (name, script) => rpc('actions.nc.saveWeapon', [name, script]),
          saveClass: (name, script) => rpc('actions.nc.saveClass', [name, script]),
          saveNPC: (id, script) => rpc('actions.nc.saveNPC', [id, script])
        }
      }
    });
    const initialize = async event => {
      if (initializing || activeLifecycle) return;
      initializing = true;
      api = createApi(event.data.id);
      try {
        pluginDefinition = undefined;
        pluginInstance = undefined;
        const module = {exports: {}};
        new Function('api', 'Plugin', 'registerGorcPlugin', 'module', 'exports', event.data.bundle)(api, Plugin, globalThis.registerGorcPlugin, module, module.exports);
        const exported = module.exports && Object.prototype.hasOwnProperty.call(module.exports, 'default') ? module.exports.default : module.exports;
        if (!pluginInstance && typeof exported === 'function') pluginInstance = new exported();
        if (!pluginInstance && exported && typeof exported === 'object' && typeof exported.onLoad === 'function') pluginDefinition = exported;
        activeLifecycle = pluginInstance || pluginDefinition;
        if (!activeLifecycle) throw new Error('Plugin bundle must instantiate a Plugin subclass or call registerGorcPlugin');
        if (pluginInstance) pluginInstance.__attach(api);
        await Promise.resolve(activeLifecycle.onLoad?.(api));
        await Promise.resolve(activeLifecycle.onStart?.());
        parent.postMessage({type:'ready'}, '*');
      } catch (error) {
        activeLifecycle = undefined;
        reportError(error);
      } finally {
        initializing = false;
      }
    };
    const shutdown = async (event) => {
      let failure;
      if (!activeLifecycle || lifecycleStopped) {
        parent.postMessage({type:'lifecycleResult', requestId:event.data.requestId, ok:true}, '*');
        return;
      }
      lifecycleStopped = true;
      try { await Promise.resolve(activeLifecycle.onStop?.()); }
      catch (error) { failure = error; reportError(error); }
      try { await Promise.resolve(activeLifecycle.onUnload?.()); }
      catch (error) { failure = failure || error; reportError(error); }
      automationCleanups.forEach(cancel => cancel());
      automationCleanups.clear();
      fileEditorHandlers.clear();
      pluginRpcHandlers.clear();
      uiActionHandlers.length = 0;
      uiClosedHandlers.length = 0;
      uiTabActionHandlers.length = 0;
      uiTabClosedHandlers.length = 0;
      parent.postMessage({type:'lifecycleResult', requestId:event.data.requestId, ok:!failure, error:failure ? String(failure) : undefined}, '*');
    };
    window.addEventListener('message', event => {
      if (event.data?.type === 'event') {
        (listeners.get(event.data.name) || []).forEach(fn => Promise.resolve(fn(...event.data.data)).catch(reportError));
      }
      if (event.data?.type === 'commandInvoke') {
        const handler = commandHandlers.get(event.data.id);
        if (typeof handler === 'function') Promise.resolve(handler(Array.isArray(event.data.args) ? event.data.args : [])).catch(reportError);
      }
      if (event.data?.type === 'pluginMessage') {
        (pluginHandlers.get(event.data.channel) || []).forEach(fn => Promise.resolve(fn({from:event.data.from, channel:event.data.channel, data:event.data.data})).catch(reportError));
      }
      if (event.data?.type === 'fileOpen') {
        const handler = fileEditorHandlers.get(event.data.editorId);
        if (typeof handler !== 'function') {
          parent.postMessage({type:'fileOpenResult', requestId:event.data.requestId, handled:false}, '*');
        } else {
          Promise.resolve(handler(event.data.file)).then(result => {
            const handled = result === true || (result && result.handled === true);
            parent.postMessage({type:'fileOpenResult', requestId:event.data.requestId, handled:!!handled}, '*');
          }).catch(error => { reportError(error); parent.postMessage({type:'fileOpenResult', requestId:event.data.requestId, handled:false}, '*'); });
        }
      }
      if (event.data?.type === 'pluginRequest') {
        const handler = pluginRpcHandlers.get(event.data.method);
        if (typeof handler !== 'function') {
          parent.postMessage({type:'pluginResponse', requestId:event.data.requestId, ok:false, error:'plugin method not found'}, '*');
        } else {
          Promise.resolve(handler(event.data.data)).then(value => parent.postMessage({type:'pluginResponse', requestId:event.data.requestId, ok:true, value}, '*')).catch(error => parent.postMessage({type:'pluginResponse', requestId:event.data.requestId, ok:false, error:error instanceof Error ? error.message : String(error)}, '*'));
        }
      }
      if (event.data?.type === 'uiAction') {
        uiActionHandlers.forEach(fn => Promise.resolve(fn({windowId:event.data.windowId, action:event.data.action, value:event.data.value})).catch(reportError));
      }
      if (event.data?.type === 'uiClosed') {
        uiClosedHandlers.forEach(fn => Promise.resolve(fn(event.data.windowId)).catch(reportError));
      }
      if (event.data?.type === 'tabAction') {
        uiTabActionHandlers.forEach(fn => Promise.resolve(fn({tabId:event.data.tabId, action:event.data.action, value:event.data.value})).catch(reportError));
      }
      if (event.data?.type === 'tabClosed') {
        uiTabClosedHandlers.forEach(fn => Promise.resolve(fn(event.data.tabId)).catch(reportError));
      }
      if (event.data?.type === 'monacoRequest') {
        const handlers = event.data.kind === 'diagnostics' ? monacoDiagnostics : monacoCompletions;
        const handler = handlers.get(event.data.language);
        if (typeof handler !== 'function') {
          parent.postMessage({type:'monacoResponse', requestId:event.data.requestId, ok:true, value:[]}, '*');
        } else {
          Promise.resolve(handler(event.data.context)).then(value => parent.postMessage({type:'monacoResponse', requestId:event.data.requestId, ok:true, value}, '*')).catch(error => parent.postMessage({type:'monacoResponse', requestId:event.data.requestId, ok:false, error:error instanceof Error ? error.message : String(error)}, '*'));
        }
      }
      if (event.data?.type === 'socketEvent') emitSocket(event.data.socketId, event.data.event);
      if (event.data?.type === 'httpRequest') {
        const request = event.data.request;
        const handler = expressRoutes.get(request.routeId);
        if (typeof handler !== 'function') {
          parent.postMessage({type:'httpResponse', requestId:request.requestId, status:404, headers:{}, body:'route not found'}, '*');
        } else {
          const response = createExpressResponse(request.requestId);
          Promise.resolve(handler(createExpressRequest(request), response)).then(() => response.end()).catch(error => response.status(500).text(error instanceof Error ? error.message : String(error)).end());
        }
      }
      if (event.data?.type === 'init') void initialize(event);
      if (event.data?.type === 'lifecycle') void shutdown(event);
    });
  <\/script></body></html>`
}
