export interface ChangelogEntry {
  version: string
  date: string
  title: string
  summary: string
  changes: string[]
  current?: boolean
}

// Keep release notes in the client as an offline fallback. The RC refreshes
// the visible history from nullborne.com when the release service is reachable.
const CHANGELOG_PT_BR: ChangelogEntry[] = [
  {
    version: "3.2.0",
    date: "17/08/2026",
    title: "Cross-platform File Browser and external editing",
    summary: "Completes the File Browser workflow, adds native .nw/.gmap editing and makes GraalScript highlighting follow RC themes.",
    current: true,
    changes: [
      "Double-clicking a .nw or .gmap file downloads it to the configured folder, opens it with the operating system's default application and uploads local edits when the server grants write access.",
      "Thumbnail previews are serialized through the native File Browser transfer channel, keep preview protocol messages out of the visible log and never fetch images at or above 500 KB.",
      "GraalScript theme definitions now cover the full set of RC themes, while Script Manager refreshes coalesce concurrent cache events into a single native request.",
      "External file sessions are detached safely when the RC disconnects, logs out or switches servers, preventing edits from being sent to the wrong session.",
    ],
  },
  {
    version: "3.1.7",
    date: "16/08/2026",
    title: "Distribuição macOS e Linux reforçada",
    summary: "Corrige o empacotamento multiplataforma, o ícone do macOS e a independência do RC em relação ao terminal.",
    current: true,
    changes: [
      "A build macOS agora entrega um app bundle com ícone icns e metadados de versão corretos; o artefato x64 funciona nativamente em Macs Intel e via Rosetta 2 em Apple Silicon.",
      "O AppImage Linux passa a carregar o runtime GTK4/WebKitGTK6 e o grclib.so nativos; os pacotes DEB/RPM também instalam a biblioteca necessária.",
      "O RC não depende mais de um terminal aberto para permanecer executando, e o updater macOS usa o novo arquivo tar.gz.",
      "Os três instaladores usam nomes canônicos e metadados alinhados à versão 3.1.7.",
    ],
  },
  {
    version: "3.1.6",
    date: "15/08/2026",
    title: "Fluxo de scripts e Sync mais controláveis",
    summary: "Melhora o Script Manager, a personalização visual e o controle das notificações de Sync.",
    current: false,
    changes: [
      "O Script Manager agora permite abrir qualquer script sincronizado diretamente no explorador de arquivos nativo do Windows, macOS ou Linux pelo menu de contexto.",
      "As notificações de progresso do Sync podem ser minimizadas ou dispensadas enquanto o download inicial continua em segundo plano.",
      "O linter GraalScript deixou de exigir ponto e vírgula depois de declarações de enum.",
      "Settings agora permite criar, editar e selecionar temas do RC, incluindo a cor da borda das janelas.",
    ],
  },
  {
    version: "3.1.5",
    date: "15/08/2026",
    title: "Updater Windows definitivo e versão visível",
    summary: "Corrige o fluxo final do auto-update Windows e mostra a versão na tela de login.",
    current: false,
    changes: [
      "O helper do auto-update Windows agora é iniciado pelo broker do Windows com elevação antes do RC fechar, aguarda o processo, instala a atualização e relança o aplicativo.",
      "A tela de login agora mostra a versão atual do RC no título para facilitar a confirmação do build instalado.",
    ],
  },
  {
    version: "3.1.4",
    date: "15/08/2026",
    title: "Frame customizada e tema NightOwl",
    summary: "Melhora a experiência das janelas de links e adiciona um tema dedicado para desenvolvimento GS2.",
    current: false,
    changes: [
      "Links abertos pelo RC Chat agora usam a mesma frame customizada, controles de janela e WebView interno das demais janelas do RC.",
      "Adicionado o tema NightOwl ao editor Monaco, com destaque alinhado aos tokens de sintaxe do GS2.",
    ],
  },
  {
    version: "3.1.3",
    date: "15/08/2026",
    title: "Updater e downloads manuais multiplataforma",
    summary: "Corrige o encerramento do auto-update no Windows e permite salvar atualizações do macOS/Linux para instalação manual.",
    current: false,
    changes: [
      "O helper do auto-update Windows agora é iniciado desacoplado, tem limite de espera e força o encerramento do RC quando necessário para liberar o instalador.",
      "Atualizações para macOS e Linux agora mostram um diálogo nativo para escolher onde salvar o arquivo verificado, sem abrir o instalador automaticamente.",
      "A API de releases passou a reconhecer os destinos Windows, Linux e macOS e só anuncia o download quando o artefato correspondente está publicado.",
      "Backups de arquivos, scripts, bancos e textos agora usam um limite configurável por alvo, com 3 versões por padrão e substituição automática da mais antiga.",
    ],
  },
  {
    version: "3.1.2",
    date: "15/08/2026",
    title: "Hotfix do relançamento do atualizador",
    summary: "O RC agora volta a abrir depois que o instalador automático termina.",
    current: false,
    changes: [
      "O helper do auto-update agora aguarda o instalador silencioso, registra falhas e relança o RC ao final para evitar que a atualização deixe o aplicativo fechado.",
    ],
  },
  {
    version: "3.1.1",
    date: "15/08/2026",
    title: "Hotfix do atualizador Windows",
    summary: "Corrige a verificação de processo usada pelo instalador durante o auto-update.",
    current: false,
    changes: [
      "Corrigida a verificação do RC em instalações iniciadas pelo auto-update; o instalador agora executa o tasklist de forma compatível com o plugin Unicode do NSIS.",
    ],
  },
  {
    version: "3.1.0",
    date: "15/08/2026",
    title: "Produtividade e contexto por servidor",
    summary: "Uma grande atualização para acelerar o desenvolvimento GraalScript e manter cada instância do RC isolada e consistente.",
    current: false,
    changes: [
      "Macros de comandos agora são salvas no PC por servidor, com migração automática dos macros antigos do navegador.",
      "Autocomplete contextual para objetos retornados por findplayer, findweapon e findlevel, além de classes importadas, GUIs e objetos built-in atribuídos a variáveis.",
      "Enums GS2 oferecem os nomes internos no autocomplete e exibem os valores explícitos apenas na informação da sugestão; new filtra construtores e preserva new[size] para arrays.",
      "Autocomplete de funções ganhou link para abrir a busca da função na wiki gscript.dev dentro de uma janela WebView do RC; links HTTP(S) do chat também são clicáveis no mesmo fluxo.",
      "Script Manager recebeu Ctrl+F focado no filtro, Ctrl+K passou a abrir armas, classes e NPCs após o primeiro sync, e novas weapons aguardam um segundo antes do refresh automático.",
      "Editor Monaco permite escolher tab width 1, 2 ou 4, com 2 como padrão.",
      "File Browser ganhou busca de pastas, F2 para renomear somente arquivos e log de download consolidado em uma única linha por arquivo.",
      "O RC agora aceita múltiplas instâncias, posiciona novas janelas no monitor da instância e identifica as janelas com o servidor primeiro no título.",
      "Desconexões fecham janelas relacionadas e limpam o contexto do File Browser antes do login ou de uma reconexão em outro servidor.",
      "A tela de criação de plugin aponta diretamente para a documentação oficial atualizada em nullborne.com.",
    ],
  },
  {
    version: "3.0.1",
    date: "15/08/2026",
    title: "Atualizações automáticas e changelog conectado",
    summary: "O RC agora pode se manter atualizado e consultar o histórico publicado pelo serviço de releases da Nullborne.",
    current: false,
    changes: [
      "Verificação automática de atualização em https://nullborne.com/update ao iniciar o RC.",
      "Download, validação e execução automáticos do instalador Windows amd64 antes de fechar a versão antiga.",
      "O changelog dentro do RC é atualizado por https://nullborne.com/changelog e mantém as notas locais como fallback offline.",
      "Metadados do release Windows, tamanho do instalador e SHA-256 publicados pela API de atualização.",
    ],
  },
  {
    version: "3.0.0",
    date: "15/08/2026",
    title: "Operações seguras e desenvolvimento GraalScript",
    summary: "Uma grande atualização para administrar servidores, desenvolver scripts e recuperar alterações com segurança.",
    changes: [
      "Busca global, central de notificações operacionais e atalhos para as principais ferramentas, mantendo o RC Chat como tela principal.",
      "Macros de comandos parametrizadas, com tipos de parâmetro, validação e preenchimento rápido no estilo slash command.",
      "Fluxo de moderação integrado à lista de players, com ações protegidas por permissões e preenchimento automático da conta logada.",
      "Sync seguro com direitos carregados no login, bloqueio para contas com escrita, progresso do primeiro sync, poll padrão de 60 minutos e recuperação de panic mode.",
      "Histórico local de auditoria e backups de scripts sincronizados, com até três versões por script, retenção configurável, exclusão e rollback.",
      "LSP contextual para GraalScript 2, atualização somente após o sync concluir, MCP local e engine de plugins para automação.",
      "RC Chat mais limpo com agrupamento de textos repetidos, preservação de linhas em branco, localização completa e correções na comparação de permissões.",
      "Melhorias no Script Manager e File Browser para ignorar scripts sem nome, evitar caminhos inválidos e exibir fallbacks quando faltam nomes ou ícones.",
    ],
  },
  {
    version: "2.0.0",
    date: "01/08/2026",
    title: "Temas e fluxo de trabalho",
    summary: "Uma atualização focada em personalização, sincronização e produtividade.",
    changes: [
      "Sincronização local modernizada, localização e alertas de mensagens privadas.",
      "Temas do editor mais completos, incluindo preview de temas personalizados.",
      "Autocomplete de comandos e jogadores no RC Chat.",
      "Janelas frameless e persistência do apelido da sessão.",
      "Settings redesenhada e melhorias no fluxo de login e perfil.",
    ],
  },
  {
    version: "1.2.1",
    date: "28/07/2026",
    title: "Inicialização e conexão",
    summary: "Melhorias para abrir o RC com mais clareza e manter o NC disponível.",
    changes: [
      "Skeleton de carregamento no frontend.",
      "Keep-alive para a conexão NC.",
    ],
  },
  {
    version: "1.2.0",
    date: "24/07/2026",
    title: "Build multiplataforma",
    summary: "A distribuição do RC passou a cobrir mais ambientes.",
    changes: [
      "Correção do build cross-platform para macOS Intel.",
    ],
  },
  {
    version: "1.1.0",
    date: "24/07/2026",
    title: "Releases automatizados",
    summary: "O pipeline de release ficou preparado para a expansão multiplataforma.",
    changes: [
      "Adicionado alvo macOS e criação automática de tags no workflow de release.",
    ],
  },
  {
    version: "1.0.0",
    date: "23/07/2026",
    title: "Primeiro release estável",
    summary: "Primeira versão estável do Graal Remote Control.",
    changes: [
      "Pipeline inicial de release e distribuição do aplicativo.",
    ],
  },
  {
    version: "0.1.0",
    date: "22/07/2026",
    title: "Primeira versão pública",
    summary: "Base inicial do RC para administrar servidores Graal.",
    changes: [
      "Sincronização do modelo RCNPC com o grclib.",
      "Exibição do level dos NPCs na interface.",
    ],
  },
]

const CHANGELOG_EN: ChangelogEntry[] = [
  {
    version: "3.2.0", date: "2026-08-17", title: "Cross-platform File Browser and external editing",
    summary: "Completes the File Browser workflow, adds native .nw/.gmap editing and makes GraalScript highlighting follow RC themes.", current: true,
    changes: [
      "Double-clicking a .nw or .gmap file downloads it to the configured folder, opens it with the operating system's default application and uploads local edits when the server grants write access.",
      "Thumbnail previews are serialized through the native File Browser transfer channel, keep preview protocol messages out of the visible log and never fetch images at or above 500 KB.",
      "GraalScript theme definitions now cover the full set of RC themes, while Script Manager refreshes coalesce concurrent cache events into a single native request.",
      "External file sessions are detached safely when the RC disconnects, logs out or switches servers, preventing edits from being sent to the wrong session.",
    ],
  },
  {
    version: "3.1.8", date: "2026-08-16", title: "File Browser workflow and themed startup",
    summary: "Adds faster file handling, external editor integration and removes the custom-theme flash when opening RC windows.", current: false,
    changes: [
      "File Browser now shows the file count for the current folder and supports selecting multiple files for bulk downloads and moves.",
      "Optional image thumbnails can be enabled in Settings; only images smaller than 500 KB are downloaded and displayed, while larger images are never fetched for previews.",
      "Settings now supports VS Code, Sublime Text and Notepad++ as external editors, with an Open with external editor action in script context menus.",
      "The folder pane in File Browser can be resized and its width can be reset with a double-click or keyboard controls.",
      "New windows apply the active custom theme before showing their content, eliminating the brief default-theme flash during startup.",
    ],
  },
  {
    version: "3.1.7", date: "2026-08-16", title: "Stronger macOS and Linux distribution",
    summary: "Fixes cross-platform packaging, the macOS icon and the RC's dependency on an open terminal.", current: false,
    changes: [
      "The macOS build now ships an app bundle with the correct icns icon and version metadata; the x64 artifact runs natively on Intel Macs and through Rosetta 2 on Apple Silicon.",
      "The Linux AppImage now carries the GTK4/WebKitGTK6 runtime and native grclib.so; DEB/RPM packages also install the required library.",
      "The RC no longer depends on an open terminal to keep running, and the macOS updater uses the new tar.gz archive.",
      "All three installers use canonical names and metadata aligned with version 3.1.7.",
    ],
  },
  {
    version: "3.1.6", date: "2026-08-15", title: "More controllable script and Sync workflow",
    summary: "Improves Script Manager, visual customization and Sync notification control.", current: false,
    changes: [
      "Script Manager can now open any synchronized script directly in the native file browser on Windows, macOS or Linux from its context menu.",
      "Sync progress notifications can be minimized or dismissed while the initial download continues in the background.",
      "The GraalScript linter no longer requires a semicolon after enum declarations.",
      "Settings now lets users create, edit and select RC themes, including the window border color.",
    ],
  },
  {
    version: "3.1.5", date: "2026-08-15", title: "Definitive Windows updater and visible version",
    summary: "Fixes the final Windows auto-update flow and shows the version on the login screen.", current: false,
    changes: [
      "The Windows automatic-update helper now starts through the Windows shell broker with elevation before the RC closes, waits for the process, installs the update and relaunches the application.",
      "The login screen now shows the current RC version in its title to make the installed build easy to confirm.",
    ],
  },
  {
    version: "3.1.4", date: "2026-08-15", title: "Custom window frame and NightOwl theme",
    summary: "Improves link windows and adds a dedicated theme for GS2 development.", current: false,
    changes: [
      "Links opened from RC Chat now use the same custom frame, window controls and internal WebView as the rest of the RC windows.",
      "Added the NightOwl theme to Monaco with highlighting aligned to GS2 syntax tokens.",
    ],
  },
  {
    version: "3.1.3", date: "2026-08-15", title: "Updater and cross-platform manual downloads",
    summary: "Fixes Windows auto-update shutdown and lets macOS/Linux users save verified updates for manual installation.", current: false,
    changes: [
      "The Windows automatic-update helper now starts detached, has a bounded wait and force-exits the RC when necessary to release the installer.",
      "macOS and Linux updates now show a native save dialog for the verified artifact instead of opening an installer automatically.",
      "The release API now recognizes Windows, Linux and macOS targets and advertises downloads only when the matching artifact is published.",
      "Backups for files, scripts, databases and server text now use a per-target configurable limit, with 3 versions by default and automatic replacement of the oldest.",
    ],
  },
  {
    version: "3.1.2", date: "2026-08-15", title: "Updater relaunch hotfix",
    summary: "The RC now opens again after the automatic installer finishes.", current: false,
    changes: [
      "The automatic-update helper now waits for the silent installer, records failures and relaunches the RC afterward so an update cannot leave the application closed.",
    ],
  },
  {
    version: "3.1.1", date: "2026-08-15", title: "Windows updater hotfix",
    summary: "Fixes the process check used by the installer during automatic updates.", current: false,
    changes: [
      "Fixed RC detection for installers launched by automatic updates; the installer now runs tasklist through a Unicode NSIS-compatible invocation.",
    ],
  },
  {
    version: "3.1.0", date: "2026-08-15", title: "Productivity and server-scoped context",
    summary: "A major update for faster GraalScript development and consistent isolation between RC instances and servers.", current: false,
    changes: [
      "Command macros are now saved on the PC per server, with automatic migration of macros created by older browser-only builds.",
      "Contextual completion now resolves objects returned by findplayer, findweapon and findlevel, imported classes, GUI controls and built-in objects assigned to variables.",
      "GS2 enums offer their internal names in completion and show explicit values only in suggestion details; new filters constructors while preserving new[size] array allocation.",
      "Function completion includes a gscript.dev wiki search link opened in an RC WebView; HTTP(S) links in RC Chat use the same internal window flow.",
      "Script Manager now focuses its filter with Ctrl+F, Ctrl+K opens weapons, classes and NPCs after the first sync, and newly created weapons wait one second before the automatic refresh.",
      "The Monaco editor supports tab widths 1, 2 or 4, with 2 as the default.",
      "File Browser adds folder search, F2 renaming for files only, and a consolidated download log with one entry per file.",
      "The RC supports multiple instances, places new windows on the instance's monitor, and puts the server first in secondary window titles.",
      "Disconnects close related windows and clear File Browser context before login or reconnecting to another server.",
      "The plugin creation screen now links directly to the official documentation on nullborne.com.",
    ],
  },
  {
    version: "3.0.1", date: "2026-08-15", title: "Automatic updates and connected release notes",
    summary: "The RC can now keep itself current and read the published release history from Nullborne's release service.", current: false,
    changes: [
      "Automatic update checks against https://nullborne.com/update when the RC starts.",
      "The Windows amd64 installer is downloaded, verified and launched automatically before the older RC closes.",
      "The in-app changelog refreshes from https://nullborne.com/changelog and keeps the bundled notes as an offline fallback.",
      "Windows release metadata, installer size and SHA-256 are published by the update API.",
    ],
  },
  {
    version: "3.0.0", date: "2026-08-15", title: "Safer operations and GraalScript development",
    summary: "A major update for server administration, script development and safe change recovery.",
    changes: [
      "Global search, an operational notification center and shortcuts to the main tools while keeping RC Chat as the primary surface.",
      "Parameterized command macros with parameter types, validation and fast slash-command-style filling.",
      "Player-list moderation workflow with permission-gated actions and automatic account filling for the logged-in user.",
      "Safer Sync with rights loaded at login, write-account enforcement, initial-sync progress, a 60-minute default poll and panic-mode recovery.",
      "Local audit history and backups for synchronized scripts, with up to three versions per script, configurable retention, deletion and rollback.",
      "Contextual GraalScript 2 LSP, refreshed only after Sync completes, plus local MCP and a plugin engine for automation.",
      "Cleaner RC Chat with repeated-text aggregation, preserved blank lines, complete localization and corrected permission comparisons.",
      "Script Manager and File Browser improvements that ignore unnamed scripts, reject invalid paths and provide fallbacks for missing names or icons.",
    ],
  },
  {
    version: "2.0.0", date: "2026-08-01", title: "Themes and workflow",
    summary: "An update focused on customization, synchronization and productivity.",
    changes: [
      "Modernized local sync, localization and private-message alerts.",
      "More complete editor themes, including custom theme previews.",
      "RC Chat autocomplete for commands and players.",
      "Frameless windows and session nickname persistence.",
      "Redesigned Settings and improved login and profile flows.",
    ],
  },
  {
    version: "1.2.1", date: "2026-07-28", title: "Startup and connection",
    summary: "Improvements for clearer startup and a more reliable NC connection.",
    changes: ["Frontend loading skeleton.", "NC connection keep-alive."],
  },
  {
    version: "1.2.0", date: "2026-07-24", title: "Cross-platform builds",
    summary: "RC distribution expanded to more environments.",
    changes: ["Fixed the cross-platform macOS Intel build."],
  },
  {
    version: "1.1.0", date: "2026-07-24", title: "Automated releases",
    summary: "The release pipeline was prepared for broader platform support.",
    changes: ["Added the macOS target and automatic tag creation to the release workflow."],
  },
  {
    version: "1.0.0", date: "2026-07-23", title: "First stable release",
    summary: "The first stable version of Graal Remote Control.",
    changes: ["Initial release and distribution pipeline."],
  },
  {
    version: "0.1.0", date: "2026-07-22", title: "First public version",
    summary: "The initial foundation for administering Graal servers.",
    changes: ["Synchronized the RCNPC model with grclib.", "Displayed NPC levels in the interface."],
  },
]

const CHANGELOG_ES: ChangelogEntry[] = [
  {
    version: "3.2.0", date: "2026-08-17", title: "Cross-platform File Browser and external editing",
    summary: "Completes the File Browser workflow, adds native .nw/.gmap editing and makes GraalScript highlighting follow RC themes.", current: true,
    changes: [
      "Double-clicking a .nw or .gmap file downloads it to the configured folder, opens it with the operating system's default application and uploads local edits when the server grants write access.",
      "Thumbnail previews are serialized through the native File Browser transfer channel, keep preview protocol messages out of the visible log and never fetch images at or above 500 KB.",
      "GraalScript theme definitions now cover the full set of RC themes, while Script Manager refreshes coalesce concurrent cache events into a single native request.",
      "External file sessions are detached safely when the RC disconnects, logs out or switches servers, preventing edits from being sent to the wrong session.",
    ],
  },
  {
    version: "3.1.7", date: "2026-08-16", title: "Distribución macOS y Linux reforzada",
    summary: "Corrige el empaquetado multiplataforma, el icono de macOS y la dependencia del RC de un terminal abierto.", current: false,
    changes: [
      "La build de macOS ahora incluye un app bundle con el icono icns y los metadatos de versión correctos; el artefacto x64 funciona de forma nativa en Macs Intel y mediante Rosetta 2 en Apple Silicon.",
      "El AppImage de Linux ahora incluye el runtime GTK4/WebKitGTK6 y el grclib.so nativo; los paquetes DEB/RPM también instalan la biblioteca requerida.",
      "El RC ya no depende de un terminal abierto para seguir ejecutándose y el actualizador de macOS usa el nuevo archivo tar.gz.",
      "Los tres instaladores usan nombres canónicos y metadatos alineados con la versión 3.1.7.",
    ],
  },
  {
    version: "3.1.6", date: "2026-08-15", title: "Flujo de scripts y Sync más controlable",
    summary: "Mejora Script Manager, la personalización visual y el control de las notificaciones de Sync.", current: false,
    changes: [
      "Script Manager ahora permite abrir cualquier script sincronizado directamente en el explorador de archivos nativo de Windows, macOS o Linux desde su menú contextual.",
      "Las notificaciones de progreso de Sync se pueden minimizar o descartar mientras la descarga inicial continúa en segundo plano.",
      "El linter de GraalScript ya no exige punto y coma después de las declaraciones de enum.",
      "Settings ahora permite crear, editar y seleccionar temas del RC, incluida la color de borde de las ventanas.",
    ],
  },
  {
    version: "3.1.5", date: "2026-08-15", title: "Actualizador definitivo de Windows y versión visible",
    summary: "Corrige el flujo final de actualización automática de Windows y muestra la versión en la pantalla de inicio de sesión.", current: false,
    changes: [
      "El helper de actualización automática de Windows ahora se inicia mediante el broker del sistema con elevación antes de cerrar el RC, espera al proceso, instala la actualización y vuelve a abrir la aplicación.",
      "La pantalla de inicio de sesión ahora muestra la versión actual del RC en el título para confirmar fácilmente el build instalado.",
    ],
  },
  {
    version: "3.1.4", date: "2026-08-15", title: "Marco personalizada y tema NightOwl",
    summary: "Mejora las ventanas de enlaces y añade un tema dedicado para el desarrollo GS2.", current: false,
    changes: [
      "Los enlaces abiertos desde RC Chat ahora usan el mismo marco personalizado, controles de ventana y WebView interno que el resto de las ventanas del RC.",
      "Se añadió el tema NightOwl a Monaco, con resaltado alineado con los tokens de sintaxis de GS2.",
    ],
  },
  {
    version: "3.1.3", date: "2026-08-15", title: "Actualizador y descargas manuales multiplataforma",
    summary: "Corrige el cierre de la actualización automática en Windows y permite guardar actualizaciones de macOS/Linux para instalarlas manualmente.", current: false,
    changes: [
      "El helper de actualización automática de Windows ahora se inicia separado, tiene un tiempo de espera limitado y fuerza el cierre del RC cuando es necesario para liberar el instalador.",
      "Las actualizaciones de macOS y Linux ahora muestran un diálogo nativo para elegir dónde guardar el archivo validado, sin abrir el instalador automáticamente.",
      "La API de releases ahora reconoce los destinos Windows, Linux y macOS y solo anuncia la descarga cuando el artefacto correspondiente está publicado.",
      "Los backups de archivos, scripts, bases de datos y textos del servidor ahora usan un límite configurable por objetivo, con 3 versiones por defecto y reemplazo automático de la más antigua.",
    ],
  },
  {
    version: "3.1.2", date: "2026-08-15", title: "Hotfix de relanzamiento del actualizador",
    summary: "El RC vuelve a abrirse después de que termina el instalador automático.", current: false,
    changes: [
      "El helper de actualización automática ahora espera al instalador silencioso, registra fallos y relanza el RC al finalizar para evitar que quede cerrado.",
    ],
  },
  {
    version: "3.1.1", date: "2026-08-15", title: "Hotfix del actualizador de Windows",
    summary: "Corrige la verificación de procesos usada por el instalador durante las actualizaciones automáticas.", current: false,
    changes: [
      "Corregida la detección del RC para instaladores iniciados por actualizaciones automáticas; el instalador ahora ejecuta tasklist con una invocación compatible con NSIS Unicode.",
    ],
  },
  {
    version: "3.1.0", date: "2026-08-15", title: "Productividad y contexto por servidor",
    summary: "Una gran actualización para acelerar el desarrollo GraalScript y mantener aisladas las instancias y los servidores.", current: false,
    changes: [
      "Las macros de comandos ahora se guardan en el PC por servidor, con migración automática de las macros de versiones antiguas.",
      "El autocompletado contextual resuelve objetos de findplayer, findweapon y findlevel, clases importadas, GUIs y objetos built-in asignados a variables.",
      "Los enums GS2 completan sus nombres internos y muestran los valores explícitos solo en el detalle; new filtra constructores y conserva new[size] para arrays.",
      "El autocompletado de funciones incluye un enlace a la búsqueda de gscript.dev dentro de una ventana WebView del RC; los enlaces HTTP(S) del chat usan el mismo flujo interno.",
      "Script Manager enfoca el filtro con Ctrl+F, Ctrl+K abre weapons, clases y NPCs después del primer sync, y las weapons nuevas esperan un segundo antes del refresh automático.",
      "El editor Monaco permite elegir tab width 1, 2 o 4, con 2 como valor predeterminado.",
      "File Browser añade búsqueda de carpetas, renombrado con F2 solo para archivos y un log de descargas consolidado con una entrada por archivo.",
      "El RC admite varias instancias, abre las ventanas nuevas en el monitor de la instancia y coloca el servidor primero en los títulos.",
      "Las desconexiones cierran las ventanas relacionadas y limpian el contexto del File Browser antes del login o de reconectar a otro servidor.",
      "La pantalla de creación de plugins enlaza directamente con la documentación oficial en nullborne.com.",
    ],
  },
  {
    version: "3.0.1", date: "2026-08-15", title: "Actualizaciones automáticas y notas conectadas",
    summary: "El RC ahora puede mantenerse actualizado y leer el historial publicado por el servicio de releases de Nullborne.", current: false,
    changes: [
      "Comprobación automática de actualizaciones en https://nullborne.com/update al iniciar el RC.",
      "Descarga, validación y ejecución automáticas del instalador Windows amd64 antes de cerrar la versión anterior.",
      "El changelog dentro del RC se actualiza desde https://nullborne.com/changelog y conserva las notas locales como fallback offline.",
      "Metadatos del release Windows, tamaño del instalador y SHA-256 publicados por la API de actualización.",
    ],
  },
  {
    version: "3.0.0", date: "2026-08-15", title: "Operaciones seguras y desarrollo GraalScript",
    summary: "Una gran actualización para administrar servidores, desarrollar scripts y recuperar cambios de forma segura.",
    changes: [
      "Búsqueda global, centro de notificaciones operativas y atajos para las herramientas principales, manteniendo RC Chat como pantalla principal.",
      "Macros de comandos parametrizadas, con tipos de parámetro, validación y llenado rápido al estilo slash command.",
      "Flujo de moderación integrado en la lista de jugadores, con acciones protegidas por permisos y cuenta del usuario conectado completada automáticamente.",
      "Sync más seguro con permisos cargados al iniciar sesión, bloqueo para cuentas con escritura, progreso del primer sync, poll predeterminado de 60 minutos y recuperación del panic mode.",
      "Historial local de auditoría y backups para scripts sincronizados, con hasta tres versiones por script, retención configurable, borrado y rollback.",
      "LSP contextual para GraalScript 2, actualizado solo después de terminar el Sync, además de MCP local y motor de plugins para automatización.",
      "RC Chat más limpio con agrupación de textos repetidos, conservación de líneas en blanco, localización completa y comparación corregida de permisos.",
      "Mejoras en Script Manager y File Browser para ignorar scripts sin nombre, rechazar rutas inválidas y mostrar fallbacks cuando faltan nombres o iconos.",
    ],
  },
  {
    version: "2.0.0", date: "2026-08-01", title: "Temas y flujo de trabajo",
    summary: "Una actualización centrada en personalización, sincronización y productividad.",
    changes: [
      "Sync local modernizado, localización y alertas de mensajes privados.",
      "Temas del editor más completos, incluyendo vista previa de temas personalizados.",
      "Autocompletado de comandos y jugadores en RC Chat.",
      "Ventanas sin marco y persistencia del apodo de la sesión.",
      "Settings rediseñada y mejoras en los flujos de inicio de sesión y perfil.",
    ],
  },
  {
    version: "1.2.1", date: "2026-07-28", title: "Inicio y conexión",
    summary: "Mejoras para abrir el RC con más claridad y mantener disponible el NC.",
    changes: ["Skeleton de carga del frontend.", "Keep-alive para la conexión NC."],
  },
  {
    version: "1.2.0", date: "2026-07-24", title: "Build multiplataforma",
    summary: "La distribución del RC llegó a más entornos.",
    changes: ["Corrección del build multiplataforma para macOS Intel."],
  },
  {
    version: "1.1.0", date: "2026-07-24", title: "Releases automatizados",
    summary: "El pipeline de release quedó preparado para ampliar el soporte de plataformas.",
    changes: ["Añadido el destino macOS y la creación automática de tags en el workflow de release."],
  },
  {
    version: "1.0.0", date: "2026-07-23", title: "Primer release estable",
    summary: "La primera versión estable de Graal Remote Control.",
    changes: ["Pipeline inicial de release y distribución de la aplicación."],
  },
  {
    version: "0.1.0", date: "2026-07-22", title: "Primera versión pública",
    summary: "La base inicial del RC para administrar servidores Graal.",
    changes: ["Sincronización del modelo RCNPC con grclib.", "Visualización del nivel de los NPCs en la interfaz."],
  },
]

export function getChangelog(language: Language): ChangelogEntry[] {
  if (language === "pt-BR") return CHANGELOG_PT_BR
  return language === "es" ? CHANGELOG_ES : CHANGELOG_EN
}
import type {Language} from "@/hooks/useLanguage"
