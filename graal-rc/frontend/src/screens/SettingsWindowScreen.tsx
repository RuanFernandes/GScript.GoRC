// SettingsWindowScreen is the content of the external Settings window (opened
// via App.OpenSettings, URL "/#settings"). Two sections: Coding (Monaco theme,
// font family, font size) and Chat (the existing color/log settings via
// ChatSettingsFields). Both persist to localStorage.
import {useCallback, useEffect, useRef, useState} from "react"
import type {ReactNode} from "react"
import {Events} from "@wailsio/runtime"
import {BookOpen, Check, ChevronDown, Code2, Copy, FolderDown, FolderOpen, Languages, ListTree, MessageSquareText, MousePointerClick, Package, PanelsTopLeft, Puzzle, Radio, RefreshCw, Server, ShieldCheck, Terminal, Waypoints} from "lucide-react"

import {Button} from "@/components/ui/button"
import {Input} from "@/components/ui/input"
import {Label} from "@/components/ui/label"
import {ThemeSelect} from "@/components/features/settings/ThemeSelect"
import {Tabs, TabsContent, TabsList, TabsTrigger} from "@/components/ui/tabs"
import {ChatSettingsFields} from "@/components/features/chat/ChatSettingsFields"
import {useChatSettings} from "@/hooks/useChatSettings"
import {useCodingSettings} from "@/hooks/useCodingSettings"
import {rcService} from "@/services/rcService"
import type {ChatSettings, FileBrowserConfig} from "@/types"
import {useLanguage, type Language} from "@/hooks/useLanguage"
import {CustomThemeDialog, NewThemeButton, ThemePreview} from "@/components/features/settings/ThemePreview"
import type {CustomTheme} from "@/types"

export function SettingsWindowScreen() {
  const coding = useCodingSettings()
  const chat = useChatSettings()
  const {language, setLanguage, t} = useLanguage()

  return (
    <div className="bg-background flex h-svh flex-col">
      <Tabs defaultValue="coding" orientation="vertical" className="flex min-h-0 flex-1 flex-row gap-0">
        <TabsList aria-label="Settings sections" className="h-auto w-48 shrink-0 flex-col items-stretch justify-start gap-1 rounded-none border-b-0 border-r bg-muted/20 p-3">
          <TabsTrigger value="coding" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <Code2 />{t("settings.coding")}
          </TabsTrigger>
          <TabsTrigger value="chat" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <MessageSquareText />{t("settings.chat")}
          </TabsTrigger>
          <TabsTrigger value="files" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <FolderDown />{t("settings.files")}
          </TabsTrigger>
          <TabsTrigger value="language" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <Languages />{t("settings.language")}
          </TabsTrigger>
          <TabsTrigger value="plugins" className="justify-start border-b-0 border-l-2 border-transparent px-3 data-[state=active]:border-primary data-[state=active]:bg-accent">
            <Puzzle />{t("settings.plugins")}
          </TabsTrigger>
        </TabsList>
        <TabsContent value="coding" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <CodingSection
            theme={coding.settings.theme}
            fontFamily={coding.settings.fontFamily}
            fontSize={coding.settings.fontSize}
            onChange={coding.update}
            onReset={coding.reset}
            t={t}
          />
        </TabsContent>
        <TabsContent value="chat" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <ChatSection
            settings={chat.settings}
            onChange={chat.update}
            onReset={chat.reset}
            t={t}
          />
        </TabsContent>
        <TabsContent value="files" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <FilesSection t={t} />
        </TabsContent>
        <TabsContent value="language" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <LanguageSection language={language} onChange={setLanguage} t={t} />
        </TabsContent>
        <TabsContent value="plugins" className="mt-0 min-h-0 flex-1 overflow-y-auto p-5 sm:p-6">
          <PluginsSection t={t} />
        </TabsContent>
      </Tabs>
    </div>
  )
}

function PluginsSection({t}: {t: (key: string) => string}) {
  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.plugins")} description={t("settings.pluginsDescription")} />
      <div className="overflow-hidden rounded-lg border bg-card/40">
        <div className="grid gap-4 p-6">
          <div className="flex h-12 w-12 items-center justify-center rounded-lg border bg-primary/10 text-primary"><Puzzle /></div>
          <div>
            <h3 className="text-base font-medium">{t("settings.pluginWorkspaceTitle")}</h3>
            <p className="text-muted-foreground mt-1 max-w-xl text-sm">{t("settings.pluginWorkspaceDescription")}</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button onClick={() => void rcService.openPluginManager()}><Puzzle />{t("settings.openPluginManager")}</Button>
            <Button variant="outline" onClick={() => void rcService.openPluginDocumentation()}><BookOpen />{t("settings.pluginDocumentation")}</Button>
          </div>
        </div>
      </div>
    </div>
  )
}

type DocumentationCopy = {
  overviewTitle: string; overview: string; overviewNote: string
  installationTitle: string; installation: string; installationNote: string
  gettingStartedTitle: string; gettingStarted: string; gettingStartedSteps: string[]
  manifestTitle: string; manifest: string; manifestNote: string
  sdkTitle: string; sdk: string; sdkNote: string
  commandsTitle: string; commands: string; panels: string
  eventsTitle: string; securityTitle: string; security: string; securityNote: string
  dataTitle: string; data: string
  socketsTitle: string; sockets: string
  internalApiTitle: string; internalApi: string
  tutorialTitle: string
  troubleshootingTitle: string; events: [string, string][]; permissions: [string, string][]; troubleshooting: [string, string][]
}

const documentationCopy: Record<Language, DocumentationCopy> = {
  "pt-BR": {
    overviewTitle: "Visão geral", overview: "Plugins são extensões locais do GoRC escritas em TypeScript e compiladas para um bundle JavaScript. Eles podem observar eventos do RC, criar comandos e painéis, guardar configurações e conversar com serviços externos através das APIs aprovadas.", overviewNote: "O plugin não recebe acesso direto aos bindings Wails, às credenciais da conta ou ao filesystem do computador.",
    installationTitle: "Instalação", installation: "O GoRC procura plugins no diretório exibido na aba Plugins instalados. Cada plugin precisa ser uma pasta com este formato:", installationNote: "Copie a pasta para o diretório indicado e clique em Atualizar. O plugin deve conter um manifest.json válido; pastas sem manifest são ignoradas.",
    gettingStartedTitle: "Começando pelo primeiro plugin", gettingStarted: "O caminho mais curto é criar o template dentro do workspace, editar src/index.ts, compilar e só então aprovar o plugin. O bundle executado é dist/index.js; o TypeScript é apenas a fonte editável.", gettingStartedSteps: ["Abra a área de Plugins e clique em Novo plugin.", "Edite manifest.json e declare apenas as capacidades necessárias.", "Escreva a classe em src/index.ts, usando Plugin como base.", "Clique em Compilar, revise o log e depois ative o plugin.", "Teste comandos pelo chat e eventos pela conexão do RC."],
    manifestTitle: "Manifest", manifest: "O manifest identifica o plugin, aponta para o bundle e declara tudo que ele pretende usar. A versão atual da API é 1.", manifestNote: "id usa formato reverse-domain. main deve ser um caminho relativo para um arquivo JavaScript dentro da pasta do plugin. Ações e hosts não declarados nunca são liberados pelo host.",
    sdkTitle: "SDK e ciclo de vida", sdk: "Estenda a classe base Plugin no index.ts. O GoRC instancia a classe e chama onLoad, onStart, onStop e onUnload. Registre listeners, comandos e painéis usando this.events, this.commands e this.panels. Comandos ficam disponíveis como /id-do-comando no chat e o callback recebe os argumentos como string[].", sdkNote: "O runtime isola cada plugin em um iframe com sandbox e usa mensagens RPC para falar com o host. Uma falha em um plugin não deve derrubar o RC.",
    commandsTitle: "Comandos e UI oficial", commands: "Comandos são a forma mais simples de executar uma função a partir do RC. O id é o nome técnico usado no chat; label é o texto mostrado no autocomplete. O callback recebe apenas os argumentos depois do comando.", panels: "Painéis são registrados como slots declarativos. O plugin pode fornecer título e HTML controlado pelo host, mas não recebe acesso ao DOM do RC. Para uma ação executável, registre um comando e conecte-o a um slot oficial quando esse slot estiver disponível; não use onclick, querySelector ou document do RC.",
    eventsTitle: "Eventos disponíveis", securityTitle: "Permissões e segurança", security: "Permissões são separadas em eventos, APIs e hosts de rede. Na lista de plugins, clique em Aprovar permissões somente depois de revisar o manifest.", securityNote: "Requests de rede têm timeout e limite de resposta. Headers Authorization e Cookie são bloqueados pelo host. O sandbox é uma barreira de capacidades para plugins locais confiáveis, não uma garantia contra código malicioso no nível do sistema operacional.",
    dataTitle: "Dados, storage e requests", data: "Use storage para configurações comuns e secrets para tokens. Use network.request para HTTPS aprovado. O host serializa objetos JSON, limita respostas e bloqueia headers sensíveis; nenhum desses recursos dá acesso direto ao filesystem.",
    socketsTitle: "WebSockets e mensagens entre plugins", sockets: "Sockets usam somente WSS e origins declarados no manifest. Mensagens entre plugins usam channels e uma allowlist de IDs. O remetente e o destinatário precisam estar ativos e com plugins.messaging aprovado.",
    internalApiTitle: "APIs HTTP internas no estilo Express", internalApi: "express.listen cria uma API local hospedada pelo GoRC. Ela não instala Node.js, não abre uma porta pública e não executa o Express do npm. Cada request chega ao plugin pelo bridge e deve ser respondido pelo objeto response.",
    tutorialTitle: "Tutorial: criando seu primeiro plugin",
    troubleshootingTitle: "Troubleshooting",
    events: [["rc.connected", "Conexão com o servidor estabelecida."], ["rc.disconnected", "Servidor desconectado e motivo."], ["rc.message", "Mensagem recebida no chat principal."], ["irc.message", "Mensagem recebida em um canal IRC."], ["pm.received", "PM recebido: playerId, conta, nick e texto."], ["pm.sent", "PM enviado pelo RC ou por um plugin autorizado."], ["script.received", "Script recebido do NC."], ["script.opened", "Script aberto pelo host ou por um plugin."], ["script.saved", "Script salvo com sucesso."], ["script.conflict", "O sync detectou conflito entre cópias."], ["weapon.changed", "Weapon adicionada ou removida."], ["class.changed", "Class adicionada ou removida."], ["npc.changed", "NPC adicionado ou removido."], ["npc.flags", "Flags de NPC recebidas."], ["npc.attributes", "Attributes de NPC recebidos."], ["filebrowser.file.opening", "Arquivo prestes a abrir; um editor registrado pode assumir."], ["filebrowser.file.saved", "Arquivo remoto salvo."], ["filebrowser.*", "Atualizações do file browser."], ["player.*", "Dados administrativos recebidos."]],
    permissions: [["pm.send", "Envia uma mensagem privada para um player."], ["admin.send", "Envia uma mensagem administrativa."], ["rc.execute", "Executa uma mensagem/comando no RC."], ["nc.read* / nc.list", "Lê scripts e o índice do NC."], ["nc.save* / nc.create* / nc.delete*", "Altera scripts e entidades do NC."], ["filebrowser.read/write", "Lê ou salva arquivos remotos dentro dos globs aprovados."], ["filebrowser.editor", "Registra editores visuais para extensões remotas."], ["ui.window", "Abre janelas declarativas do plugin."], ["ui.notification", "Mostra notificações controladas no RC."], ["monaco", "Registra linguagens, diagnósticos e completions."], ["network.http", "Permite requests HTTPS para hosts aprovados."], ["network.socket", "Permite conexões WebSocket WSS para origins aprovados."], ["plugins.messaging", "Permite listar plugins e enviar mensagens para destinos aprovados."], ["express.http", "Permite registrar APIs HTTP locais no servidor do GoRC."]],
    troubleshooting: [["Plugin não aparece", "Confirme o diretório, manifest.json e clique em Atualizar."], ["Status invalid", "Leia o erro exibido no plugin; normalmente é apiVersion, id ou main inválido."], ["Evento não chega", "Aprove a permissão do evento e confirme que o plugin está Ativo."], ["Request negado", "Declare network.http e o origin HTTPS exato no manifest, depois aprove as permissões."], ["Alteração não aparece", "Recompile dist/index.js, desative/ative o plugin e atualize a descoberta."]],
  },
  en: {
    overviewTitle: "Overview", overview: "Plugins are local GoRC extensions written in TypeScript and compiled into a JavaScript bundle. They can observe RC events, create commands and panels, store configuration, and communicate with external services through approved APIs.", overviewNote: "A plugin does not receive direct access to Wails bindings, account credentials, or the computer filesystem.",
    installationTitle: "Installation", installation: "GoRC scans the directory shown in the Installed plugins tab. Each plugin must be a folder with this structure:", installationNote: "Copy the folder to the displayed directory and click Refresh. The plugin must contain a valid manifest.json; folders without a manifest are ignored.",
    gettingStartedTitle: "Start with your first plugin", gettingStarted: "The shortest path is to create the template inside the workspace, edit src/index.ts, build it, and only then approve the plugin. The executed bundle is dist/index.js; TypeScript is the editable source.", gettingStartedSteps: ["Open the Plugins workspace and select New plugin.", "Edit manifest.json and declare only the capabilities you need.", "Write the class in src/index.ts, extending Plugin.", "Select Compile, review the log, and then enable the plugin.", "Test commands through chat and events through the RC connection."],
    manifestTitle: "Manifest", manifest: "The manifest identifies the plugin, points to its bundle, and declares everything it intends to use. The current API version is 1.", manifestNote: "id uses reverse-domain format. main must be a relative path to a JavaScript file inside the plugin folder. Undeclared actions and hosts are never released by the host.",
    sdkTitle: "SDK and lifecycle", sdk: "Extend the Plugin base class in index.ts. GoRC instantiates the class and calls onLoad, onStart, onStop, and onUnload. Register listeners, commands, and panels through this.events, this.commands, and this.panels. Commands are available as /command-id in RC chat, and the callback receives the arguments as string[].", sdkNote: "The runtime isolates each plugin in a sandboxed iframe and uses RPC messages to communicate with the host. One plugin failure must not bring down RC.",
    commandsTitle: "Commands and official UI", commands: "Commands are the simplest way to execute a function from RC. The id is the technical name used in chat; label is the text shown in autocomplete. The callback receives only the arguments after the command.", panels: "Panels are registered as declarative slots. A plugin can provide a title and host-controlled HTML, but it does not receive access to RC's DOM. For an executable action, register a command and connect it to an official slot when that slot is available; do not use onclick, querySelector, or the RC document.",
    eventsTitle: "Available events", securityTitle: "Permissions and security", security: "Permissions are separated into events, APIs, and network hosts. In the plugin list, click Approve permissions only after reviewing the manifest.", securityNote: "Network requests have a timeout and response-size limit. Authorization and Cookie headers are blocked by the host. The sandbox is a capability barrier for trusted local plugins, not a guarantee against OS-level malicious code.",
    dataTitle: "Data, storage, and requests", data: "Use storage for regular configuration and secrets for tokens. Use network.request for approved HTTPS calls. The host serializes JSON objects, limits responses, and blocks sensitive headers; none of these APIs gives direct filesystem access.",
    socketsTitle: "WebSockets and plugin messaging", sockets: "Sockets use WSS only and require origins declared in the manifest. Plugin messages use channels and an ID allowlist. Both sender and recipient must be enabled with plugins.messaging approved.",
    internalApiTitle: "Express-style internal HTTP APIs", internalApi: "express.listen creates a local API hosted by GoRC. It does not install Node.js, open a public port, or run npm Express. Each request reaches the plugin through the bridge and must be answered through the response object.",
    tutorialTitle: "Tutorial: creating your first plugin",
    troubleshootingTitle: "Troubleshooting",
    events: [["rc.connected", "The server connection was established."], ["rc.disconnected", "The server disconnected and provided a reason."], ["rc.message", "A message received in the main chat."], ["irc.message", "A message received in an IRC channel."], ["pm.received", "A PM received: playerId, account, nickname, and text."], ["pm.sent", "A PM sent by RC or an authorized plugin."], ["script.received", "A script received from NC."], ["script.opened", "A script opened by the host or a plugin."], ["script.saved", "A script was saved successfully."], ["script.conflict", "Sync detected a conflict between copies."], ["weapon.changed", "A weapon was added or removed."], ["class.changed", "A class was added or removed."], ["npc.changed", "An NPC was added or removed."], ["npc.flags", "NPC flags received."], ["npc.attributes", "NPC attributes received."], ["filebrowser.file.opening", "A file is about to open; a registered editor may take over."], ["filebrowser.file.saved", "A remote file was saved."], ["filebrowser.*", "File browser updates."], ["player.*", "Player administration data received."]],
    permissions: [["pm.send", "Send a private message to a player."], ["admin.send", "Send an administrative message."], ["rc.execute", "Execute a message or command in RC."], ["nc.read* / nc.list", "Read NC scripts and the script index."], ["nc.save* / nc.create* / nc.delete*", "Modify NC scripts and entities."], ["filebrowser.read/write", "Read or save remote files inside approved globs."], ["filebrowser.editor", "Register visual editors for remote extensions."], ["ui.window", "Open declarative plugin windows."], ["ui.notification", "Show controlled notifications in RC."], ["monaco", "Register languages, diagnostics, and completions."], ["network.http", "Allow HTTPS requests to approved hosts."], ["network.socket", "Allow WebSocket WSS connections to approved origins."], ["plugins.messaging", "List plugins and send messages to approved targets."], ["express.http", "Register local HTTP APIs in the GoRC server."]],
    troubleshooting: [["Plugin is missing", "Confirm the directory and manifest.json, then click Refresh."], ["Invalid status", "Read the error on the plugin card; it is usually an invalid apiVersion, id, or main path."], ["Event is not received", "Approve the event permission and confirm the plugin is Enabled."], ["Request denied", "Declare network.http and the exact HTTPS origin in the manifest, then approve permissions."], ["Changes are missing", "Rebuild dist/index.js, disable/enable the plugin, and refresh discovery."]],
  },
  es: {
    overviewTitle: "Descripción general", overview: "Los plugins son extensiones locales de GoRC escritas en TypeScript y compiladas como un bundle JavaScript. Pueden observar eventos del RC, crear comandos y paneles, guardar configuración y comunicarse con servicios externos mediante APIs aprobadas.", overviewNote: "El plugin no recibe acceso directo a los bindings de Wails, las credenciales de la cuenta ni el filesystem del ordenador.",
    installationTitle: "Instalación", installation: "GoRC busca plugins en el directorio mostrado en la pestaña Plugins instalados. Cada plugin debe ser una carpeta con esta estructura:", installationNote: "Copia la carpeta al directorio mostrado y pulsa Actualizar. El plugin debe contener un manifest.json válido; las carpetas sin manifest se ignoran.",
    gettingStartedTitle: "Comienza con tu primer plugin", gettingStarted: "El camino más corto es crear el template dentro del workspace, editar src/index.ts, compilarlo y solo después aprobar el plugin. El bundle ejecutado es dist/index.js; TypeScript es la fuente editable.", gettingStartedSteps: ["Abre el área de Plugins y pulsa Nuevo plugin.", "Edita manifest.json y declara solo las capacidades necesarias.", "Escribe la clase en src/index.ts extendiendo Plugin.", "Pulsa Compilar, revisa el log y activa el plugin.", "Prueba comandos desde el chat y eventos desde la conexión del RC."],
    manifestTitle: "Manifest", manifest: "El manifest identifica el plugin, apunta a su bundle y declara todo lo que pretende usar. La versión actual de la API es 1.", manifestNote: "id usa formato reverse-domain. main debe ser una ruta relativa a un archivo JavaScript dentro de la carpeta del plugin. El host nunca libera acciones ni hosts no declarados.",
    sdkTitle: "SDK y ciclo de vida", sdk: "Extiende la clase base Plugin en index.ts. GoRC instancia la clase y llama a onLoad, onStart, onStop y onUnload. Registra listeners, comandos y paneles mediante this.events, this.commands y this.panels. Los comandos están disponibles como /id-del-comando en el chat y el callback recibe los argumentos como string[].", sdkNote: "El runtime aísla cada plugin en un iframe con sandbox y usa mensajes RPC para comunicarse con el host. Un fallo de un plugin no debe cerrar el RC.",
    commandsTitle: "Comandos y UI oficial", commands: "Los comandos son la forma más simple de ejecutar una función desde RC. El id es el nombre técnico usado en el chat; label es el texto mostrado en el autocomplete. El callback recibe solo los argumentos posteriores al comando.", panels: "Los paneles se registran como slots declarativos. El plugin puede proporcionar un título y HTML controlado por el host, pero no recibe acceso al DOM del RC. Para una acción ejecutable, registra un comando y conéctalo a un slot oficial cuando esté disponible; no uses onclick, querySelector ni el document del RC.",
    eventsTitle: "Eventos disponibles", securityTitle: "Permisos y seguridad", security: "Los permisos se separan en eventos, APIs y hosts de red. En la lista de plugins, pulsa Aprobar permisos solo después de revisar el manifest.", securityNote: "Las requests de red tienen timeout y límite de respuesta. El host bloquea los headers Authorization y Cookie. El sandbox es una barrera de capacidades para plugins locales confiables, no una garantía contra código malicioso a nivel del sistema operativo.",
    dataTitle: "Datos, storage y requests", data: "Usa storage para configuraciones comunes y secrets para tokens. Usa network.request para llamadas HTTPS aprobadas. El host serializa objetos JSON, limita respuestas y bloquea headers sensibles; ninguna de estas APIs da acceso directo al filesystem.",
    socketsTitle: "WebSockets y mensajes entre plugins", sockets: "Los sockets usan solo WSS y requieren origins declarados en el manifest. Los mensajes entre plugins usan channels y una allowlist de IDs. El remitente y el destinatario deben estar activos y tener plugins.messaging aprobado.",
    internalApiTitle: "APIs HTTP internas al estilo Express", internalApi: "express.listen crea una API local hospedada por GoRC. No instala Node.js, no abre un puerto público y no ejecuta Express de npm. Cada request llega al plugin por el bridge y debe responderse con el objeto response.",
    tutorialTitle: "Tutorial: crea tu primer plugin",
    troubleshootingTitle: "Solución de problemas",
    events: [["rc.connected", "Se estableció la conexión con el servidor."], ["rc.disconnected", "El servidor se desconectó e indicó el motivo."], ["rc.message", "Mensaje recibido en el chat principal."], ["irc.message", "Mensaje recibido en un canal IRC."], ["pm.received", "PM recibido: playerId, cuenta, nick y texto."], ["pm.sent", "PM enviado por RC o por un plugin autorizado."], ["script.received", "Script recibido desde NC."], ["script.opened", "El host o un plugin abrió un script."], ["script.saved", "Un script se guardó correctamente."], ["script.conflict", "Sync detectó un conflicto entre copias."], ["weapon.changed", "Weapon añadida o eliminada."], ["class.changed", "Class añadida o eliminada."], ["npc.changed", "NPC añadido o eliminado."], ["npc.flags", "Flags de NPC recibidas."], ["npc.attributes", "Attributes de NPC recibidos."], ["filebrowser.file.opening", "Un archivo está por abrirse; un editor registrado puede asumirlo."], ["filebrowser.file.saved", "Se guardó un archivo remoto."], ["filebrowser.*", "Actualizaciones del file browser."], ["player.*", "Datos administrativos recibidos."]],
    permissions: [["pm.send", "Envía un mensaje privado a un player."], ["admin.send", "Envía un mensaje administrativo."], ["rc.execute", "Ejecuta un mensaje o comando en RC."], ["nc.read* / nc.list", "Lee scripts y el índice del NC."], ["nc.save* / nc.create* / nc.delete*", "Modifica scripts y entidades del NC."], ["filebrowser.read/write", "Lee o guarda archivos remotos dentro de globs aprobados."], ["filebrowser.editor", "Registra editores visuales para extensiones remotas."], ["ui.window", "Abre ventanas declarativas del plugin."], ["ui.notification", "Muestra notificaciones controladas en RC."], ["monaco", "Registra lenguajes, diagnósticos y completions."], ["network.http", "Permite requests HTTPS a hosts aprobados."], ["network.socket", "Permite conexiones WebSocket WSS a origins aprobados."], ["plugins.messaging", "Permite listar plugins y enviar mensajes a destinos aprobados."], ["express.http", "Permite registrar APIs HTTP locales en el servidor de GoRC."]],
    troubleshooting: [["El plugin no aparece", "Confirma el directorio y manifest.json, y pulsa Actualizar."], ["Estado invalid", "Lee el error del plugin; normalmente es apiVersion, id o main inválido."], ["El evento no llega", "Aprueba el permiso del evento y confirma que el plugin está Activo."], ["Request denegada", "Declara network.http y el origin HTTPS exacto en el manifest, y aprueba los permisos."], ["El cambio no aparece", "Vuelve a compilar dist/index.js, desactiva/activa el plugin y actualiza el descubrimiento."]],
  },
}

type AdvancedDocumentationCopy = {
  fileBrowserTitle: string
  fileBrowser: string
  uiTitle: string
  ui: string
  automationTitle: string
  automation: string
  monacoTitle: string
  monaco: string
  ncTitle: string
  nc: string
  rpcTitle: string
  rpc: string
}

const advancedDocumentationCopy: Record<Language, AdvancedDocumentationCopy> = {
  "pt-BR": {
    fileBrowserTitle: "File Browser e editores visuais", fileBrowser: "Plugins podem declarar extensões remotas, receber o evento de abertura e substituir o editor padrão. O callback recebe somente metadados; use fileBrowser.readText para buscar o conteúdo e writeText com expectedRevision para evitar sobrescrever uma edição concorrente. Os glob patterns são relativos ao servidor e nunca concedem acesso ao disco local.",
    uiTitle: "Janelas e UI declarativa", ui: "A UI oficial é serializável e renderizada pelo host em uma janela própria. Use stack, row, heading, text, input, select, code e table. Ações chegam por ui.onAction; atualize a view para refletir o novo estado. notifications.info/success/warning/error mostra feedback no RC e exige ui.notification. Não use DOM, HTML arbitrário ou bindings Wails dentro do plugin.",
    automationTitle: "Automação, timers e retry", automation: "A automação é local ao plugin e é cancelada automaticamente no unload/reload. timeout e interval retornam canceladores; debounce reduz chamadas repetidas; retry aplica backoff limitado. Evite intervalos menores que o necessário e nunca mantenha referências de timers fora da SDK.",
    monacoTitle: "Integração com Monaco", monaco: "Plugins podem registrar linguagens, diagnósticos e completions para os editores Monaco do GoRC. O host encaminha os pedidos para o sandbox correto, limita o payload e ignora falhas de providers sem impedir a edição. Use posições 1-based nos diagnósticos.",
    ncTitle: "NC, scripts e dados administrativos", nc: "A API NC separa leitura de escrita. list, readWeapon/readClass/readNPC e leitura de flags/attributes são capabilities independentes das operações de salvar, criar, remover ou resetar. Eventos script.opened, script.saved, script.received e weapon/class/npc.changed permitem manter ferramentas visuais sincronizadas.",
    rpcTitle: "RPC entre plugins e APIs internas", rpc: "plugins.expose publica uma função nomeada no sandbox do plugin e plugins.call invoca uma função de outro plugin autorizado. O alvo precisa declarar plugins.messaging e o manifest do chamador precisa listar o ID do alvo. express.listen cria uma API HTTP local controlada pelo host; use-a para integrar ferramentas locais sem abrir uma porta pública.",
  },
  en: {
    fileBrowserTitle: "File Browser and visual editors", fileBrowser: "Plugins can declare remote extensions, receive the open event, and replace the default editor. The callback receives metadata only; use fileBrowser.readText to fetch content and writeText with expectedRevision to avoid overwriting a concurrent edit. Glob patterns are relative to the server and never grant local disk access.",
    uiTitle: "Windows and declarative UI", ui: "The official UI is serializable and rendered by the host in its own window. Use stack, row, heading, text, input, select, code, and table. Actions arrive through ui.onAction; update the view to reflect state. notifications.info/success/warning/error provides RC feedback and requires ui.notification. Do not use DOM, arbitrary HTML, or Wails bindings inside a plugin.",
    automationTitle: "Automation, timers, and retry", automation: "Automation is local to the plugin and is cancelled automatically on unload/reload. timeout and interval return cancellers; debounce reduces repeated calls; retry applies bounded backoff. Avoid unnecessarily short intervals and never keep timer references outside the SDK.",
    monacoTitle: "Monaco integration", monaco: "Plugins can register languages, diagnostics, and completions for GoRC Monaco editors. The host forwards requests to the correct sandbox, limits payloads, and ignores provider failures without preventing editing. Use 1-based positions for diagnostics.",
    ncTitle: "NC, scripts, and administrative data", nc: "The NC API separates reads from writes. list, readWeapon/readClass/readNPC, and flags/attributes reads are independent capabilities from save, create, delete, and reset operations. script.opened, script.saved, script.received, and weapon/class/npc.changed events let visual tools stay synchronized.",
    rpcTitle: "Plugin RPC and internal APIs", rpc: "plugins.expose publishes a named function inside a plugin sandbox and plugins.call invokes a function in another approved plugin. The target must declare plugins.messaging and the caller manifest must list the target ID. express.listen creates a host-controlled local HTTP API for local tooling without opening a public port.",
  },
  es: {
    fileBrowserTitle: "File Browser y editores visuales", fileBrowser: "Los plugins pueden declarar extensiones remotas, recibir el evento de apertura y reemplazar el editor predeterminado. El callback recibe solo metadatos; usa fileBrowser.readText para leer y writeText con expectedRevision para no sobrescribir una edición concurrente. Los patrones glob son relativos al servidor y nunca dan acceso al disco local.",
    uiTitle: "Ventanas y UI declarativa", ui: "La UI oficial es serializable y el host la renderiza en su propia ventana. Usa stack, row, heading, text, input, select, code y table. Las acciones llegan por ui.onAction; actualiza la view para reflejar el estado. notifications.info/success/warning/error muestra feedback en RC y requiere ui.notification. No uses DOM, HTML arbitrario ni bindings Wails dentro del plugin.",
    automationTitle: "Automatización, timers y retry", automation: "La automatización pertenece al plugin y se cancela automáticamente en unload/reload. timeout e interval devuelven canceladores; debounce reduce llamadas repetidas; retry aplica backoff limitado. Evita intervalos innecesariamente cortos y no guardes referencias de timers fuera de la SDK.",
    monacoTitle: "Integración con Monaco", monaco: "Los plugins pueden registrar lenguajes, diagnósticos y completions para los editores Monaco de GoRC. El host reenvía las solicitudes al sandbox correcto, limita el payload e ignora fallos de providers sin impedir la edición. Usa posiciones 1-based en los diagnósticos.",
    ncTitle: "NC, scripts y datos administrativos", nc: "La API NC separa lectura y escritura. list, readWeapon/readClass/readNPC y la lectura de flags/attributes son capabilities independientes de guardar, crear, eliminar o resetear. Los eventos script.opened, script.saved, script.received y weapon/class/npc.changed permiten mantener sincronizadas las herramientas visuales.",
    rpcTitle: "RPC entre plugins y APIs internas", rpc: "plugins.expose publica una función dentro del sandbox y plugins.call invoca una función de otro plugin autorizado. El destino debe declarar plugins.messaging y el manifest del llamador debe listar su ID. express.listen crea una API HTTP local controlada por el host para herramientas locales sin abrir un puerto público.",
  },
}

type DocumentationNavItem = {id: string; label: string}

const documentationNavigation: Record<Language, DocumentationNavItem[]> = {
  "pt-BR": [
    {id: "doc-overview", label: "Visão geral"},
    {id: "doc-installation", label: "Instalação"},
    {id: "doc-getting-started", label: "Primeiro plugin"},
    {id: "doc-manifest", label: "Manifest"},
    {id: "doc-sdk", label: "SDK e ciclo de vida"},
    {id: "doc-commands", label: "Comandos e UI"},
    {id: "doc-events", label: "Eventos"},
    {id: "doc-data", label: "Dados e rede"},
    {id: "doc-filebrowser", label: "File Browser"},
    {id: "doc-ui", label: "UI declarativa"},
    {id: "doc-automation", label: "Automação"},
    {id: "doc-monaco", label: "Monaco"},
    {id: "doc-nc", label: "NC e scripts"},
    {id: "doc-rpc", label: "RPC e APIs"},
    {id: "doc-sockets", label: "Sockets e mensagens"},
    {id: "doc-internal-api", label: "API HTTP interna"},
    {id: "doc-security", label: "Permissões e segurança"},
    {id: "doc-tutorial", label: "Tutorial completo"},
    {id: "doc-troubleshooting", label: "Troubleshooting"},
  ],
  en: [
    {id: "doc-overview", label: "Overview"},
    {id: "doc-installation", label: "Installation"},
    {id: "doc-getting-started", label: "First plugin"},
    {id: "doc-manifest", label: "Manifest"},
    {id: "doc-sdk", label: "SDK and lifecycle"},
    {id: "doc-commands", label: "Commands and UI"},
    {id: "doc-events", label: "Events"},
    {id: "doc-data", label: "Data and network"},
    {id: "doc-filebrowser", label: "File Browser"},
    {id: "doc-ui", label: "Declarative UI"},
    {id: "doc-automation", label: "Automation"},
    {id: "doc-monaco", label: "Monaco"},
    {id: "doc-nc", label: "NC and scripts"},
    {id: "doc-rpc", label: "RPC and APIs"},
    {id: "doc-sockets", label: "Sockets and messaging"},
    {id: "doc-internal-api", label: "Internal HTTP API"},
    {id: "doc-security", label: "Permissions and security"},
    {id: "doc-tutorial", label: "Complete tutorial"},
    {id: "doc-troubleshooting", label: "Troubleshooting"},
  ],
  es: [
    {id: "doc-overview", label: "Descripción general"},
    {id: "doc-installation", label: "Instalación"},
    {id: "doc-getting-started", label: "Primer plugin"},
    {id: "doc-manifest", label: "Manifest"},
    {id: "doc-sdk", label: "SDK y ciclo de vida"},
    {id: "doc-commands", label: "Comandos y UI"},
    {id: "doc-events", label: "Eventos"},
    {id: "doc-data", label: "Datos y red"},
    {id: "doc-filebrowser", label: "File Browser"},
    {id: "doc-ui", label: "UI declarativa"},
    {id: "doc-automation", label: "Automatización"},
    {id: "doc-monaco", label: "Monaco"},
    {id: "doc-nc", label: "NC y scripts"},
    {id: "doc-rpc", label: "RPC y APIs"},
    {id: "doc-sockets", label: "Sockets y mensajes"},
    {id: "doc-internal-api", label: "API HTTP interna"},
    {id: "doc-security", label: "Permisos y seguridad"},
    {id: "doc-tutorial", label: "Tutorial completo"},
    {id: "doc-troubleshooting", label: "Solución de problemas"},
  ],
}

const manifestFieldDescriptions: Record<Language, [string, string][]> = {
  "pt-BR": [
    ["id", "Identificador único em formato reverse-domain, por exemplo com.example.tools."],
    ["name", "Nome exibido na lista de plugins e nos logs."],
    ["version", "Versão do plugin, preferencialmente semver."],
    ["apiVersion", "Versão da API do GoRC. Plugins incompatíveis são rejeitados."],
    ["main", "Bundle JavaScript relativo, normalmente dist/index.js."],
    ["permissions.events", "Eventos que o plugin quer receber."],
    ["permissions.apis", "Capacidades de leitura ou escrita que serão chamadas pelo plugin."],
    ["permissions.network", "Origins HTTPS/WSS exatos aprovados para requests e sockets."],
    ["permissions.plugins", "IDs de plugins que podem receber mensagens deste plugin."],
    ["permissions.files.read/write", "Glob patterns de arquivos remotos permitidos para leitura e escrita."],
  ],
  en: [
    ["id", "Unique reverse-domain identifier, for example com.example.tools."],
    ["name", "Name shown in the plugin list and logs."],
    ["version", "Plugin version, preferably semver."],
    ["apiVersion", "GoRC API version. Incompatible plugins are rejected."],
    ["main", "Relative JavaScript bundle, usually dist/index.js."],
    ["permissions.events", "Events the plugin wants to receive."],
    ["permissions.apis", "Read or write capabilities the plugin may call."],
    ["permissions.network", "Exact approved HTTPS/WSS origins for requests and sockets."],
    ["permissions.plugins", "Plugin IDs that may receive messages from this plugin."],
    ["permissions.files.read/write", "Glob patterns for remote files allowed for reading and writing."],
  ],
  es: [
    ["id", "Identificador único reverse-domain, por ejemplo com.example.tools."],
    ["name", "Nombre mostrado en la lista de plugins y en los logs."],
    ["version", "Versión del plugin, preferiblemente semver."],
    ["apiVersion", "Versión de la API de GoRC. Los plugins incompatibles se rechazan."],
    ["main", "Bundle JavaScript relativo, normalmente dist/index.js."],
    ["permissions.events", "Eventos que el plugin quiere recibir."],
    ["permissions.apis", "Capacidades de lectura o escritura que puede llamar el plugin."],
    ["permissions.network", "Origins HTTPS/WSS exactos aprobados para requests y sockets."],
    ["permissions.plugins", "IDs de plugins que pueden recibir mensajes de este plugin."],
    ["permissions.files.read/write", "Patrones glob de archivos remotos permitidos para lectura y escritura."],
  ],
}

export function PluginDocumentation({language}: {language: Language}) {
  const copy = documentationCopy[language]
  const advanced = advancedDocumentationCopy[language]
  const navigation = documentationNavigation[language]
  const manifestHeaders: [string, string] = language === "en" ? ["Field", "Description"] : language === "es" ? ["Campo", "Descripción"] : ["Campo", "Descrição"]
  const contentRef = useRef<HTMLDivElement>(null)
  const [activeSection, setActiveSection] = useState(navigation[0].id)

  useEffect(() => {
    const content = contentRef.current
    if (!content) return
    const sections = navigation
      .map(item => document.getElementById(item.id))
      .filter((section): section is HTMLElement => Boolean(section))
    const scroller = content.closest("[data-plugin-doc-scroll]")
    const updateActiveSection = () => {
      const threshold = 170
      let current = sections[0]?.id ?? navigation[0].id
      for (const section of sections) {
        if (section.getBoundingClientRect().top <= threshold) current = section.id
      }
      setActiveSection(current)
    }
    updateActiveSection()
    scroller?.addEventListener("scroll", updateActiveSection, {passive: true})
    window.addEventListener("resize", updateActiveSection)
    return () => {
      scroller?.removeEventListener("scroll", updateActiveSection)
      window.removeEventListener("resize", updateActiveSection)
    }
  }, [navigation, language])

  const navigateTo = (id: string) => {
    const section = document.getElementById(id)
    if (!section) return
    setActiveSection(id)
    section.scrollIntoView({behavior: window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth", block: "start"})
  }

  const navigationTitle = language === "en" ? "On this page" : language === "es" ? "En esta página" : "Nesta página"
  return (
    <div ref={contentRef} className="grid gap-5 text-sm leading-6 lg:grid-cols-[13rem_minmax(0,1fr)] lg:items-start">
      <aside className="sticky top-4 z-10 hidden max-h-[calc(100svh-2rem)] overflow-y-auto lg:block">
        <DocumentationNav title={navigationTitle} items={navigation} activeSection={activeSection} onNavigate={navigateTo} />
      </aside>

      <div className="min-w-0">
        <div className="mb-4 overflow-x-auto rounded-lg border bg-card/40 p-2 lg:hidden">
          <DocumentationNav title={navigationTitle} items={navigation} activeSection={activeSection} onNavigate={navigateTo} horizontal />
        </div>

        <div className="grid gap-4">
          <DocCard id="doc-overview" icon={<BookOpen />} title={copy.overviewTitle}>
            <p className="max-w-3xl">{copy.overview}</p>
            <p className="text-muted-foreground max-w-3xl">{copy.overviewNote}</p>
          </DocCard>

          <DocCard id="doc-installation" icon={<Package />} title={copy.installationTitle}>
            <p>{copy.installation}</p>
            <DocumentationCodeBlock language={language}>{`com.example.example-plugin/
  manifest.json
  src/
    index.ts
    plugin.d.ts
  dist/
    index.js`}</DocumentationCodeBlock>
            <p>{copy.installationNote}</p>
            <p className="text-muted-foreground">{language === "en" ? "Use the New plugin button in the plugin workspace to create the template directly in the correct directory." : language === "es" ? "Usa el botón Nuevo plugin del workspace para crear el template directamente en el directorio correcto." : "Use o botão Novo plugin na área de plugins para criar o template diretamente no diretório correto."}</p>
          </DocCard>

          <DocCard id="doc-getting-started" icon={<Waypoints />} title={copy.gettingStartedTitle}>
            <p>{copy.gettingStarted}</p>
            <ol className="grid gap-2">
              {copy.gettingStartedSteps.map((step, index) => <li key={step} className="flex gap-3 rounded-md border bg-muted/20 px-3 py-2"><span className="font-mono text-xs font-semibold text-primary">{index + 1}</span><span>{step}</span></li>)}
            </ol>
            <DocumentationCodeBlock language={language}>{tutorialCode.bundle}</DocumentationCodeBlock>
          </DocCard>

          <DocCard id="doc-manifest" icon={<Code2 />} title={copy.manifestTitle}>
            <p>{copy.manifest}</p>
            <DocumentationCodeBlock language={language}>{`{
  "id": "com.example.discord-log",
  "name": "Discord Log",
  "version": "1.0.0",
  "apiVersion": 1,
  "main": "dist/index.js",
  "description": "Sends selected events to an approved webhook.",
  "permissions": {
    "events": ["rc.message", "pm.received"],
    "apis": ["network.http", "network.socket", "plugins.messaging", "express.http"],
    "network": ["https://discord.com", "wss://gateway.example.com"],
    "plugins": ["com.example.other-plugin"],
    "files": {"read": ["levels/**/*.arc"], "write": ["levels/**/*.arc"]}
  }
}`}</DocumentationCodeBlock>
            <p>{copy.manifestNote}</p>
            <DocTable headers={manifestHeaders} rows={manifestFieldDescriptions[language]} />
          </DocCard>

          <DocCard id="doc-sdk" icon={<Terminal />} title={copy.sdkTitle}>
            <p>{copy.sdk}</p>
            <DocumentationCodeBlock language={language}>{tutorialCode.lifecycle}</DocumentationCodeBlock>
            <p className="text-muted-foreground">{copy.sdkNote}</p>
          </DocCard>

          <DocCard id="doc-commands" icon={<MousePointerClick />} title={copy.commandsTitle}>
            <p>{copy.commands}</p>
            <div className="grid gap-4 xl:grid-cols-2">
              <div className="grid min-w-0 gap-2">
                <h4 className="font-medium">{language === "en" ? "A command with a reusable callback" : language === "es" ? "Un comando con callback reutilizable" : "Um comando com callback reutilizável"}</h4>
                <DocumentationCodeBlock language={language}>{tutorialCode.commandButton}</DocumentationCodeBlock>
              </div>
              <div className="grid min-w-0 gap-2">
                <h4 className="font-medium">{language === "en" ? "A declarative panel" : language === "es" ? "Un panel declarativo" : "Um painel declarativo"}</h4>
                <DocumentationCodeBlock language={language}>{tutorialCode.panel}</DocumentationCodeBlock>
              </div>
            </div>
            <p className="text-muted-foreground">{copy.panels}</p>
          </DocCard>

          <DocCard id="doc-events" icon={<Radio />} title={copy.eventsTitle}>
            <div className="grid gap-2 sm:grid-cols-2">
              {copy.events.map(([name, description]) => (
                <div key={name} className="rounded-md border bg-muted/20 px-3 py-2">
                  <code className="text-xs text-primary">{name}</code>
                  <p className="text-muted-foreground mt-1 text-xs leading-5">{description}</p>
                </div>
              ))}
            </div>
            <DocumentationCodeBlock language={language}>{tutorialCode.eventPayload}</DocumentationCodeBlock>
          </DocCard>

          <DocCard id="doc-data" icon={<Server />} title={copy.dataTitle}>
            <p>{copy.data}</p>
            <div className="grid gap-4 xl:grid-cols-2">
              <DocumentationCodeBlock language={language}>{tutorialCode.storage}</DocumentationCodeBlock>
              <div className="grid min-w-0 gap-3">
                <DocumentationCodeBlock language={language}>{tutorialCode.networkManifest}</DocumentationCodeBlock>
                <DocumentationCodeBlock language={language}>{tutorialCode.network}</DocumentationCodeBlock>
              </div>
            </div>
          </DocCard>

          <DocCard id="doc-filebrowser" icon={<FolderOpen />} title={advanced.fileBrowserTitle}>
            <p>{advanced.fileBrowser}</p>
            <div className="grid gap-4 xl:grid-cols-2">
              <DocumentationCodeBlock language={language}>{tutorialCode.fileBrowserManifest}</DocumentationCodeBlock>
              <DocumentationCodeBlock language={language}>{tutorialCode.fileBrowser}</DocumentationCodeBlock>
            </div>
          </DocCard>

          <DocCard id="doc-ui" icon={<PanelsTopLeft />} title={advanced.uiTitle}>
            <p>{advanced.ui}</p>
            <div className="grid gap-4 xl:grid-cols-2">
              <DocumentationCodeBlock language={language}>{tutorialCode.ui}</DocumentationCodeBlock>
              <DocumentationCodeBlock language={language}>{tutorialCode.notifications}</DocumentationCodeBlock>
            </div>
          </DocCard>

          <DocCard id="doc-automation" icon={<RefreshCw />} title={advanced.automationTitle}>
            <p>{advanced.automation}</p>
            <DocumentationCodeBlock language={language}>{tutorialCode.automation}</DocumentationCodeBlock>
          </DocCard>

          <DocCard id="doc-monaco" icon={<Code2 />} title={advanced.monacoTitle}>
            <p>{advanced.monaco}</p>
            <DocumentationCodeBlock language={language}>{tutorialCode.monaco}</DocumentationCodeBlock>
          </DocCard>

          <DocCard id="doc-nc" icon={<Server />} title={advanced.ncTitle}>
            <p>{advanced.nc}</p>
            <DocumentationCodeBlock language={language}>{tutorialCode.nc}</DocumentationCodeBlock>
          </DocCard>

          <DocCard id="doc-rpc" icon={<Waypoints />} title={advanced.rpcTitle}>
            <p>{advanced.rpc}</p>
            <DocumentationCodeBlock language={language}>{tutorialCode.rpc}</DocumentationCodeBlock>
          </DocCard>

          <DocCard id="doc-sockets" icon={<Waypoints />} title={copy.socketsTitle}>
            <p>{copy.sockets}</p>
            <div className="grid gap-4 xl:grid-cols-2">
              <div className="grid min-w-0 gap-3">
                <DocumentationCodeBlock language={language}>{tutorialCode.socketsManifest}</DocumentationCodeBlock>
                <DocumentationCodeBlock language={language}>{tutorialCode.sockets}</DocumentationCodeBlock>
              </div>
              <div className="grid min-w-0 gap-3">
                <DocumentationCodeBlock language={language}>{tutorialCode.messagingManifest}</DocumentationCodeBlock>
                <DocumentationCodeBlock language={language}>{tutorialCode.messaging}</DocumentationCodeBlock>
              </div>
            </div>
          </DocCard>

          <DocCard id="doc-internal-api" icon={<Server />} title={copy.internalApiTitle}>
            <p>{copy.internalApi}</p>
            <DocumentationCodeBlock language={language}>{tutorialCode.express}</DocumentationCodeBlock>
            <p className="text-muted-foreground">{language === "en" ? "Callers must send X-GoRC-Plugin-Token with the token returned by listen(). The endpoint is local to the computer running GoRC." : language === "es" ? "Los clientes deben enviar X-GoRC-Plugin-Token con el token devuelto por listen(). El endpoint es local al ordenador que ejecuta GoRC." : "Os clientes devem enviar X-GoRC-Plugin-Token com o token retornado por listen(). O endpoint é local ao computador que executa o GoRC."}</p>
          </DocCard>

          <DocCard id="doc-security" icon={<ShieldCheck />} title={copy.securityTitle}>
            <p>{copy.security}</p>
            <div className="grid gap-2">
              {copy.permissions.map(([name, description]) => <PermissionRow key={name} name={name} description={description} />)}
            </div>
            <p className="text-muted-foreground">{copy.securityNote}</p>
          </DocCard>

          <DocCard id="doc-tutorial" icon={<ListTree />} title={copy.tutorialTitle}>
            <LocalizedPluginTutorial language={language} />
          </DocCard>

          <DocCard id="doc-troubleshooting" icon={<CircleHelpIcon />} title={copy.troubleshootingTitle}>
            <div className="grid gap-2">
              {copy.troubleshooting.map(([name, description]) => <PermissionRow key={name} name={name} description={description} />)}
            </div>
          </DocCard>
        </div>
      </div>
    </div>
  )
}

function DocumentationNav({title, items, activeSection, onNavigate, horizontal = false}: {title: string; items: DocumentationNavItem[]; activeSection: string; onNavigate: (id: string) => void; horizontal?: boolean}) {
  return (
    <nav aria-label={title} className={horizontal ? "flex min-w-max items-center gap-1" : "grid gap-1 rounded-lg border bg-card/40 p-2"}>
      {!horizontal && <div className="flex items-center gap-2 px-2 pb-2 pt-1 text-xs font-semibold"><ListTree className="size-3.5 text-primary" />{title}</div>}
      {items.map(item => (
        <button key={item.id} type="button" aria-current={activeSection === item.id ? "location" : undefined} onClick={() => onNavigate(item.id)} className={`whitespace-nowrap rounded-md px-2.5 py-1.5 text-left text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${activeSection === item.id ? "bg-accent text-foreground" : "text-muted-foreground hover:bg-muted/60 hover:text-foreground"}`}>
          {item.label}
        </button>
      ))}
    </nav>
  )
}

function CircleHelpIcon() {
  return <span className="inline-flex size-4 items-center justify-center rounded-full border text-[10px] font-semibold">?</span>
}

function DocTable({headers, rows}: {headers: [string, string]; rows: [string, string][]}) {
  return (
    <div className="overflow-hidden rounded-md border">
      <div className="grid grid-cols-[minmax(8rem,0.35fr)_minmax(0,1fr)] border-b bg-muted/30 px-3 py-2 text-xs font-medium">
        <span>{headers[0]}</span><span>{headers[1]}</span>
      </div>
      {rows.map(([name, description]) => <div key={name} className="grid grid-cols-[minmax(8rem,0.35fr)_minmax(0,1fr)] gap-3 border-b px-3 py-2 text-xs last:border-b-0"><code className="text-primary">{name}</code><span className="text-muted-foreground">{description}</span></div>)}
    </div>
  )
}

function TutorialStep({number, title, children}: {number: string; title: string; children: ReactNode}) {
  return (
    <div className="grid gap-2 border-l-2 border-primary/30 pl-4">
      <div className="flex items-baseline gap-2">
        <span className="font-mono text-xs font-semibold text-primary">{number}</span>
        <h4 className="font-medium">{title}</h4>
      </div>
      <div className="grid gap-2 text-sm">{children}</div>
    </div>
  )
}

type TutorialCopy = {title: string; paragraphs: string[]; code?: string[]}

const tutorialCode = {
  manifest: `{
  "id": "com.example.example-plugin",
  "name": "Example Plugin",
  "version": "0.1.0",
  "apiVersion": 1,
  "main": "dist/index.js",
  "description": "An example plugin for GoRC.",
  "permissions": {
    "events": ["rc.message"]
  }
}`,
  lifecycle: `export default class ExamplePlugin extends Plugin {
  onLoad() {
    console.log("Plugin loaded")
  }

  onStart() {
    console.log("Plugin started")
  }

  onStop() {
    console.log("Plugin stopping")
  }

  onUnload() {
    console.log("Plugin unloaded")
  }
}`,
  bundle: `export default class ExamplePlugin extends Plugin {
  onLoad() {
    this.events.on("rc.message", message => {
      console.log("Message received:", message)
    })

    this.commands.register({
      id: "status",
      label: "Plugin: status"
    }, args => {
      console.log("/status executed", args)
    })

    this.panels.register({
      id: "about",
      title: "Plugin status",
      html: "<p>This plugin is active.</p>"
    })
  }
}`,
  commandButton: `export default class TaskPlugin extends Plugin {
  onLoad() {
    const runTask = async (args: string[]) => {
      console.log("Task executed with:", args)
    }

    this.commands.register(
      {id: "run-task", label: "Run task"},
      runTask
    )
  }
}`,
  panel: `export default class StatusPlugin extends Plugin {
  onLoad() {
    this.panels.register({
      id: "status",
      title: "Plugin status",
      html: "<p>Ready. Use /run-task to execute the action.</p>"
    })
  }
}`,
  events: `// manifest.json
"events": ["rc.message", "pm.received"]

// inside onLoad/onStart
this.events.on("pm.received", (playerId, account, nick, message) => {
  console.log(account, message)
})`,
  eventPayload: `this.events.on("pm.received", (playerId, account, nickname, text) => {
  console.log({playerId, account, nickname, text})
})`,
  storage: `const webhook = await this.secrets.get("webhookUrl")
await this.storage.set("enabled", "true")
const enabled = await this.storage.get("enabled")

if (!webhook) {
  console.warn("Configure the webhook before sending events")
}`,
  networkManifest: `"apis": ["network.http"],
"network": ["https://discord.com"]`,
  network: `const response = await this.network.request({
  url: "https://discord.com/api/webhooks/...",
  method: "POST",
  headers: {"Content-Type": "application/json"},
  body: {content: "Event received by GoRC"}
})
console.log(response.status)`,
  fileBrowserManifest: `"apis": ["filebrowser.read", "filebrowser.write", "filebrowser.editor"],
"files": {
  "read": ["levels/**/*.arc"],
  "write": ["levels/**/*.arc"]
}`,
  fileBrowser: `this.fileBrowser.editors.register({
  id: "arc-visual-editor",
  label: "Visual ARC editor",
  extensions: [".arc"],
  priority: 100
}, async file => {
  const document = await this.fileBrowser.readText(file.path)
  const window = await this.ui.windows.open({
    id: "arc-editor",
    title: file.name,
    view: {type: "code", language: "arc", value: document.content}
  })
  return {handled: true}
})`,
  ui: `const window = await this.ui.windows.open({
  id: "monitor",
  title: "Event monitor",
  view: {type: "stack", gap: 12, children: [
    {type: "heading", text: "Plugin monitor"},
    {type: "text", text: "Waiting for an action…", tone: "muted"},
    {type: "button", id: "refresh", label: "Refresh", action: "refresh"}
  ]}
})

this.ui.onAction(async action => {
  if (action.windowId !== window.id || action.action !== "refresh") return
  await window.update({type: "text", text: "Refreshed at " + new Date().toISOString()})
})`,
  notifications: `// manifest.json
"apis": ["ui.notification"]

this.notifications.success("Webhook connected", "Discord logger")
this.notifications.error("Could not send event")`,
  automation: `const job = this.automation.schedule(async () => {
  const state = await this.storage.get("enabled")
  console.log("automation tick", state)
}, {delayMs: 1_000, intervalMs: 30_000})

const onMessage = this.automation.debounce(
  (message: string) => console.log("coalesced", message),
  250
)

await this.automation.retry(
  () => this.network.request({url: "https://api.example.com/health"}),
  {attempts: 4, delayMs: 500, backoff: 2}
)

job.pause()
job.resume()
// job.cancel() is optional: unload/reload cancels it automatically.`,
  monaco: `"apis": ["monaco"]

this.monaco.diagnostics.register("graalscript", ({text}) => {
  const diagnostics = []
  if (text.includes("TODO")) diagnostics.push({
    message: "Remove TODO before publishing",
    severity: 2,
    startLine: 1, startColumn: 1,
    endLine: 1, endColumn: 5,
    source: "quality-plugin"
  })
  return diagnostics
})

this.monaco.completions.register("graalscript", ({position}) => [{
  label: "log",
  insertText: "log()",
  kind: "function",
  detail: "Quality plugin helper"
}])`,
  nc: `"apis": [
  "nc.list", "nc.readWeapon", "nc.readClass", "nc.readNPC",
  "nc.saveWeapon"
]

const index = await this.nc.list()
for (const weapon of index.weapons) {
  const document = await this.nc.readWeapon(weapon.name)
  console.log(document.name, document.script.length)
}

await this.nc.saveWeapon("Example", "function onCreated() {\n  // generated\n}")`,
  rpc: `"apis": ["plugins.messaging", "express.http"],
"plugins": ["com.example.audit"]

this.plugins.expose("health", async () => ({ok: true}))
const health = await this.plugins.call<{ok: boolean}>(
  "com.example.audit", "health", {}
)

const api = await this.express.listen()
await api.post("/events", async (request, response) => {
  response.status(202).json({accepted: request.json()})
})`,
  action: `"apis": ["pm.send"]

await this.actions.pm.send(playerId, "Message sent by the plugin")`,
  socketsManifest: `"apis": ["network.socket"],
"network": ["wss://gateway.example.com"]`,
  sockets: `const socket = await this.sockets.connect({
  url: "wss://gateway.example.com"
})
socket.on("message", event => {
  console.log("Gateway:", event.data)
})
await socket.send(JSON.stringify({type: "hello"}))`,
  messagingManifest: `"apis": ["plugins.messaging"],
"plugins": ["com.example.audit"]`,
  messaging: `const stop = this.plugins.on("audit", message => {
  console.log("Message from", message.from, message.data)
})

await this.plugins.send("com.example.audit", "audit", {type: "pm.received"})
// stop() removes the listener when the plugin is unloaded`,
  express: `const server = await this.express.listen()

await server.post("/events", async (request, response) => {
  const event = request.json<{type: string}>()
  response.status(202).json({accepted: event?.type ?? "unknown"})
})

console.log("Local API:", server.baseUrl)
// External local callers must send X-GoRC-Plugin-Token: server.token`,
}

const tutorialCopies: Record<Language, TutorialCopy[]> = {
  "pt-BR": [
    {title: "Abra o workspace de plugins", paragraphs: ["Na tela Settings → Plugins, clique em Abrir área de plugins e depois em Novo plugin.", "Informe o nome do plugin. O GoRC cria manifest.json, src/index.ts, as declarações da API e o bundle inicial diretamente no diretório correto."]},
    {title: "Configure o manifest", paragraphs: ["Edite manifest.json. Neste primeiro exemplo, o plugin vai apenas observar mensagens do RC:", "O id deve ser único. Use uma versão semântica e declare somente as permissões realmente necessárias."], code: [tutorialCode.manifest]},
    {title: "Escreva o plugin", paragraphs: ["Abra src/index.ts e substitua a classe de exemplo pelo seu código. Exporte a classe com export default; o GoRC cria a instância automaticamente.", "Clique em Compilar: o GoRC transpila o TypeScript para dist/index.js sem exigir Node.js ou npm."], code: [tutorialCode.bundle]},
    {title: "Confira o template", paragraphs: ["Use Abrir pasta para localizar o diretório criado pelo GoRC e revise manifest.json, src/index.ts e dist/index.js.", "O template já é criado com uma estrutura válida e começa desativado para que você revise o código antes de executar."]},
    {title: "Instale no GoRC", paragraphs: ["Na tela Settings → Plugins, copie ou mova a pasta example-plugin para o diretório mostrado em Plugins instalados. Depois clique em Atualizar.", "O plugin aparecerá como desativado. Isso é intencional: nenhum plugin começa executando sem sua aprovação."]},
    {title: "Aprove e ative", paragraphs: ["Na lista, revise o nome, versão, descrição e permissões. Clique em Aprovar permissões e depois marque Ativo.", "Se o status ficar invalid, leia a mensagem exibida no card e corrija o manifest ou o caminho de main."]},
    {title: "Teste o evento", paragraphs: ["Com o plugin ativo, receba ou envie uma mensagem no chat do RC. O handler registrado em this.events será chamado dentro do sandbox.", "Para testar outros eventos, adicione o nome ao manifest e ao código. Depois de alterar permissões ou o bundle, desative e ative o plugin novamente."], code: [tutorialCode.events]},
    {title: "Adicione configuração persistente", paragraphs: ["Use storage para dados comuns e secrets para tokens, webhooks e credenciais:", "O plugin não grava diretamente no filesystem. O host namespaces os dados pelo ID do plugin."], code: [tutorialCode.storage]},
    {title: "Use requests externos com segurança", paragraphs: ["Declare o origin no manifest e peça a permissão network.http:", "No código, use a API de rede do plugin:", "Somente HTTPS e origins aprovados são aceitos. Nunca coloque tokens diretamente no bundle; use secrets."], code: [tutorialCode.networkManifest, tutorialCode.network]},
    {title: "Abra um WebSocket controlado", paragraphs: ["Para conectar a um gateway, declare network.socket e o origin wss:// exato no manifest. O GoRC controla a conexão, limita mensagens a 1 MiB e fecha sockets quando o plugin é descarregado."], code: [tutorialCode.socketsManifest, tutorialCode.sockets]},
    {title: "Converse com outro plugin", paragraphs: ["Declare plugins.messaging e liste cada ID de destino em plugins. O envio só é permitido quando o remetente e o destinatário estão ativos e aprovados; use channels pequenos e dados JSON."], code: [tutorialCode.messagingManifest, tutorialCode.messaging]},
    {title: "Crie uma API HTTP interna", paragraphs: ["express.http cria uma API local no próprio GoRC, com token por plugin. A API tem formato Express, mas não é o pacote Express do npm e não exige Node.js."], code: [tutorialCode.express]},
    {title: "Adicione ações de escrita somente quando necessário", paragraphs: ["Ações administrativas exigem permissões próprias:", "Também existem admin.send, rc.execute, nc.saveWeapon, nc.saveClass e nc.saveNPC. Use o menor conjunto possível."], code: [tutorialCode.action]},
    {title: "Compile, atualize e depure", paragraphs: ["Depois de editar o plugin, clique em Compilar e, se necessário, em Recarregar. O GoRC executa o bundle gerado sem ferramentas externas.", "Erros do bundle aparecem no log do WebView identificados pelo ID do plugin. Um erro não deve derrubar os outros plugins nem o RC."]},
  ],
  en: [
    {title: "Open the plugin workspace", paragraphs: ["In Settings → Plugins, click Open plugin workspace and then New plugin.", "Enter a plugin name. GoRC creates manifest.json, src/index.ts, the API declarations, and the initial bundle directly in the correct directory."]},
    {title: "Configure the manifest", paragraphs: ["Edit manifest.json. In this first example, the plugin only observes RC messages:", "The id must be unique. Use a semantic version and declare only the permissions you really need."], code: [tutorialCode.manifest]},
    {title: "Write the plugin", paragraphs: ["Open src/index.ts and replace the example class with your code. Export it with export default; GoRC creates the instance automatically.", "Click Compile: GoRC transpiles TypeScript to dist/index.js without requiring Node.js or npm."], code: [tutorialCode.bundle]},
    {title: "Review the template", paragraphs: ["Use Open folder to locate the directory created by GoRC and review manifest.json, src/index.ts, and dist/index.js.", "The template is created with a valid structure and starts disabled so you can review the code before running it."]},
    {title: "Install it in GoRC", paragraphs: ["In Settings → Plugins, copy or move the example-plugin folder into the directory shown under Installed plugins. Then click Refresh.", "The plugin starts disabled. This is intentional: no plugin runs before you approve it."]},
    {title: "Approve and enable it", paragraphs: ["Review the name, version, description and permissions. Click Approve permissions, then mark the plugin as Enabled.", "If the status is invalid, read the error on the card and fix the manifest or main path."]},
    {title: "Test the event", paragraphs: ["With the plugin enabled, receive or send a message in RC chat. The handler registered through this.events runs inside the sandbox.", "To test other events, add the name to the manifest and code. After changing permissions or the bundle, disable and enable the plugin again."], code: [tutorialCode.events]},
    {title: "Add persistent configuration", paragraphs: ["Use storage for regular data and secrets for tokens, webhooks and credentials:", "The plugin does not write directly to the filesystem. The host namespaces data by plugin ID."], code: [tutorialCode.storage]},
    {title: "Use external requests safely", paragraphs: ["Declare the origin in the manifest and request the network.http permission:", "Use the plugin network API in your code:", "Only HTTPS and approved origins are accepted. Never put tokens directly in the bundle; use secrets."], code: [tutorialCode.networkManifest, tutorialCode.network]},
    {title: "Open a controlled WebSocket", paragraphs: ["To connect to a gateway, declare network.socket and the exact wss:// origin in the manifest. GoRC controls the connection, limits messages to 1 MiB, and closes sockets when the plugin unloads."], code: [tutorialCode.socketsManifest, tutorialCode.sockets]},
    {title: "Message another plugin", paragraphs: ["Declare plugins.messaging and list each target ID in plugins. Sending is allowed only when both plugins are active and approved; keep channels small and data JSON-serializable."], code: [tutorialCode.messagingManifest, tutorialCode.messaging]},
    {title: "Create an internal HTTP API", paragraphs: ["express.http creates a local API inside GoRC with a per-plugin token. The API has an Express-style shape, but it is not the npm Express package and does not require Node.js."], code: [tutorialCode.express]},
    {title: "Add write actions only when needed", paragraphs: ["Administrative actions require their own permissions:", "Other actions include admin.send, rc.execute, nc.saveWeapon, nc.saveClass and nc.saveNPC. Use the smallest set possible."], code: [tutorialCode.action]},
    {title: "Compile, update, and debug", paragraphs: ["After editing the plugin, click Compile and, if needed, Reload. GoRC runs the generated bundle without external tools.", "Bundle errors appear in the WebView log with the plugin ID. One failing plugin must not take down other plugins or RC."]},
  ],
  es: [
    {title: "Abre el workspace de plugins", paragraphs: ["En Settings → Plugins, pulsa Abrir área de plugins y después Nuevo plugin.", "Introduce el nombre del plugin. GoRC crea manifest.json, src/index.ts, las declaraciones de la API y el bundle inicial directamente en el directorio correcto."]},
    {title: "Configura el manifest", paragraphs: ["Edita manifest.json. En este primer ejemplo, el plugin solo observa mensajes del RC:", "El id debe ser único. Usa una versión semántica y declara solo los permisos necesarios."], code: [tutorialCode.manifest]},
    {title: "Escribe el plugin", paragraphs: ["Abre src/index.ts y reemplaza la clase de ejemplo por tu código. Expórtala con export default; GoRC crea la instancia automáticamente.", "Pulsa Compilar: GoRC transpila TypeScript a dist/index.js sin necesitar Node.js ni npm."], code: [tutorialCode.bundle]},
    {title: "Revisa el template", paragraphs: ["Usa Abrir carpeta para localizar el directorio creado por GoRC y revisa manifest.json, src/index.ts y dist/index.js.", "El template se crea con una estructura válida y empieza desactivado para que revises el código antes de ejecutarlo."]},
    {title: "Instálalo en GoRC", paragraphs: ["En Settings → Plugins, copia o mueve la carpeta example-plugin al directorio mostrado en Plugins instalados. Después pulsa Actualizar.", "El plugin aparecerá desactivado. Es intencional: ningún plugin se ejecuta antes de tu aprobación."]},
    {title: "Aprueba y activa", paragraphs: ["Revisa el nombre, versión, descripción y permisos. Pulsa Aprobar permisos y marca el plugin como Activo.", "Si el estado es invalid, lee el error del card y corrige el manifest o la ruta main."]},
    {title: "Prueba el evento", paragraphs: ["Con el plugin activo, recibe o envía un mensaje en el chat del RC. El handler registrado mediante this.events se ejecutará dentro del sandbox.", "Para probar otros eventos, añade el nombre al manifest y al código. Después de cambiar permisos o el bundle, desactiva y activa el plugin otra vez."], code: [tutorialCode.events]},
    {title: "Añade configuración persistente", paragraphs: ["Usa storage para datos comunes y secrets para tokens, webhooks y credenciales:", "El plugin no escribe directamente en el filesystem. El host separa los datos por ID del plugin."], code: [tutorialCode.storage]},
    {title: "Usa requests externos de forma segura", paragraphs: ["Declara el origin en el manifest y solicita el permiso network.http:", "Usa la API de red del plugin en tu código:", "Solo se aceptan HTTPS y origins aprobados. Nunca pongas tokens directamente en el bundle; usa secrets."], code: [tutorialCode.networkManifest, tutorialCode.network]},
    {title: "Abre un WebSocket controlado", paragraphs: ["Para conectar con un gateway, declara network.socket y el origin wss:// exacto en el manifest. GoRC controla la conexión, limita los mensajes a 1 MiB y cierra los sockets cuando el plugin se descarga."], code: [tutorialCode.socketsManifest, tutorialCode.sockets]},
    {title: "Comunícate con otro plugin", paragraphs: ["Declara plugins.messaging y lista cada ID de destino en plugins. El envío solo se permite cuando ambos plugins están activos y aprobados; usa channels pequeños y datos JSON."], code: [tutorialCode.messagingManifest, tutorialCode.messaging]},
    {title: "Crea una API HTTP interna", paragraphs: ["express.http crea una API local dentro de GoRC con un token por plugin. Tiene forma Express, pero no es el paquete Express de npm y no necesita Node.js."], code: [tutorialCode.express]},
    {title: "Añade acciones de escritura solo cuando sea necesario", paragraphs: ["Las acciones administrativas requieren permisos propios:", "También existen admin.send, rc.execute, nc.saveWeapon, nc.saveClass y nc.saveNPC. Usa el conjunto mínimo posible."], code: [tutorialCode.action]},
    {title: "Compila, actualiza y depura", paragraphs: ["Después de editar el plugin, pulsa Compilar y, si es necesario, Recargar. GoRC ejecuta el bundle generado sin herramientas externas.", "Los errores del bundle aparecen en el log del WebView con el ID del plugin. Un plugin con errores no debe derribar los demás ni el RC."]},
  ],
}

function LocalizedPluginTutorial({language}: {language: Language}) {
  return <>{tutorialCopies[language].map((step, index) => <TutorialStep key={step.title} number={String(index + 1)} title={step.title}>{step.paragraphs.map(paragraph => <p key={paragraph}>{paragraph}</p>)}{step.code?.map(code => <DocumentationCodeBlock key={code} language={language}>{code}</DocumentationCodeBlock>)}</TutorialStep>)}</>
}

function DocCard({id, icon, title, children}: {id?: string; icon: ReactNode; title: string; children: ReactNode}) {
  return (
    <section id={id} className="scroll-mt-6 overflow-hidden rounded-lg border bg-card/40">
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <span className="text-primary">{icon}</span>
        <h3 className="font-medium">{title}</h3>
      </div>
      <div className="grid gap-3 p-4">{children}</div>
    </section>
  )
}

function PermissionRow({name, description}: {name: string; description: string}) {
  return (
    <div className="flex flex-col gap-1 rounded-md border bg-muted/20 px-3 py-2 sm:flex-row sm:items-baseline sm:gap-3">
      <code className="shrink-0 text-xs text-primary">{name}</code>
      <span className="text-muted-foreground text-xs">{description}</span>
    </div>
  )
}

type CodeLanguage = "typescript" | "json" | "text"
type CodeTokenKind = "comment" | "string" | "number" | "keyword" | "boolean" | "function" | "property" | "operator" | "punctuation" | "plain"

function inferCodeLanguage(code: string): CodeLanguage {
  const trimmed = code.trimStart()
  if (trimmed.startsWith("com.example.") && trimmed.includes("manifest.json")) return "text"
  if (trimmed.startsWith("{") || /^\"(?:apis|events|network|plugins)\"\s*:/.test(trimmed)) return "json"
  return "typescript"
}

function tokenClass(kind: CodeTokenKind): string {
  switch (kind) {
    case "comment": return "text-slate-500 italic dark:text-slate-400"
    case "string": return "text-amber-700 dark:text-amber-300"
    case "number": return "text-cyan-700 dark:text-cyan-300"
    case "keyword": return "text-sky-700 dark:text-sky-300"
    case "boolean": return "text-violet-700 dark:text-violet-300"
    case "function": return "text-emerald-700 dark:text-emerald-300"
    case "property": return "text-blue-700 dark:text-blue-300"
    case "operator": return "text-pink-700 dark:text-pink-300"
    case "punctuation": return "text-muted-foreground"
    default: return "text-foreground"
  }
}

function tokenizeCodeLine(line: string, language: CodeLanguage): Array<{text: string; kind: CodeTokenKind}> {
  const tokens: Array<{text: string; kind: CodeTokenKind}> = []
  const keywords = new Set(["export", "default", "class", "extends", "const", "let", "var", "this", "new", "return", "if", "else", "async", "await", "function", "type", "interface", "readonly", "onLoad", "onStart", "onStop", "onUnload"])
  let index = 0
  while (index < line.length) {
    const rest = line.slice(index)
    if (rest.startsWith("//")) {
      tokens.push({text: rest, kind: "comment"})
      break
    }
    if (rest.startsWith("/*")) {
      const end = line.indexOf("*/", index + 2)
      const length = end < 0 ? line.length - index : end + 2 - index
      tokens.push({text: line.slice(index, index + length), kind: "comment"})
      index += length
      continue
    }
    const character = line[index]
    if (character === '"' || character === "'" || character === "`") {
      let end = index + 1
      while (end < line.length) {
        if (line[end] === "\\") { end += 2; continue }
        if (line[end] === character) { end += 1; break }
        end += 1
      }
      tokens.push({text: line.slice(index, end), kind: "string"})
      index = end
      continue
    }
    if (/\d/.test(character)) {
      const match = rest.match(/^(?:0x[\da-f]+|\d+(?:\.\d+)?)/i)
      const value = match?.[0] ?? character
      tokens.push({text: value, kind: "number"})
      index += value.length
      continue
    }
    if (/[A-Za-z_$]/.test(character)) {
      const match = rest.match(/^[A-Za-z_$][\w$]*/)
      const value = match?.[0] ?? character
      const following = line.slice(index + value.length).trimStart()[0]
      const kind: CodeTokenKind = value === "true" || value === "false" || value === "null" || value === "undefined"
        ? "boolean"
        : keywords.has(value)
          ? "keyword"
          : following === "(" ? "function" : language === "json" && following === ":" ? "property" : "plain"
      tokens.push({text: value, kind})
      index += value.length
      continue
    }
    if (/[=+*/<>!&|?:%-]/.test(character)) {
      tokens.push({text: character, kind: "operator"})
    } else if (/[{}()[\].,;]/.test(character)) {
      tokens.push({text: character, kind: "punctuation"})
    } else {
      tokens.push({text: character, kind: "plain"})
    }
    index += 1
  }
  return tokens
}

function DocumentationCodeBlock({children, language}: {children: string; language: Language}) {
  const labels = language === "en" ? {copy: "Copy", copied: "Copied"} : language === "es" ? {copy: "Copiar", copied: "Copiado"} : {copy: "Copiar", copied: "Copiado"}
  return <CodeBlock copyLabel={labels.copy} copiedLabel={labels.copied}>{children}</CodeBlock>
}

function CodeBlock({children, language, copyLabel = "Copy", copiedLabel = "Copied"}: {children: string; language?: CodeLanguage; copyLabel?: string; copiedLabel?: string}) {
  const [copied, setCopied] = useState(false)
  const codeLanguage = language ?? inferCodeLanguage(children)
  const label = codeLanguage === "typescript" ? "TypeScript" : codeLanguage === "json" ? "JSON" : "Structure"
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(children)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      setCopied(false)
    }
  }
  const lines = children.split("\n")
  return (
    <div className="overflow-hidden rounded-lg border bg-[#0b0d10] text-slate-100 shadow-sm dark:bg-[#0b0d10]">
      <div className="flex items-center justify-between border-b border-white/10 bg-white/[0.04] px-3 py-2">
        <span className="font-mono text-[10px] font-semibold uppercase tracking-[0.12em] text-slate-400">{label}</span>
        <button type="button" onClick={() => void copy()} className="inline-flex items-center gap-1.5 rounded px-2 py-1 text-[11px] text-slate-400 transition-colors hover:bg-white/10 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-400" aria-label={copyLabel} title={copyLabel}>
          <Copy className="size-3.5" />{copied ? copiedLabel : copyLabel}
        </button>
      </div>
      <pre className="max-h-[34rem] overflow-auto p-3 font-mono text-[12px] leading-5"><code>{lines.map((line, lineIndex) => (
        <span className="flex min-w-max" key={`${lineIndex}-${line}`}>
          <span className="mr-4 w-5 shrink-0 select-none text-right text-slate-600">{lineIndex + 1}</span>
          <span>{line.length ? tokenizeCodeLine(line, codeLanguage).map((token, tokenIndex) => <span className={tokenClass(token.kind)} key={`${tokenIndex}-${token.text}`}>{token.text}</span>) : " "}</span>
        </span>
      ))}</code></pre>
    </div>
  )
}

// FilesSection configures the required downloads folder (without it, downloads
// in the File Browser are disabled). Persists via the backend and stays in sync
// across windows via the raw rc:fbConfig event.
function FilesSection({t}: {t: (key: string) => string}) {
  const [dir, setDir] = useState("")

  useEffect(() => {
    rcService
      .getFileBrowserConfig()
      .then((c) => setDir(c.downloadDir ?? ""))
      .catch(() => {})
    const off = Events.On("rc:fbConfig", (e: {data: string}) => {
      try {
        const c = JSON.parse(e.data) as FileBrowserConfig
        setDir(c.downloadDir ?? "")
      } catch {
        // ignore
      }
    })
    return () => {
      off()
    }
  }, [])

  const browse = async () => {
    const chosen = await rcService.chooseDirectory()
    if (chosen) {
      await rcService.setFileBrowserConfig(chosen)
      setDir(chosen)
    }
  }

  const clear = async () => {
    await rcService.setFileBrowserConfig("")
    setDir("")
  }

  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.files")} description={t("settings.filesDescription")} />
      <div className="overflow-hidden rounded-lg border bg-card/40">
        <div className="border-b px-4 py-3">
          <p className="text-sm font-medium">{t("settings.downloads")}</p>
          <p className="text-muted-foreground mt-1 text-xs">{t("settings.downloadsDescription")}</p>
        </div>
        <div className="grid gap-3 p-4">
          <Label htmlFor="dl-dir">{t("settings.downloadsFolder")}</Label>
          <Input id="dl-dir" value={dir} readOnly placeholder={`${t("settings.notSet")} — downloads disabled`} />
          <div className="flex gap-2">
            <Button variant="outline" onClick={browse}>{t("settings.browse")}</Button>
            <Button variant="ghost" onClick={clear} disabled={!dir}>{t("settings.clear")}</Button>
          </div>
        </div>
      </div>
    </div>
  )
}

function primaryFontFamily(value: string): string {
  const first = value.split(",", 1)[0]?.trim() ?? ""
  if (first.length >= 2 && first[0] === first[first.length - 1] && (first[0] === "'" || first[0] === '"')) {
    return first.slice(1, -1).trim()
  }
  return first
}

function CodingSection({
  theme,
  fontFamily,
  fontSize,
  onChange,
  onReset,
  t,
}: {
  theme: string
  fontFamily: string
  fontSize: number
  onChange: (patch: {theme?: string; fontFamily?: string; fontSize?: number}) => void
  onReset: () => void
  t: (key: string) => string
}) {
  // Installed system fonts for the font picker. Fetched once; the current
  // value is kept as an option so older/custom settings remain selectable.
  const [fonts, setFonts] = useState<string[]>([])
  useEffect(() => {
    rcService
      .listFonts()
      .then((list) => {
        if (list) setFonts(list)
      })
      .catch(() => {})
  }, [])

  // Active remote theme name (if any), so the Theme dropdown can show it.
  const [remoteName, setRemoteName] = useState<string>("")
  const [remoteDefinition, setRemoteDefinition] = useState<string | undefined>(undefined)
  const [customThemes, setCustomThemes] = useState<CustomTheme[]>([])
  const [themeDialog, setThemeDialog] = useState<{open: boolean; theme?: CustomTheme}>({open: false})
  useEffect(() => {
    rcService
      .getRemoteTheme()
      .then((rt) => {
        if (rt?.name) setRemoteName(rt.name)
        if (rt?.definition) setRemoteDefinition(rt.definition)
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    rcService.getCustomThemes().then((themes) => setCustomThemes(themes ?? [])).catch(() => {})
  }, [])

  const activeCustom = customThemes.find((item) => item.key === theme)
  const selectedFont = primaryFontFamily(fontFamily) || "monospace"
  const fontOptions = Array.from(new Set([selectedFont, ...fonts].filter(Boolean)))

  const activateRemote = useCallback(async (name: string, definition: string) => {
    await rcService.saveRemoteTheme(name, definition)
    setRemoteName(name)
    setRemoteDefinition(definition)
    onChange({theme: "remoteTheme"})
  }, [onChange])

  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.coding")} description={t("settings.codingDescription")} />
      <div className="overflow-hidden rounded-lg border bg-card/40">
        <div className="border-b px-4 py-3">
          <p className="text-sm font-medium">{t("settings.editorAppearance")}</p>
          <p className="text-muted-foreground mt-1 text-xs">{t("settings.editorAppearanceDescription")}</p>
        </div>
        <div className="grid gap-4 p-4">
          <ThemePreview
            key={`settings-preview-${theme}-${themeDialog.open ? "dialog-open" : "dialog-closed"}`}
            theme={theme}
            definition={activeCustom?.definition ?? (theme === "remoteTheme" ? remoteDefinition : undefined)}
            fontFamily={fontFamily}
            fontSize={fontSize}
          />
          <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:items-center">
            <Label htmlFor="theme">{t("settings.theme")}</Label>
            <ThemeSelect
              value={theme}
              onChange={(k) => onChange({theme: k})}
              customOptions={customThemes.map((item) => ({key: item.key, label: `${item.name} · ${t("settings.customTheme")}`}))}
              extraOption={
                theme === "remoteTheme" && remoteName
                  ? {key: "remoteTheme", label: `Remote: ${remoteName}`}
                  : undefined
              }
            />
            <div className="flex flex-wrap gap-2">
              <NewThemeButton onClick={() => setThemeDialog({open: true})} />
              {activeCustom && <Button variant="ghost" size="sm" onClick={() => setThemeDialog({open: true, theme: activeCustom})}>{t("settings.editTheme")}</Button>}
            </div>
          </div>

      <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:items-start">
        <Label htmlFor="remote" className="pt-2">
          {t("settings.remoteTheme")}
        </Label>
        <RemoteThemePicker onActivate={activateRemote} />
      </div>

      <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:items-center">
        <Label htmlFor="font">{t("settings.fontFamily")}</Label>
        <select
          id="font"
          value={selectedFont}
          onChange={(e) => onChange({fontFamily: e.target.value})}
          className="border-input bg-background ring-offset-background focus-visible:ring-ring flex h-9 w-full rounded-md border px-3 py-1 text-sm shadow-sm outline-none focus-visible:ring-2 focus-visible:ring-offset-2"
        >
          {fontOptions.map((font) => (
            <option key={font} value={font}>{font}</option>
          ))}
        </select>
      </div>

      <div className="grid gap-2 sm:grid-cols-[140px_1fr] sm:items-center">
        <Label htmlFor="size">{t("settings.fontSize")}</Label>
        <div className="flex items-center gap-3">
          <input
            id="size"
            type="range"
            min={8}
            max={32}
            value={fontSize}
            onChange={(e) => onChange({fontSize: Number(e.target.value)})}
            className="flex-1"
          />
          <span className="text-muted-foreground w-8 text-sm tabular-nums">{fontSize}</span>
        </div>
      </div>

        </div>
      </div>
      <div className="flex justify-end">
        <Button variant="ghost" onClick={onReset}>{t("settings.reset")}</Button>
      </div>
      <CustomThemeDialog
        open={themeDialog.open}
        theme={themeDialog.theme}
        onClose={() => setThemeDialog({open: false})}
        onSaved={(saved) => {
          setCustomThemes((current) => [...current.filter((item) => item.key !== saved.key), saved])
          onChange({theme: saved.key})
        }}
      />
    </div>
  )
}

const THEMELIST_URL =
  "https://cdn.jsdelivr.net/gh/brijeshb42/monaco-themes@master/themes/themelist.json"
const THEMES_BASE =
  "https://cdn.jsdelivr.net/gh/brijeshb42/monaco-themes@master/themes/"

// RemoteThemePicker is an autocomplete over the brijeshb42/monaco-themes gallery
// (~80 themes). On pick it fetches the theme JSON, caches it via the backend
// (so it works offline afterwards), and activates it.
function RemoteThemePicker({
  onActivate,
}: {
  onActivate: (name: string, definition: string) => void | Promise<void>
}) {
  const {t} = useLanguage()
  const [names, setNames] = useState<string[]>([])
  const [value, setValue] = useState("")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  useEffect(() => {
    fetch(THEMELIST_URL)
      .then((r) => r.json() as Promise<Record<string, string>>)
      .then((map) => setNames(Object.values(map).sort((a, b) => a.localeCompare(b))))
      .catch(() => setError(t("settings.galleryError")))
  }, [])

  const apply = useCallback(async () => {
    const name = value.trim()
    if (!name) return
    setBusy(true)
    setError("")
    try {
      // themelist.json maps {slug: displayName}; the theme files are named by
      // displayName (e.g. "Blackboard.json", with spaces), so fetch directly.
      const definition = await fetch(THEMES_BASE + encodeURIComponent(name) + ".json").then((r) => {
        if (!r.ok) throw new Error(String(r.status))
        return r.text()
      })
      await onActivate(name, definition)
    } catch {
      setError(`Couldn't fetch "${name}" (online? exact name?)`)
    } finally {
      setBusy(false)
    }
  }, [value, onActivate])

  return (
    <div className="grid gap-1.5">
      <div className="flex gap-2">
        <Input
          list="remote-themes"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") apply()
          }}
          placeholder={t("settings.gallerySearch")}
        />
        <datalist id="remote-themes">
          {names.map((n) => (
            <option key={n} value={n} />
          ))}
        </datalist>
        <Button onClick={apply} disabled={busy || !value.trim()}>
          {busy ? "…" : t("settings.apply")}
        </Button>
      </div>
      {error && <p className="text-destructive text-xs">{error}</p>}
      {names.length > 0 && (
        <p className="text-muted-foreground text-xs">{names.length} themes in gallery.</p>
      )}
    </div>
  )
}

function ChatSection({
  settings,
  onChange,
  onReset,
  t,
}: {
  settings: ChatSettings
  onChange: (patch: Partial<ChatSettings>) => void
  onReset: () => void
  t: (key: string) => string
}) {
  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.chat")} description={t("settings.chatDescription")} />
      <div className="overflow-hidden rounded-lg border bg-card/40 p-4">
      <ChatSettingsFields
        settings={settings}
        onChange={onChange}
        onBrowse={async () => {
          const dir = await rcService.chooseDirectory()
          if (dir) onChange({logDir: dir})
        }}
        onBrowsePm={async () => {
          const dir = await rcService.chooseDirectory()
          if (dir) onChange({pmLogDir: dir})
        }}
      />
      </div>
      <div className="flex justify-end">
        <Button variant="ghost" onClick={onReset}>{t("settings.reset")}</Button>
      </div>
    </div>
  )
}

function LanguageSection({language, onChange, t}: {language: Language; onChange: (language: Language) => void; t: (key: string) => string}) {
  return (
    <div className="mx-auto grid max-w-3xl gap-5">
      <SectionHeading title={t("settings.language")} description={t("language.description")} />
      <div className="border-border bg-card/40 rounded-lg border p-4">
        <div className="grid gap-2 sm:grid-cols-[180px_1fr] sm:items-center">
          <Label htmlFor="language">{t("language.title")}</Label>
          <LanguagePicker language={language} onChange={onChange} />
        </div>
        <p className="text-muted-foreground mt-3 text-xs">{t("language.saved")}</p>
      </div>
    </div>
  )
}

const languageOptions: Array<{value: Language; label: string; region: string}> = [
  {value: "pt-BR", label: "Português (Brasil)", region: "PT-BR"},
  {value: "en", label: "English", region: "EN-US"},
  {value: "es", label: "Español", region: "ES-ES"},
]

function LanguagePicker({language, onChange}: {language: Language; onChange: (language: Language) => void}) {
  const [open, setOpen] = useState(false)
  const rootRef = useRef<HTMLDivElement>(null)
  const selected = languageOptions.find((option) => option.value === language) ?? languageOptions[0]

  useEffect(() => {
    if (!open) return
    const closeOnOutsideClick = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) setOpen(false)
    }
    const closeOnEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false)
    }
    document.addEventListener("mousedown", closeOnOutsideClick)
    document.addEventListener("keydown", closeOnEscape)
    return () => {
      document.removeEventListener("mousedown", closeOnOutsideClick)
      document.removeEventListener("keydown", closeOnEscape)
    }
  }, [open])

  const choose = (value: Language) => {
    onChange(value)
    setOpen(false)
  }

  return (
    <div ref={rootRef} className="relative">
      <button
        id="language"
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="border-input bg-input/30 hover:bg-input/50 flex h-10 w-full items-center justify-between rounded-md border px-3 text-left text-sm outline-none transition-colors focus-visible:ring-2 focus-visible:ring-ring"
      >
        <span className="flex min-w-0 items-center gap-3">
          <span className="bg-primary/12 text-primary flex size-6 shrink-0 items-center justify-center rounded text-[10px] font-bold tracking-wide">{selected.region.slice(0, 2)}</span>
          <span className="truncate">{selected.label}</span>
        </span>
        <ChevronDown className={`text-muted-foreground size-4 shrink-0 transition-transform ${open ? "rotate-180" : ""}`} />
      </button>
      {open && (
        <div role="listbox" aria-label="Language options" className="border-border bg-popover text-popover-foreground absolute inset-x-0 top-[calc(100%+6px)] z-50 overflow-hidden rounded-md border p-1 shadow-lg">
          {languageOptions.map((option) => {
            const active = option.value === language
            return (
              <button
                key={option.value}
                type="button"
                role="option"
                aria-selected={active}
                onClick={() => choose(option.value)}
                className={`flex w-full items-center justify-between rounded px-2.5 py-2 text-left text-sm transition-colors ${active ? "bg-accent text-accent-foreground" : "hover:bg-accent/70"}`}
              >
                <span className="flex items-center gap-3">
                  <span className={`flex size-6 items-center justify-center rounded text-[10px] font-bold tracking-wide ${active ? "bg-primary/15 text-primary" : "bg-muted text-muted-foreground"}`}>{option.region.slice(0, 2)}</span>
                  <span>
                    <span className="block">{option.label}</span>
                    <span className="text-muted-foreground block text-[11px]">{option.region}</span>
                  </span>
                </span>
                {active && <Check className="text-primary size-4" />}
              </button>
            )
          })}
        </div>
      )}
    </div>
  )
}

function SectionHeading({title, description}: {title: string; description: string}) {
  return (
    <div>
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      <p className="text-muted-foreground mt-1 text-sm">{description}</p>
    </div>
  )
}
