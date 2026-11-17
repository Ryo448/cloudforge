# Atualizações do aplicativo

O desktop instalado consulta as releases estáveis de `Ryo448/cloudforge` ao iniciar e a cada duas horas. O botão **Nova atualização** aparece na barra superior quando há uma versão mais recente. O download só começa após **Atualizar agora** no modal. O modal mostra percentual, tamanho e velocidade; ao concluir, encerra o Core, instala em modo silencioso e reabre o aplicativo. Falhas permitem tentar novamente. Arquivos com alterações pendentes precisam ser salvos ou descartados antes de atualizar.

O mecanismo usa `electron-updater`, com verificação de integridade pelo SHA-512 dos metadados gerados pelo `electron-builder`. O download e a instalação não são iniciados ao simplesmente consultar uma release ou fechar o aplicativo. O modo de desenvolvimento e a pasta Windows sem instalação não são canais de distribuição para testar o fluxo; valide com o instalador NSIS no Windows ou com AppImage no Linux. O fluxo Linux precisa de validação em uma máquina Linux.

## Publicar uma versão

1. Atualize a versão em `package.json`, nos pacotes dos workspaces e em `package-lock.json`. A versão precisa ser maior que a instalada. O build injeta a versão de `apps/desktop/package.json` no Core.
2. Execute `npm test` e `npm run package` no sistema de destino. O comando gera os artefatos localmente e não publica no GitHub.
3. Crie uma release estável com a tag correspondente (por exemplo, `v2.2.0`) em `Ryo448/cloudforge` e anexe os arquivos gerados juntos, sem renomear os binários:
   - Windows: `Cloudforge-Setup-<versão>.exe`, seu `.blockmap` e `latest.yml`.
   - Linux: o `.AppImage` e `latest-linux.yml`.
4. Teste em uma instalação de versão anterior: aguarde a consulta inicial, abra **Nova atualização**, confirme e verifique o progresso, o instalador e a reabertura.

Releases antigas que contêm somente o instalador não oferecem os metadados necessários. Não coloque token do GitHub no aplicativo: o canal é a release pública configurada no empacotamento. Não publique metadados apontando para um instalador de outra versão ou plataforma.

## Compatibilidade do Core

Ao abrir ou reconectar, o desktop compara a versão da API, a versão do app e o SHA-256 do executável em execução com o Core incluído no pacote. Se houver diferença, encerra a instância antiga e inicia o binário atual. Isso também cobre uma recompilação com o mesmo número de versão. Fechar o desktop normalmente continua permitindo que o Core execute em segundo plano.

O endpoint local autenticado `POST /api/shutdown` permite a troca controlada, preservando o estado desejado dos projetos para recuperação na próxima inicialização. Ele não é exposto no painel público nem na lista genérica de rotas IPC do renderer. O desktop aguarda o processo sair antes de iniciar o substituto.

Para migrar um Core antigo sem esse endpoint, o desktop identifica o dono do listener local e confere o caminho exato do executável antes de encerrá-lo. No Windows, encerra também sua árvore de processos. A migração recusa encerrar um binário de outro caminho; isso evita atingir outra instalação.
