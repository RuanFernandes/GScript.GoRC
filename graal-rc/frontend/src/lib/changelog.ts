export interface ChangelogEntry {
  version: string
  date: string
  title: string
  summary: string
  changes: string[]
  current?: boolean
}

// Keep release notes in the client so the changelog is available offline and
// remains tied to the versioned RC source instead of a remote service.
const CHANGELOG_PT_BR: ChangelogEntry[] = [
  {
    version: "3.0.0",
    date: "15/08/2026",
    title: "Operações seguras e desenvolvimento GraalScript",
    summary: "Uma grande atualização para administrar servidores, desenvolver scripts e recuperar alterações com segurança.",
    current: true,
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
    version: "3.0.0", date: "2026-08-15", title: "Safer operations and GraalScript development",
    summary: "A major update for server administration, script development and safe change recovery.", current: true,
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
    version: "3.0.0", date: "2026-08-15", title: "Operaciones seguras y desarrollo GraalScript",
    summary: "Una gran actualización para administrar servidores, desarrollar scripts y recuperar cambios de forma segura.", current: true,
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
