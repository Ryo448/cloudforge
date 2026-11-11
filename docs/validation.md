# Validação

Esta lista distingue testes reais locais de etapas que precisam de conta externa ou alterações no sistema. Mensagens da interface não são consideradas prova suficiente.

## Evidências locais em Windows

- Build de React desktop e web e checagem TypeScript executados.
- Core Go compilado, `go vet` executado.
- Testes de Argon2id, criação única de administrador, hash de tokens, rotação, CSRF e revogação executados.
- Testes da fronteira pública: sem autenticação retorna 401; origem/CSRF incorretos retornam 403; endpoints administrativos não ficam disponíveis na web; diretório do projeto não é retornado.
- Projeto Python temporário criou venv, instalou requirements, executou Python real, reutilizou o cache e reinstalou após mudança. Instalação inválida falhou sem avançar o hash bem-sucedido.
- Projeto Node.js temporário instalou dependências, executou código real e reutilizou o cache; mudança de manifesto invalidou o cache.
- O supervisor iniciou, reiniciou e parou servidores HTTP reais em Node.js e Python. Um segundo comando de início não criou outro processo. A restauração respeitou a parada manual e iniciou os projetos com estado desejado em execução.
- UI Electron consultada em execução: recursos reais, PID, filhos, saúde da porta, stdout/stderr, lista de arquivos e leitura de `server.cjs`.
- Dashboard web: login pelo formulário, estado pelo WebSocket, confirmação e parada real, início real, sessão mantida após recarregar e layout de 390 px sem transbordamento horizontal.
- O Electron foi fechado; Core e servidor HTTP continuaram respondendo. Depois, a parada de QA encerrou a árvore e liberou a porta. Somente os processos de QA foram encerrados.
- O executável Windows empacotado abriu a visão geral e restaurou um projeto de teste ao iniciar o Core empacotado. O servidor respondeu por HTTP e reutilizou o cache de dependências.
- O instalador NSIS x64 sem assinatura foi gerado. Os hashes dos arquivos do painel web e do Core no pacote correspondem aos artefatos da compilação. O SHA-256 do instalador acompanha o executável em `release/`.
- O executável final abriu com o Core conectado e a instalação normal vazia, pronta para criar o primeiro administrador.
- Revisão visual final: os três ajustes revisados foram confirmados (destaque de ações desativadas, legibilidade de métricas e logs e bordas do login). A borda do login mediu 3,92:1; o painel coube em 390 px CSS com três controles de 44 px CSS e sem rolagem horizontal.
- O Core também compilou para Linux amd64. A execução nesse ambiente fica para um teste em máquina Linux.
- Rota offline retornou 503 em teste e permaneceu persistida. Compensação de falha de DNS foi testada com servidor HTTP de teste, sem alterar uma conta Cloudflare real.
- Na versão 0.1.1, o pacote Windows incluiu o `cloudflared` 2026.10.7 com SHA-256 conferido. O binário do pacote respondeu a `--version`, e a CLI oficial validou a regra de ingresso e o 404 para hosts fora do domínio.
- O novo fluxo passou em testes de autorização de domínio, conflito de DNS, reutilização de túnel após falha e cadastro de rota de projeto com uma CLI simulada. A tela renderizada mostrou o botão de login pelo navegador sem campo de API Token. O pacote final abriu com a conta de administrador existente preservada.
- A consulta real de túneis sem correspondência retornou `null` na saída JSON do `cloudflared` com código de saída zero. O teste de regressão da versão 0.1.2 usa essa resposta na criação do primeiro túnel e uma lista JSON no reaproveitamento.
- `npm audit` não reportou vulnerabilidades na resolução verificada. `govulncheck` não encontrou vulnerabilidades alcançáveis; reportou avisos em partes não chamadas de dependências. Isso não equivale a uma auditoria de segurança independente.

Fixtures e credenciais efêmeras de QA são geradas em diretórios temporários ou `.local`, ignorados pelo Git. A inicialização normal nunca cria administrador ou projetos de exemplo.

## Verificações externas ou de sistema pendentes

- Autorização de um domínio com uma conta Cloudflare real no novo fluxo de navegador.
- Criação real de túnel/DNS, conexão cloudflared e resposta HTTPS pelo domínio.
- Ativação real da inicialização automática e reinício físico da máquina.
- Testes de execução em Linux, pnpm e Yarn. A compilação cruzada não substitui execução nesses ambientes.
- Assinatura de distribuição e licença do próprio projeto.

