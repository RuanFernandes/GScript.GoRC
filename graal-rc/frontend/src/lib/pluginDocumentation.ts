import {Browser} from "@wailsio/runtime"

export const OFFICIAL_PLUGIN_DOCUMENTATION_URL = "https://nullborne.com/#plugin-docs"

export async function openOfficialPluginDocumentation(): Promise<void> {
  try {
    await Browser.OpenURL(OFFICIAL_PLUGIN_DOCUMENTATION_URL)
  } catch {
    // Opening the external browser is best effort.
  }
}
