# Teste da branch `ruan/rc-stability`

As mudanças são locais ao cliente e usam o protocolo RC existente. Não há replay automático de comandos, mensagens, uploads ou alterações administrativas.

## Executável de teste

O build Windows fica em `graal-rc/bin/rc-stability/nullborne-rc-stability.exe`, acompanhado de `grclib64.dll`. A pasta é independente do instalador. O programa continua usando as configurações da sua conta de usuário; feche a versão instalada antes de abrir o build de teste.

## O que mudou

- **Sync incompleto:** uma falha em qualquer download invalida o snapshot. O sync mostra o erro, preserva os arquivos locais e não avança a geração de sincronização. A inicialização tenta novamente com espera crescente, até 30 segundos entre tentativas.
- **Rascunhos:** scripts, configurações e textos editáveis mantêm um journal local imediato e uma cópia em disco após 250 ms sem alterações. Os rascunhos são identificados por conta, listserver, endpoint do servidor e recurso. Um texto remoto alterado é apresentado para comparação ao recuperar o rascunho. A restauração não envia nada ao servidor.
- **Proteção da sessão:** leituras, saves e conflitos dos editores usam tokens vinculados à sessão de abertura. Uma janela antiga não pode salvar seu conteúdo em uma nova sessão. Finalizar um save também não apaga texto digitado enquanto o upload estava em andamento.
- **Cancelamento:** uma queda invalida a sessão e cancela esperas diretamente no backend. Downloads aguardando a fila abandonam a sessão anterior. Cancelar só o chamador de um download ativo não libera prematuramente o único slot de transferência da DLL; ele é liberado pela resposta, desconexão ou timeout.
- **Reconexão principal:** até cinco tentativas para erros de transporte reconhecidos, com esperas de 1, 2, 4, 8 e 16 segundos e até 500 ms de variação. Cada tentativa possui contexto de 30 segundos. Cancelar, sair ou iniciar login manual encerra a recuperação anterior. O servidor é localizado por nome único na lista recém-autenticada, nunca pelo índice antigo.
- **Reconexão NC:** até quatro novas tentativas com espera crescente e variação aleatória. Uma conexão que oscila brevemente não restaura imediatamente todo o orçamento de tentativas. Desconexão manual e rejeições conhecidas não disparam reconexão automática.
- **Rajadas:** uma fila de até 512 recursos agrupa alterações repetidas. Se atingir o limite ou houver pausa, mantém uma reconciliação completa pendente até ela conseguir terminar. Parar o engine impede um worker antigo de consumir a fila da próxima execução.
- **Biblioteca nativa:** diagnóstico de chamadas e espera no mutex; watchdog independente registra chamadas acima de cinco segundos, com repetição limitada. Ele continua observando durante o encerramento do aplicativo. Argumentos e credenciais não entram nessas métricas.
- **Backups:** gravações rápidas não dependem mais da resolução do relógio para gerar identificadores únicos. Arquivos existentes não são sobrescritos, e a retenção preserva as versões mais recentes mesmo quando os timestamps coincidem.

## Roteiro manual

Use scripts e uma pasta de sync destinados ao teste.

1. Abra um script e um arquivo de texto, altere ambos sem salvar e interrompa a conexão. Reconecte, reabra os mesmos recursos e confira os textos recuperados. Repita fechando o aplicativo antes do debounce terminar.
2. Edite um script, inicie um save e continue digitando antes da resposta. O texto novo deve continuar marcado como não salvo e recuperável após reabrir.
3. Recupere um rascunho cujo conteúdo remoto foi alterado por outro cliente. Confira o aviso e a comparação antes de decidir salvar ou descartar.
4. Interrompa uma sincronização e verifique que o erro aparece e nenhum arquivo local é removido devido a download ausente. Depois de restaurar a conexão, execute Re-Sync e confira a conclusão.
5. Interrompa a rede durante downloads e restaure-a. A interface deve permitir cancelar a recuperação; pedidos enfileirados da sessão antiga não devem executar no servidor reconectado.
6. Desconecte o NC pelo comando da interface. Ele deve continuar desconectado até uma ação manual. Em uma queda de transporte, confira as tentativas espaçadas no log.
7. Durante uma reconexão, clique em Cancelar. A tela deve voltar ao login e não iniciar outra conexão sozinha. Reabra um editor após login para recuperar o rascunho.
8. Gere várias atualizações no ambiente de teste e verifique que a interface continua recebendo chat enquanto o sync processa a fila. Pause e retome o sync; as alterações pendentes devem ser reconciliadas.
9. Faça saves rápidos no mesmo recurso e confira o histórico: ele deve manter as últimas versões até o limite configurado, sem perder backups por colisão de identificadores.

## Validação automatizada

Na pasta `graal-rc`:

```powershell
go test ./...
go vet ./...
wails3 generate bindings -ts -i
```

Na pasta `graal-rc/frontend`:

```powershell
npm.cmd test
npm.cmd run build
```

O detector de corridas exige um compilador C compatível no Windows. Nesta máquina, a execução de `go test -race` ficou bloqueada porque `gcc` não está disponível. Os testes de concorrência convencionais incluem interleavings controlados por canais para cancelamento, troca de sessão, snapshots, filas e revisão de rascunhos.

## Limites conhecidos

- O watchdog detecta bloqueios nativos; ele não interrompe uma chamada de DLL já em execução. Um deadline de Go também só será observado quando essa chamada retornar. Isolar grclib em um processo auxiliar é uma evolução separada, detalhada em `rclib/NATIVE_STABILITY.md`.
- A interface nativa não expõe confirmação da atualização do endpoint NC. A espera de compatibilidade de 500 ms permanece; as novas tentativas espaçadas reduzem o impacto dessa limitação.
- Motivos de desconexão desconhecidos, falhas do pump e rejeições de autenticação/permissão exigem login manual. Não se presume que toda expulsão seja uma falha de rede.
- Rascunhos têm limite de 4 MiB por texto e 128 MiB no armazenamento em disco. Falhas de quota, persistência ou corrupção são mostradas; um arquivo de rascunho corrompido não é sobrescrito silenciosamente.
- Não foram executadas operações em uma sessão RC real durante o desenvolvimento. Os testes de rede real do roteiro acima ficam para a validação desta branch.
