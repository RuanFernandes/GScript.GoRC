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
    version: "3.1.0",
    date: "Em desenvolvimento",
    title: "GraalScript e automação",
    summary: "A próxima geração do RC para desenvolvimento e administração de servidores Graal.",
    current: true,
    changes: [
      "MCP local para RC Chat e File Browser, com integração para Codex, Claude Code e OpenCode.",
      "LSP contextual para GraalScript 2, autocomplete, diagnósticos, assinatura e hover com JSDoc.",
      "Sync bidirecional com conflitos no editor, prioridade do servidor e criação automática de classes e weapons locais.",
      "Engine de plugins com APIs para scripts, arquivos, Monaco, janelas, notificações e automações.",
      "Novas melhorias de tokenização, temas Monaco, file browser e integração com o servidor.",
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
    version: "3.1.0", date: "In development", title: "GraalScript and automation",
    summary: "The next generation of RC for Graal server development and administration.", current: true,
    changes: [
      "Local MCP for RC Chat and File Browser, with Codex, Claude Code and OpenCode integration.",
      "Contextual GraalScript 2 LSP with autocomplete, diagnostics, signatures and JSDoc hover.",
      "Two-way sync with editor conflicts, server priority and automatic creation of local classes and weapons.",
      "Plugin engine with APIs for scripts, files, Monaco, windows, notifications and automation.",
      "New tokenization, Monaco themes, file browser and server integration improvements.",
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
    version: "3.1.0", date: "En desarrollo", title: "GraalScript y automatización",
    summary: "La próxima generación del RC para desarrollar y administrar servidores Graal.", current: true,
    changes: [
      "MCP local para RC Chat y File Browser, con integración para Codex, Claude Code y OpenCode.",
      "LSP contextual para GraalScript 2, autocompletado, diagnósticos, firmas y hover con JSDoc.",
      "Sync bidireccional con conflictos en el editor, prioridad del servidor y creación automática de clases y weapons locales.",
      "Motor de plugins con APIs para scripts, archivos, Monaco, ventanas, notificaciones y automatización.",
      "Nuevas mejoras de tokenización, temas de Monaco, file browser e integración con el servidor.",
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
