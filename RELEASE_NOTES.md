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
