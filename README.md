# Cloudforge

Plataforma self-hosted para executar aplicações Python e Node.js no seu computador. Electron administra projetos, arquivos, logs, métricas e Cloudflare. Um Core independente em Go mantém os processos ativos. A dashboard web autenticada permite somente visualizar, iniciar, parar e reiniciar.

## Executar no Windows

Abra `release/0.1.4/win-unpacked/Cloudforge.exe` para executar a versão local diretamente da pasta do pacote. Para instalar o aplicativo, use `release/0.1.4/Cloudforge Setup 0.1.4.exe`. O arquivo `.sha256` permite verificar a integridade do instalador. As duas opções usam o SQLite local e dispensam PostgreSQL.

Na primeira abertura:

1. Confira a verificação real da máquina.
2. Crie seu administrador, com senha de pelo menos 12 caracteres. Não existem credenciais padrão.
3. Escolha conectar a Cloudflare ou configurar posteriormente.
4. Em Projetos, escolha uma pasta existente, confirme o runtime e o comando de execução e autorize os scripts daquele projeto confiável.
5. Inicie a aplicação e acompanhe seu estado e logs. Fechar o Electron mantém o Core e os projetos ativos.

Python é necessário somente para projetos Python; Node.js e o gerenciador correspondente, somente para projetos Node.js. O pacote não instala esses runtimes globais silenciosamente.

## Desenvolvimento

Requisitos: Node.js 24 LTS, npm e Go 1.24 ou superior. Um SDK portátil em `.tools/go` é reconhecido pelos scripts, quando presente.

```powershell
npm ci
npm run dev
```

`npm ci` baixa o executável oficial do Electron pelo instalador do pacote. `npm run dev` compila a web e o Core, inicia o Vite administrativo e abre o Electron. O Core continua independente quando a interface fecha. Para reiniciar o Core após alterações em Go, encerre a instância anterior antes de executar uma nova compilação. Nunca publique as portas administrativas.

```powershell
npm run typecheck
npm run lint
npm test
npm run build
npm run package:dir
npm run package
```

`package:dir` gera a pasta executável. `package` gera o instalador NSIS no Windows ou AppImage no Linux. Os binários não possuem assinatura de distribuição; a assinatura exige certificados do mantenedor e não é simulada.

## Cloudflare

1. Clique em **Conectar com a Cloudflare**. O navegador padrão abre a página oficial de login.
2. Entre na sua conta, selecione um domínio ativo na Cloudflare e autorize o acesso. Volte ao Cloudforge.
3. Escolha o subdomínio do painel, por exemplo `painel`. Se a Cloudflare não informar o nome da zona, digite o mesmo domínio que autorizou no navegador. Confira o endereço e clique em **Criar endereço público**.
4. Clique em **Verificar acesso HTTPS** para confirmar que o painel responde pelo domínio. Para publicar uma aplicação, abra o projeto, escolha **Subdomínio** e informe o nome e a porta HTTP local.

O pacote Windows x64 já inclui o `cloudflared` oficial com SHA-256 verificado. Em outros ambientes, o Core reutiliza a instalação existente ou baixa uma versão oficial verificada nas arquiteturas compatíveis. O login gera um certificado no perfil privado do Cloudforge e as novas conexões usam um túnel gerenciado localmente. Túneis já configurados pela API continuam usando suas credenciais originais.

Em **Cloudflare**, use **Alterar endereço do painel**. Para usar outro domínio no mesmo túnel local, autorize-o na mesma conta Cloudflare com **Autorizar outro domínio**. Em cada projeto, **Subdomínio** permite alterar ou remover seu endereço público. Na troca ou remoção, o Cloudforge só exclui o CNAME anterior se ele ainda apontar para o túnel da instalação. Se a limpeza falhar, o app informa qual registro ficou pendente.

Em **Projetos → Configurações → Porta HTTP**, informe a porta que o servidor realmente usa. O Cloudforge atualiza também o destino do subdomínio. Um código que fixa a porta em `80` precisa ser cadastrado com `80`. Aplicações que leem a variável `PORT` passam a usar o novo valor ao reiniciar. Projetos sem HTTP podem continuar sem porta e sem endereço público.

Em **Projetos → Configurações → Excluir projeto**, confirme o encerramento e a remoção do cadastro, dos logs e da rota pública. A pasta original, incluindo dependências instaladas dentro dela, é preservada. Você pode cadastrar a mesma pasta novamente.

Consulte o [fluxo oficial de túnel gerenciado localmente](https://developers.cloudflare.com/tunnel/features/locally-managed-tunnels/create-local-tunnel/).

## Processos e dependências

- Python: cria ou reutiliza `.venv`, `venv` ou `env`, valida o interpretador, instala requirements pelo pip da venv e registra SHA-256 e identidade do ambiente somente após sucesso.
- Node.js: detecta npm, pnpm e Yarn pelo lockfile e configuração; usa `npm ci` com package-lock ou instalação normal sem lock. Scripts e ferramentas são executados por argumentos explícitos, sem shell genérico.
- O cache considera manifesto, lockfile, versões e validade básica do ambiente. Falhas de instalação impedem o início do projeto.
- O supervisor diferencia estado desejado e real, permite políticas nunca/em falha/sempre, registra encerramentos e aplica backoff de até 64 segundos.
- Windows usa Job Objects; Linux usa grupos de processos. A parada tenta sinalização controlada antes de encerrar a árvore. Aplicações Windows sem console podem exigir a etapa de encerramento forçado após o prazo.
- Logs capturam stdout/stderr, preservam histórico limitado e giram arquivos. O painel é somente leitura, com pesquisa, cópia e limpeza da visualização.
- Métricas de projetos somam processo principal e filhos detectados. RAM usa RSS e pode contar memória compartilhada. Valores indisponíveis aparecem como tal.

## Inicialização automática

Ative em Configurações, mediante confirmação. No Windows, utiliza a chave Run do usuário atual; no Linux, um serviço systemd de usuário. Essa escolha preserva a identidade do usuário e o acesso ao armazenamento de segredos. Não é instalado um serviço como LocalSystem nem são solicitadas credenciais do Windows.

O início automático ocorre na sessão do usuário. Execução antes de login e Linux com linger exigem configuração operacional adicional e não são ativados silenciosamente. Reinício físico da máquina e essas configurações dependem de validação no ambiente de destino.

## Dados e recuperação

Windows: `%APPDATA%\Cloudforge`. Linux: `${XDG_CONFIG_HOME:-~/.config}/Cloudforge`.

O diretório contém SQLite/WAL, chave administrativa local, logs, conector e perfil Electron. Mantenha esse diretório privado. No modo de login pelo navegador, `cloudflare-browser` guarda o certificado da conta e a credencial do túnel. Túneis remotos já configurados usam o armazenamento de segredos do sistema. Para um backup consistente, pare o Core antes de copiar os dados ou use a API de backup do SQLite. Os arquivos de projetos continuam nas pastas originais e precisam de backup próprio.

Uma trava impede dois Cores no mesmo diretório. O estado desejado é restaurado ao iniciar. Processos anteriores detectados não são duplicados sem verificação. Operações Cloudflare têm um diário e recuperação de alterações parciais; a interface informa quando é preciso usar Recuperar operações.

Para desenvolvimento isolado, o Electron aceita `CLOUDFORGE_DATA`. O Core aceita `--data`, `--web-dir`, `--admin` e `--public`, com bind obrigatório em loopback. O sinalizador `--development` permite cookies HTTP exclusivamente para a verificação local da web; a execução normal usa cookies Secure e o hostname administrativo HTTPS configurado.

## Segurança e limites

As aplicações executam com os privilégios do usuário do Core. Execute somente código próprio e confiável. Não há isolamento equivalente a containers. Não exponha a API administrativa, que exige chave local e rejeita chamadas com Origin de navegador. A dashboard pública tem outra superfície de endpoints e não expõe diretórios, arquivos ou tokens.

Sessões usam cookies HttpOnly, Secure e SameSite Strict em produção, tokens opacos com hash no banco, expiração em 30 dias, rotação, logout, revogação, rate limiting, CSRF e validação de origem. A troca de senha revoga todas as sessões. O Electron isola o renderer, bloqueia navegação externa e valida IPC.

SQLite é o adaptador inicial. O contrato `persistence.Repository` permite um futuro adaptador PostgreSQL; esse adaptador e o executor Docker não estão implementados nesta versão. A licença do próprio Cloudforge permanece uma decisão do mantenedor antes da distribuição open source. Avisos de terceiros devem ser preservados.

## Estrutura

```text
apps/desktop/       React + Electron + preload restrito
apps/web/           Dashboard remota independente
services/core/      API, autenticação, supervisor, runtimes, proxy e Cloudflare
packages/           Tipos compartilhados e componentes/estilos
scripts/            Desenvolvimento, build, testes e fixtures de QA
docs/               Arquitetura, referência e evidências de validação
```

Veja [arquitetura](docs/architecture.md), [análise da referência](docs/reference-review.md) e [validação](docs/validation.md).
