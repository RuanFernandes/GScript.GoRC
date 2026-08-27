# Graal Remote Control 4.2.0

## Plugin workspace tabs

- Plugins can register, update, open, and close first-class tabs in the main RC workspace.
- Tabs use a safe declarative view tree and support stacks, rows, cards, text, headings, badges, dividers, empty states, buttons, inputs, textareas, checkboxes, selects, progress indicators, code blocks, and tables.
- Plugin controls send typed actions back through the sandbox, and tabs can be updated without injecting HTML or accessing the RC DOM.
- The generated plugin template now includes a working tab example and requests the `ui.tabs` permission.
- Host-side validation bounds tab and window views while keeping the existing `ui.windows` API compatible.

## Connection and scripting stability

- NC recovery now refreshes the dedicated NPC-server endpoint before attempting to reconnect after an observed drop.
- Automatic recovery is limited to one attempt per observed drop, then becomes eligible again only after NC has successfully returned.
- Opening a weapon, class, or NPC script now waits for the restored NC session instead of failing immediately with a misleading disconnected error.

## Compatibility

- The plugin API remains version 1; existing plugins continue to use their approved capabilities.
- Release artifacts cover Windows x64/x86, Linux x64/386, and macOS x64/arm64.
