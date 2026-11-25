# README

## About

This is the official Wails React-TS template.

You can configure the project by editing `wails.json`. More information about the project settings can be found
here: https://wails.io/docs/reference/project-config

## Live Development

To run in live development mode, run `wails dev` in the project directory. This will run a Vite development
server that will provide very fast hot reload of your frontend changes. If you want to develop in a browser
and have access to your Go methods, there is also a dev server that runs on http://localhost:34115. Connect
to this in your browser, and you can call your Go code from devtools.

## Building

To build a redistributable, production mode package, use `wails build`.

# MCP local

Ao iniciar, o cliente expõe um servidor MCP HTTP somente no loopback em
`http://127.0.0.1:8765/mcp`. O endpoint implementa `initialize`, `tools/list` e
`tools/call`, com as ferramentas `get_rc_chat`, `send_rc_chat`,
`filebrowser_list`, `filebrowser_cd`, `filebrowser_search` e
`filebrowser_read`.

O endereço pode ser alterado com `RC_MCP_ADDR`. A integração é deliberadamente
local; não use `0.0.0.0` sem colocar autenticação e uma camada de rede segura na
frente dela. Para barrar páginas web, o endpoint recusa requisições com `Origin`, com `Host`
que não seja de loopback (ou o de `RC_MCP_ADDR`) e sem `Content-Type: application/json`. Isso não
autentica programas locais. `filebrowser_cd`, `filebrowser_list` e `filebrowser_search` só respondem depois que o
servidor entrega a listagem (limite de 20 segundos). `get_rc_chat` retorna no máximo 50 mensagens com timestamp UTC e `filebrowser_read`
recusa caminhos com traversal e conteúdo que não seja UTF-8.
