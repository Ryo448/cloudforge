![Mascote do Cloudforge em uma forja digital](assets/readme/cloudforge-hero.webp)

# Cloudforge

**Hospede aplicações Python, Node.js e bancos de dados locais no seu próprio computador.** O Cloudforge prepara dependências, executa processos reais, acompanha logs e métricas, provisiona bancos PostgreSQL, MySQL, MongoDB e Redis em Docker e, se você quiser acesso pela internet, publica o painel e as aplicações com Cloudflare Tunnel.

O aplicativo desktop administra tudo localmente. Um Core independente mantém os projetos e bancos ativos mesmo depois que a janela é fechada. O painel web autenticado permite acompanhar e controlar aplicações e bancos remotamente.

**Código aberto sob [licença MIT](LICENSE).** Windows e Linux · SQLite local · Docker para bancos · Cloudflare e Google Drive opcionais.

## Neste guia

- [Recursos](#recursos)
- [Como funciona](#como-funciona)
- [Comece a usar](#comece-a-usar)
- [Cadastre sua primeira aplicação](#cadastre-sua-primeira-aplicação)
- [Acesso remoto com Cloudflare](#acesso-remoto-com-cloudflare)
- [Operação diária](#operação-diária)
- [Bancos de dados locais](docs/databases.md)
- [Snapshots dos bancos de dados](docs/snapshots.md)
- [Atualizações e releases](docs/updates.md)
- [Desenvolvimento e empacotamento](#desenvolvimento-e-empacotamento)
- [Dados, segurança e limites](#dados-segurança-e-limites)
- [Contribuição e licença](#contribuição-e-licença)

## Recursos

| Recurso                 | O que faz                                                                                                                                                                                                                                                                       |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Projetos locais         | Cadastra uma pasta existente sem mover ou copiar o código. Detecta Python e Node.js.                                                                                                                                                                                            |
| Dependências            | Prepara `venv` e instala requisitos no Python; usa npm, pnpm ou Yarn conforme o projeto Node.js. Reutiliza o ambiente quando o cache ainda é válido.                                                                                                                            |
| Processos               | Inicia, para e reinicia; persiste o estado desejado e oferece políticas de reinício: nunca, em falha ou sempre, exceto após parada manual.                                                                                                                                      |
| Observabilidade         | Mostra estado, CPU, RAM, tempo de execução e logs de `stdout`/`stderr` (até 1.000 linhas com pesquisa e cópia). Terminal interativo (`xterm.js`) opcional em janela dedicada no desktop.                                                                                        |
| Arquivos e configuração | Navega na pasta, edita código com realce de sintaxe (CodeMirror) e pré-visualiza mídia (imagem, vídeo e áudio) no desktop; gerencia porta HTTP, arquivos `.env` ou variáveis criptografadas no cofre do sistema.                                                                |
| Bancos de dados locais  | Provisiona PostgreSQL, MySQL, MongoDB e Redis em containers Docker isolados, com limite de memória, TLS/mTLS (`.pem`, `.crt`, `.key`), DBeaver integrado no desktop (PostgreSQL e MySQL) e snapshots no Google Drive.                                                           |
| Publicação              | Usa Cloudflare Tunnel para dar um subdomínio HTTPS ao painel e, opcionalmente, a cada aplicação HTTP.                                                                                                                                                                           |
| Acesso remoto           | O painel web autenticado lista projetos e bancos de dados, permite iniciar, parar e reiniciar aplicações e gerenciar bancos (criar, controlar, ver métricas, credenciais e certificados). Arquivos, variáveis, terminal, snapshots, DBeaver e Cloudflare permanecem no desktop. |

## Como funciona

![O mascote liga os projetos locais ao navegador por um túnel](assets/readme/cloudforge-flow.webp)

```text
Seu computador
├─ Desktop (Electron + React) → cadastro, arquivos, variáveis, terminal, DBeaver, Drive e Cloudflare
├─ Core (Go)                  → API local, SQLite, supervisor, bancos Docker, snapshots e proxy
├─ Seus projetos              → Python ou Node.js nas pastas originais
└─ Bancos de dados (Docker)   → PostgreSQL, MySQL, MongoDB e Redis em containers isolados com TLS
                                  │
                                  ├─ Google Drive (opcional) → snapshots manuais e agendados
                                  └─ Cloudflare Tunnel (opcional)
                                     ├─ painel web autenticado
                                     └─ subdomínios das aplicações HTTP
```

O desktop conversa com a API administrativa do Core apenas em loopback (`127.0.0.1`). O Core prepara dependências, executa os projetos, gerencia os containers Docker dos bancos de dados, executa snapshots agendados e guarda o estado em SQLite. Fechar a janela não encerra o Core, os projetos nem os bancos. A interface web usa uma API pública separada, com escopo limitado; o túnel nunca expõe a API administrativa.

Cloudflare, Docker (para bancos e DBeaver) e Google Drive (para snapshots) são **opcionais** e exigidos apenas se você usar esses recursos. Para acesso remoto pela internet, você precisa de uma conta Cloudflare e de um domínio ativo nela. Projetos sem servidor HTTP, como bots e tarefas de longa duração, podem ser executados sem porta e sem subdomínio.

## Comece a usar

### Instalação para uso diário

No Windows, abra o instalador `Cloudforge-Setup-<versão>.exe` gerado ou distribuído pelo projeto. A pasta `win-unpacked` também contém uma versão executável sem instalação. No Linux, o empacotamento gera um AppImage; a execução nesse sistema ainda precisa ser validada no ambiente de destino. Os artefatos de `release/` são gerados localmente e não fazem parte do código-fonte.

O aplicativo instalado verifica atualizações no GitHub ao abrir e a cada duas horas. Quando houver uma versão nova, use **Nova atualização** na barra superior para confirmar, acompanhar o download e instalar. O Core é conferido a cada abertura e substituído automaticamente quando pertence a outra versão ou compilação. Veja [atualizações e publicação de releases](docs/updates.md).

Você só precisa instalar os runtimes usados pelas **suas** aplicações: Python para projetos Python; Node.js e o gerenciador correspondente para projetos Node.js. O Cloudforge não instala esses runtimes globais silenciosamente. Se quiser criar bancos de dados locais ou usar o DBeaver integrado, instale também o **Docker** (Docker Desktop ou Docker Engine em modo Linux; veja [bancos de dados locais](docs/databases.md)).

Na primeira abertura:

1. Confira a verificação da máquina e crie o administrador com uma senha de pelo menos **12 caracteres**. Não há credenciais padrão.
2. Escolha conectar a Cloudflare agora ou configurar isso mais tarde.
3. Clique em **Adicionar projeto**, selecione uma pasta existente e revise runtime, comando de inicialização, porta e política de reinício.
4. Marque **Confio neste projeto** apenas para código que você conhece: a instalação de dependências e a aplicação usam as permissões do seu usuário.
5. Inicie o projeto e acompanhe **Visão geral** e **Logs**.

### Rodar a partir do código-fonte

Requisitos para desenvolver ou compilar: **Node.js 24 LTS**, npm e **Go 1.24 ou superior**. Os scripts reconhecem um SDK Go portátil em `.tools/go`, quando presente. Para usar bancos de dados locais, rodar os testes Docker ou empacotar o runtime do DBeaver (`package` / `package:dir`), é necessário também ter o **Docker** disponível.

```powershell
git clone https://github.com/Ryo448/cloudforge.git
cd cloudforge
npm ci
npm run dev
```

`npm run dev` compila o painel web e o Core, inicia o Vite do desktop e abre o Electron. Para executar a versão compilada:

```powershell
npm run build
npm start
```

`npm ci` instala dependências e baixa o executável oficial do Electron pelo instalador do pacote. O build gera os avisos de licenças de terceiros. Se você alterar o Go durante o desenvolvimento, encerre a instância anterior do Core antes de iniciar outra compilação.

## Cadastre sua primeira aplicação

Em **Aplicações → Adicionar projeto**, escolha a pasta original ou informe seu caminho absoluto. O Cloudforge identifica `package.json` para Node.js e arquivos ou manifestos Python para Python. Você pode ajustar o nome e o comando sugeridos antes de salvar.

| Campo                 | Exemplo                            | O que informar                                                                         |
| --------------------- | ---------------------------------- | -------------------------------------------------------------------------------------- |
| Ambiente              | `Node.js` ou `Python`              | O runtime detectado para a pasta.                                                      |
| Inicialização Node.js | `npm run start` ou `node index.js` | Um script do `package.json` ou um arquivo JavaScript. Não há shell genérico.           |
| Inicialização Python  | `app.py`                           | Um arquivo `.py` existente na pasta, com argumentos opcionais.                         |
| Porta HTTP            | `3000`                             | A porta em que **seu servidor realmente escuta**. Deixe vazia para processos sem HTTP. |
| Reinicialização       | `Em caso de falha`                 | Nunca, em falha ou sempre, exceto após parada manual.                                  |

O aplicativo prepara as dependências antes do primeiro início. Em Python, cria ou reutiliza `.venv`, `venv` ou `env` e instala requisitos pelo `pip` do ambiente. Em Node.js, detecta npm, pnpm ou Yarn pelo manifesto e lockfile; com `package-lock.json`, usa `npm ci`. Uma falha nessa etapa impede o processo de iniciar e aparece nos logs.

Se o servidor lê a variável `PORT`, defina a mesma porta no Cloudforge e reinicie o projeto para aplicar a mudança. Se o código fixa uma porta, cadastre esse valor. A porta pode ser alterada em **Projeto → Geral → Porta HTTP**; a rota pública existente é atualizada junto com a configuração.

## Acesso remoto com Cloudflare

1. Em **Cloudflare**, clique em **Conectar com a Cloudflare**. O navegador padrão abre a autorização oficial; escolha um domínio da sua conta.
2. Volte ao aplicativo e defina o subdomínio do painel, por exemplo `painel` para `painel.exemplo.com`. Se o domínio não vier preenchido, informe exatamente o que foi autorizado.
3. Clique em **Criar endereço público** e depois em **Verificar acesso HTTPS**.
4. Para publicar uma aplicação HTTP, abra **Projeto → Network → Subdomínio** e escolha domínio, nome e porta local.

O túnel aponta para um roteador local que reconhece apenas os hosts cadastrados. Uma aplicação parada responde como indisponível, sem apagar seu DNS. Você pode alterar ou remover endereços no aplicativo; a limpeza de um CNAME anterior só ocorre se ele ainda apontar para o túnel desta instalação. Se uma operação parcial precisar de atenção, use **Cloudflare → Recuperar operações**.

O pacote Windows x64 inclui o `cloudflared` oficial com SHA-256 verificado. Em outros ambientes compatíveis, o Core usa uma instalação existente ou baixa uma versão oficial verificada. A criação real de túnel, DNS e HTTPS depende da sua conta e deve ser conferida nela. Veja também o [guia oficial de túneis gerenciados localmente](https://developers.cloudflare.com/tunnel/features/locally-managed-tunnels/create-local-tunnel/).

## Operação diária

### Desktop e painel web

No desktop, a barra lateral dá acesso a **Visão geral**, **Aplicações**, **Bancos de dados**, **Cloudflare** e **Configurações**. Dentro de cada projeto, **Logs** mostra a saída atual; **Visão geral** traz ações e métricas de CPU/RAM; **Arquivos** permite navegar, editar texto/código e pré-visualizar mídia; **Geral** configura inicialização, porta e terminal; **Variáveis de ambiente** trabalha com `.env` ou modo exclusivo no cofre; **Network** cuida do endereço público; **Área de risco** remove o cadastro.

Os logs capturam `stdout` e `stderr` e mantêm histórico limitado a **1.000 linhas somente da execução atual**. Cada início ou reinício, inclusive automático, limpa o histórico e os arquivos de log anteriores. O painel de logs é somente leitura, com pesquisa, cópia e limpeza visual. Quando habilitado em **Geral**, o terminal interativo abre em janela dedicada apenas no desktop.

Em **Arquivos**, o editor usa realce de sintaxe (Python, HTML, CSS, JavaScript/TypeScript, JSON e Markdown) com salvamento por `Ctrl+S` e controle de conflito de versão, além de pré-visualização de imagens, vídeos e áudios (até 100 MB) e atalho para abrir a pasta no explorador do sistema.

Em **Variáveis de ambiente**, selecione ou importe um arquivo `.env` na pasta do projeto ou, quando não houver `.env`, cadastre variáveis no **modo exclusivo** (criptografadas com AES-GCM usando chave armazenada no cofre do sistema operacional). Reinicie a aplicação após salvar para aplicar os valores. O painel web não lê nem edita arquivos ou variáveis.

O painel web exige login e oferece **Aplicações** (status, métricas e ações de iniciar, parar e reiniciar) e **Bancos de dados** (criar, controlar, ver métricas, revelar credenciais e baixar certificados). Ele não permite cadastrar projetos, editar arquivos ou variáveis, abrir terminal, gerenciar snapshots/DBeaver nem configurar o túnel. Em **Configurações** do desktop, você pode configurar a inicialização automática, conectar o Google Drive, trocar a senha administrativa e revogar sessões remotas.

### Bancos de dados e snapshots

Em **Bancos de dados** (no desktop ou no painel web), crie instâncias **PostgreSQL**, **MySQL**, **MongoDB** e **Redis** no Docker local em modo Linux, com container, rede e volume persistente próprios, limite de memória configurável, métricas e conexão protegida por TLS/mTLS (com download de `.pem`, `.crt` e `.key`, além de redefinição de senha e certificado em **Conexão**). Veja o [guia de bancos de dados](docs/databases.md).

No desktop, instâncias PostgreSQL e MySQL também oferecem a aba **DBeaver** (com opção de abrir em janela separada), conectada automaticamente via CloudBeaver Community com verificação da CA local.

Na aba **Snapshots** (disponível no desktop), é possível enviar backups manuais e agendar envios automáticos para o **Google Drive** (configurado em **Configurações → Google Drive** com cliente OAuth desktop e tutorial integrado), manter de 1 a 50 versões e baixar, restaurar ou importar backups locais. Os agendamentos são executados pelo Core em segundo plano enquanto estiver ativo. Veja o [guia de snapshots](docs/snapshots.md).

### Inicialização automática e remoção

Em **Configurações → Inicialização e execução**, ative o Core na sessão do usuário. No Windows, isso usa a chave Run do usuário atual (`HKCU`); no Linux, um serviço systemd de usuário. O Core recupera os projetos e bancos cujo estado desejado era executar. Inicialização antes de login e Linux com `linger` exigem configuração adicional.

Para remover um cadastro de aplicação, abra **Projeto → Área de risco → Excluir projeto** e confirme pelo nome. O Cloudforge encerra o processo e remove cadastro, logs e endereço público. **A pasta original e seus arquivos permanecem no computador**, inclusive dependências instaladas nela.

### Diagnóstico rápido

| Sintoma                                    | O que conferir                                                                                                                                             |
| ------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------- |
| O projeto não inicia                       | Abra **Logs**; confira o runtime instalado, o arquivo ou script de inicialização e erros na instalação de dependências.                                    |
| O processo está ativo, mas a URL falha     | Confirme em **Geral → Porta HTTP** a porta em que o servidor escuta. Reinicie se a aplicação lê `PORT`.                                                    |
| O painel ou subdomínio não abre por HTTPS  | Em **Cloudflare**, use **Verificar acesso HTTPS**; confira domínio autorizado e estado do túnel. Se houver operação pendente, use **Recuperar operações**. |
| O banco de dados não sobe ou DBeaver falha | Verifique se o Docker está ativo em modo Linux. Confira os guias em [bancos de dados locais](docs/databases.md).                                           |
| O Core não abre uma segunda vez            | O mesmo diretório de dados aceita uma única instância do Core. Verifique se ele já continua ativo em segundo plano.                                        |

Para mudar o endereço do painel, use **Cloudflare → Alterar endereço do painel**. Para usar outro domínio no mesmo túnel local, autorize esse domínio na mesma conta Cloudflare. Em **Projeto → Network**, você pode alterar ou remover o subdomínio da aplicação.

## Desenvolvimento e empacotamento

Na raiz do repositório:

```powershell
npm run typecheck      # checa TypeScript
npm run lint           # confere formatação com Prettier
npm run format         # formata arquivos com Prettier
npm test               # testes Electron (Node), testes Go e typecheck
npm run build          # desktop, web, Core e avisos de terceiros
npm run bundle:dbeaver # exporta o runtime do DBeaver/CloudBeaver via Docker
npm run package:dir    # pasta executável (inclui cloudflared no Windows e DBeaver)
npm run package        # instalador NSIS (Windows) ou AppImage (Linux)
```

Testes de integração opcionais com Docker (bancos de dados e round-trip de snapshots) ficam documentados em [bancos de dados locais](docs/databases.md#teste-com-docker) e [snapshots dos bancos de dados](docs/snapshots.md#validar).

Os binários gerados não são assinados; distribuição assinada exige certificados do mantenedor. O pacote Windows inclui o `cloudflared` oficial e o pacote x64 inclui o runtime offline do DBeaver/CloudBeaver. Para escopo e resultados já verificados, consulte [validação](docs/validation.md).

| Caminho          | Responsabilidade                                                                                                                                                                                      |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `apps/desktop/`  | Interface React (CodeMirror, xterm.js), processo Electron, atualizador, DBeaver integrado e preload restrito.                                                                                         |
| `apps/web/`      | Painel remoto independente (projetos e bancos de dados).                                                                                                                                              |
| `services/core/` | API, autenticação, persistência, supervisor, terminal PTY, bancos de dados, snapshots, proxy e Cloudflare.                                                                                            |
| `packages/`      | Tipos compartilhados e componentes da interface (incluindo bancos e snapshots).                                                                                                                       |
| `scripts/`       | Desenvolvimento, build, testes, empacotamento de `cloudflared`/DBeaver e geração de avisos.                                                                                                           |
| `assets/`        | Ícone oficial, logotipos de runtime e imagens do README.                                                                                                                                              |
| `third_party/`   | Fontes e licenças do DBeaver e CloudBeaver Community empacotados com o aplicativo.                                                                                                                    |
| `docs/`          | [Arquitetura](docs/architecture.md), [bancos de dados](docs/databases.md), [snapshots](docs/snapshots.md), [atualizações](docs/updates.md), [validação](docs/validation.md) e material de referência. |

## Dados, segurança e limites

O Cloudforge guarda os dados locais em `%APPDATA%\Cloudforge` no Windows e `${XDG_CONFIG_HOME:-~/.config}/Cloudforge` no Linux. Ali ficam SQLite/WAL, chave administrativa local, logs, conector e perfil Electron. Credenciais OAuth do Google Drive, refresh tokens e a chave mestra das variáveis de ambiente em modo exclusivo ficam armazenados no **cofre do sistema operacional** (Credential Manager no Windows ou Secret Service no Linux). Os arquivos dos projetos continuam nas pastas de origem e precisam de backup próprio. Para copiar o banco SQLite com consistência, pare o Core ou use a API de backup do SQLite.

O código das aplicações cadastradas roda **com os privilégios do usuário do Core**, diretamente no host — não há isolamento equivalente a containers para projetos Python e Node.js; execute somente projetos confiáveis. A API administrativa aceita apenas loopback, usa chave local e não deve ser publicada. O painel remoto tem endpoints separados, autenticação, proteção de origem e CSRF; sessões usam cookies HttpOnly, Secure e SameSite Strict em produção. Trocar a senha revoga as sessões.

Os bancos de dados locais e o DBeaver integrado já utilizam containers Docker em modo Linux. Já o armazenamento interno do Core utiliza SQLite; o contrato de persistência em `services/core/internal/persistence/repository.go` permite um futuro adaptador PostgreSQL, ainda não implementado nesta versão. Os testes locais e as verificações externas pendentes estão documentados em [validação](docs/validation.md).

## Contribuição e licença

Contribuições são bem-vindas: abra uma issue com contexto reproduzível ou envie um pull request pequeno, com a motivação e os testes relevantes. Antes de enviar código, execute `npm test` e `npm run lint`. Para mudanças na execução ou no acesso remoto, inclua os cenários de falha e segurança que a alteração afeta.

O Cloudforge é distribuído sob a [licença MIT](LICENSE). Os avisos e licenças das dependências permanecem em [THIRD_PARTY_NOTICES.txt](docs/THIRD_PARTY_NOTICES.txt), as atribuições do DBeaver/CloudBeaver em [third_party/dbeaver/README.md](third_party/dbeaver/README.md) (Apache-2.0), os ícones de bancos em [packages/ui/icons/README.md](packages/ui/icons/README.md) e a licença do `cloudflared` acompanha a distribuição Windows.
