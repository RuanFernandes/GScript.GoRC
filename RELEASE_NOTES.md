# Graal Remote Control 5.2.3

## Distribuição Linux

- Adicionado instalador `.deb` para Ubuntu/Debian x64, mantendo o AppImage como opção universal para Linux.

# Graal Remote Control 5.2.2

## GraalScript LSP

- Corrigida a concatenação multilinha de strings com `NL`, sem exigir `;` antes de `NL`.
- Reduzido o tempo de validação em scripts grandes, agrupando alterações durante a digitação e evitando diagnósticos redundantes ao salvar.

# Graal Remote Control 5.2.1

## File Browser

- Corrigido o upload por seleção de arquivos para enviar cada arquivo à pasta remota aberta e evitar tentativas de download de arquivos inexistentes.
- Corrigido o drag and drop nativo: a indicação de drop é limpa ao soltar fora da janela, e arquivos soltos no File Browser são enviados sem travar a interface.
- Consolidado o progresso de uploads grandes em uma única mensagem por arquivo.

# Graal Remote Control 5.2.0

## Menções no RC Chat

- Detecta menções explícitas por account name e community name no RC Chat, canais IRC e mensagens do servidor.
- Exibe notificação nativa do sistema operacional quando o usuário é chamado, inclusive quando o RC está minimizado na tray ou quando o próprio usuário faz um ping.
- O toast do Windows usa o nome do servidor conectado e o ícone do aplicativo; a mensagem mencionada também recebe um destaque visual no chat.

# Graal Remote Control 5.1.0

## Chat e ações de jogadores

- Identificação automática de account name e community name no RC Chat, canais IRC e conversas PM.
- Card com informações do jogador ao passar o mouse e menu contextual com as mesmas ações da Player List.
- Adicionado o comando para copiar o account name diretamente pelo menu contextual.

# Graal Remote Control 5.0.1

## File Browser

- Adicionado fluxo de backup local somente leitura, com seleção hierárquica de pastas, progresso, cancelamento e tratamento de falhas.
- Isoladas as operações de listagem e transferência para impedir que backups concorrentes interfiram no File Browser normal.
- Incluídos testes para a árvore de pastas, transferências e encerramento seguro de operações durante a troca de sessão.

# Graal Remote Control 5.0.0

## Estabilidade e experiência de uso

- Adicionada recuperação controlada de conexão, com cancelamento seguro, proteção contra sessões antigas e preservação de rascunhos locais durante quedas do servidor.
- O Sync passou a lidar com falhas e rajadas de alterações sem apagar arquivos locais ou deixar workers da sessão anterior ativos.
- Melhorada a responsividade de janelas durante movimentação e resize: WebView2 não mantém renderização pesada de janelas ocultas, e polling de players/status não acumula chamadas concorrentes.

## Player List

- A lista agora mostra nickname, account, community name, level e ID em colunas próprias, mantendo as ações de moderação e mensagens privadas.
- O level do player também aparece no inspector e é preservado na separação entre admins e players.

## GraalScript LSP

- Functions inline e callbacks agora são reconhecidos pelo analisador de semicolons, evitando o falso erro `Expected ';'` em construções como `temp.var = function () { ... };`.
- O LSP continua validando semicolons ausentes dentro do corpo dessas funções.

## Operações nativas

- Mantidos diagnósticos de chamadas nativas, reconexão do NC e o keepalive alinhado ao cliente C++ sem consultas periódicas desnecessárias de flags do NPC.
