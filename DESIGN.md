---
name: "Cloudforge"
description: "Sistema visual de precisão industrial em carbono forjado e laranja incandescente para acompanhar recursos e operar projetos locais e remotos."
colors:
  brand-orange: "#fc7800"
  accent: "#ff7a1a"
  accent-hover: "#ff9138"
  on-accent: "#140800"
  online: "#ff8524"
  transition: "#f7b955"
  error: "#f87a74"
  ram: "#68a8e8"
  canvas: "#0b0d11"
  surface: "#13171e"
  sidebar: "#0e1116"
  active-nav: "#2c1b12"
  active-nav-text: "#ffffff"
  remote-canvas: "#0b0d11"
  terminal: "#080a0d"
  border: "#232834"
  login-outline: "#6b7588"
  text: "#f1f3f7"
  muted: "#949eae"
  control-surface: "#1a1f29"
  control-text: "#ebedf2"
  disabled-text: "#939dad"
  quiet-danger: "#f59e98"
  terminal-text: "#dce2ec"
typography:
  brand:
    fontFamily: "\"Poppins\", sans-serif"
    fontSize: "22px"
    fontWeight: 700
    lineHeight: 1
    letterSpacing: "-0.025em"
  display:
    fontFamily: "\"Poppins\", \"IBM Plex Sans\", sans-serif"
    fontSize: "42px"
    fontWeight: 600
    lineHeight: 1
    letterSpacing: "-0.03em"
  headline:
    fontFamily: "\"Poppins\", \"IBM Plex Sans\", sans-serif"
    fontSize: "30px"
    fontWeight: 600
    lineHeight: 1.2
    letterSpacing: "-0.03em"
  title:
    fontFamily: "\"IBM Plex Sans\", sans-serif"
    fontSize: "17px"
    fontWeight: 600
    letterSpacing: "-0.015em"
  body:
    fontFamily: "\"IBM Plex Sans\", sans-serif"
    fontSize: "12px"
    lineHeight: 1.65
  label:
    fontFamily: "\"IBM Plex Sans\", sans-serif"
    fontSize: "12px"
    fontWeight: 500
  button:
    fontFamily: "\"IBM Plex Sans\", sans-serif"
    fontSize: "12px"
    fontWeight: 500
    lineHeight: 1.3
  terminal:
    fontFamily: "\"IBM Plex Mono\", ui-monospace, SFMono-Regular, Consolas, monospace"
    fontSize: "13px"
    lineHeight: 1.7
rounded:
  field: "6px"
  button: "7px"
  tile: "8px"
  panel: "12px"
  dialog: "14px"
  setup: "16px"
spacing:
  inline-gap: "8px"
  control-gap: "12px"
  group-gap: "16px"
  panel-gap: "20px"
  panel-inset: "22px"
  remote-inset: "24px"
components:
  button-primary:
    backgroundColor: "{colors.accent}"
    textColor: "{colors.on-accent}"
    typography: "{typography.button}"
    rounded: "{rounded.button}"
    padding: "10px 14px"
  button-primary-hover:
    backgroundColor: "{colors.accent-hover}"
  button-primary-disabled:
    backgroundColor: "{colors.control-surface}"
    textColor: "{colors.disabled-text}"
  button-secondary:
    backgroundColor: "{colors.control-surface}"
    textColor: "{colors.control-text}"
    typography: "{typography.button}"
    rounded: "{rounded.button}"
    padding: "10px 14px"
  button-quiet-danger:
    backgroundColor: "{colors.control-surface}"
    textColor: "{colors.quiet-danger}"
    rounded: "{rounded.button}"
  field-login:
    backgroundColor: "{colors.remote-canvas}"
    textColor: "{colors.text}"
    rounded: "{rounded.field}"
    padding: "11px 12px"
    width: "100%"
  nav-item-active:
    backgroundColor: "{colors.active-nav}"
    textColor: "{colors.active-nav-text}"
    rounded: "{rounded.button}"
    padding: "12px"
  status-online:
    textColor: "{colors.online}"
    typography: "{typography.label}"
  status-offline:
    textColor: "{colors.muted}"
    typography: "{typography.label}"
  status-transition:
    textColor: "{colors.transition}"
    typography: "{typography.label}"
  status-error:
    textColor: "{colors.error}"
    typography: "{typography.label}"
  remote-card:
    backgroundColor: "{colors.surface}"
    rounded: "{rounded.panel}"
    padding: "24px"
  terminal-output:
    backgroundColor: "{colors.terminal}"
    textColor: "{colors.terminal-text}"
    typography: "{typography.terminal}"
    height: "390px"
    padding: "15px 14px"
---

# Sistema de design do Cloudforge

## Overview

**Creative North Star: "Forja de precisão operacional"**

O Cloudforge combina superfícies profundas de carbono e aço temperado (`#0b0d11` e `#13171e`) com a identidade de brasa e bigorna da marca. A marca renderiza o símbolo oficial em `assets/icon/icon.png` acompanhado pelo logotipo tipográfico em **Poppins Bold**, onde **Cloud** aparece em branco puro (`#ffffff`) e **forge** no laranja incandescente (`#fc7800`).

A interface apoia-se em **IBM Plex Sans** para leitura operacional e **IBM Plex Mono** para telemetria, caminhos, chips de host e saída de logs. O laranja da forja (`#ff7a1a`) destaca a ação primária disponível, o item ativo da navegação e o estado Online, enquanto o âmbar dourado (`#f7b955`) identifica transições e o coral (`#f87a74`) aponta falhas.

**Key Characteristics:**

- Superfícies de carbono obsidiana sem matiz esverdeado, com divisórias estruturais de 1px (`#232834`) e brilho superior discreto.
- Marca com ícone oficial (`assets/icon/icon.png`) e wordmark `Cloudforge` em Poppins Bold (`Cloud` branco + `forge` laranja `#fc7800`).
- Métricas com algarismos tabulares em IBM Plex Sans e saída de processos em IBM Plex Mono.
- Estados operacionais identificados por texto e cor, com foco de teclado visível em laranja forja.

As regras de interface estão em [packages/ui/styles.css](packages/ui/styles.css) e os componentes compartilhados em [packages/ui/index.tsx](packages/ui/index.tsx). A direção das telas de operação está em [docs/ui-surface.md](docs/ui-surface.md).

## Colors

- **Marca, ação e leitura de estado.** `brand-orange` (`#fc7800`) colore o sufixo `forge` da marca. `accent` (`#ff7a1a`) preenche ações primárias disponíveis e indica foco; `online` (`#ff8524`) identifica processos em execução. No gráfico de telemetria, o laranja identifica CPU e o azul aço `ram` (`#68a8e8`) identifica memória.
- **Transições e falhas.** `transition` (`#f7b955`, inspirado na centelha da logo) acompanha Iniciando, Parando e Reiniciando. `error` (`#f87a74`) acompanha Erro. Parar usa texto vermelho suave (`#f59e98`) sobre o fundo do controle secundário.
- **Superfícies e texto.** `canvas` (`#0b0d11`), `surface` (`#13171e`), `sidebar` (`#0e1116`) e `terminal` (`#080a0d`) organizam o espaço escuro em camadas de carbono e ferro temperado. `text` (`#f1f3f7`) destaca informações principais e `muted` (`#949eae`) mantém rótulos secundários, métricas auxiliares e o estado Offline legíveis.

## Typography

- **Poppins** (pesos 500, 600 e 700), **IBM Plex Sans** (400, 500 e 600) e **IBM Plex Mono** (400 e 500) são auto-hospedadas localmente nos dois aplicativos.
- A marca `Cloudforge` usa **Poppins Bold (700)** com tracking `-0.025em`. Os títulos de página (`h1`), o cabeçalho de configuração inicial e o login usam **Poppins SemiBold (600)**.
- A visão geral reserva os números maiores para o total em execução (42px em Poppins SemiBold) e para as métricas da máquina (23px, chegando a 27px a partir de 1550px). A faixa do projeto usa rótulos de 10px e valores de 14px. Os cartões remotos usam rótulos de 12px e valores de 15px, reduzidos a 14px até 760px.
- Os logs, arquivos, chips de host e prévias de URL usam **IBM Plex Mono** (13px com entrelinha 1,7 no terminal). Hora e origem usam 11px.

## Layout

- No desktop, a barra lateral fixa (224px) e a barra superior (65px) mantêm a orientação. Na visão geral, o conteúdo cresce ao lado de um resumo de 244px, com 20px entre os painéis. A lista de projetos usa linhas tabulares e rolagem no próprio contêiner quando necessário.
- Até 1150px, a barra lateral passa a 200px, a visão geral vira uma coluna e o resumo é recolhido. Até 760px, a barra lateral passa a 68px exibindo apenas o símbolo da marca (`assets/icon/icon.png`) e os ícones de navegação, as métricas da máquina ficam em uma coluna e os recursos do projeto passam para duas. A partir de 1550px, a área principal ganha respiro e o gráfico aumenta de altura.
- Na web, a área principal tem até 1120px de largura. Os cartões passam de duas colunas para uma até 760px. As três ações dividem a largura do cartão e têm pelo menos 44px de altura nessa faixa. O formulário de login usa uma coluna de até 420px.

## Elevation & Depth

Painéis, listas e cartões em repouso usam superfícies de aço escuro (`#13171e`), divisórias finas (`#232834`), brilho interno de topo (`inset 0 1px 0 rgba(255, 255, 255, 0.035)`) e sombra discreta. O diálogo é a camada elevada (`#151922`), com sombra ambiente `0 24px 80px rgba(0, 0, 0, 0.65)` e fundo escurecido com desfoque de 4px.

## Shapes

Controles de formulário (6px), botões (7px) e elementos internos como o ícone de runtime (8px) têm cantos discretos. Painéis e cartões usam 12px, o diálogo usa 14px e a configuração inicial usa 16px. Pontos circulares indicam estado e acompanham texto.

## Components

### Marca (`Brand`)

Renderiza o ativo `assets/icon/icon.png` seguido pelo nome `Cloudforge` em Poppins Bold (700), com `Cloud` em `#ffffff` e `forge` em `#fc7800`. Na barra lateral estreita (até 760px), preserva apenas o símbolo gráfico centralizado.

### Ações de execução

Iniciar é primário (`#ff7a1a` com texto `#140800`) quando disponível. Online, em transição ou durante uma operação pendente, recebe a aparência neutra de botão desativado (`#1a1f29`), com opacidade de 0,45. Reiniciar usa o estilo secundário e Parar mantém o mesmo fundo com texto vermelho suave (`#f59e98`). As mudanças de fundo e borda ao passar o ponteiro duram 150ms. O foco de teclado tem contorno laranja de 2px com afastamento de 3px.

### Campos de login

Rótulos acima dos campos mantêm a identificação visível. O contorno em repouso usa `login-outline` (`#6b7588`) sobre `#0e1218` (contraste > 3,7:1). O estado de foco usa o mesmo contorno laranja dos botões.

### Navegação e estado

A barra lateral usa ícones e rótulos, com gradiente âmbar-laranja translúcido, filete lateral de 3px e ícone laranja no item ativo. No detalhe do projeto, a aba selecionada usa um sublinhado laranja de 2px. Status usa um ponto de 5px seguido pelo texto, em 12px e peso 500, na tabela, no detalhe e nos cartões remotos.

### Métricas, cartões e logs

Cada métrica apresenta rótulo, valor e unidade quando aplicável, com barras de progresso em gradiente de forja (`#ff5500` → `#ff7a1a` → `#ffb000`). No detalhe do projeto, uma faixa separa recursos e saúde do processo da área de logs. No cartão remoto, uma divisória separa CPU, RAM e tempo ativo das três ações. O terminal de leitura tem fundo preto de fornalha (`#080a0d`), colunas para hora, origem e mensagem e rolagem própria.

## Do's and Don'ts

- **Faça.** Renderize sempre a marca com o ativo `assets/icon/icon.png` e `Cloudforge` em Poppins Bold (`Cloud` branco e `forge` em `#fc7800`).
- **Faça.** Combine o ponto de estado com um rótulo como Online, Offline ou Reiniciando.
- **Faça.** Use algarismos tabulares nas métricas e mantenha visíveis os rótulos e as unidades.
- **Faça.** Preserve os 44px de altura mínima das três ações na web até 760px.
- **Faça.** Mantenha o contorno específico dos campos de login e o foco visível com teclado.
- **Evite.** manter o preenchimento laranja primário de Iniciar quando a ação estiver indisponível.
- **Evite.** preencher lacunas de telemetria com valores simulados. Mostre o marcador de ausência ou a mensagem de espera da superfície.
