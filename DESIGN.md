---
name: "Cloudforge"
description: "Sistema visual para acompanhar recursos e operar projetos locais e remotos."
colors:
  accent: "#a5d4aa"
  accent-hover: "#bde2c1"
  on-accent: "#14271b"
  transition: "#e7c089"
  error: "#f1a5a0"
  ram: "#9bb9d4"
  canvas: "#111715"
  surface: "#171f1b"
  sidebar: "#141d18"
  active-nav: "#293d2e"
  active-nav-text: "#d6eadd"
  remote-canvas: "#111b15"
  terminal: "#0d1510"
  border: "#2c3932"
  login-outline: "#667c6d"
  text: "#e9eeeb"
  muted: "#a2b2a8"
  control-surface: "#202c25"
  control-text: "#e7eee9"
  disabled-text: "#acbdb1"
  quiet-danger: "#f0b4ae"
  terminal-text: "#d1dfd5"
typography:
  display:
    fontFamily: "\"Inter Variable\", Inter, sans-serif"
    fontSize: "42px"
    fontWeight: 500
    lineHeight: 1
    letterSpacing: "-0.03em"
  headline:
    fontFamily: "\"Inter Variable\", Inter, sans-serif"
    fontSize: "30px"
    fontWeight: 570
    lineHeight: 1.2
    letterSpacing: "-0.03em"
  title:
    fontFamily: "\"Inter Variable\", Inter, sans-serif"
    fontSize: "17px"
    fontWeight: 570
    letterSpacing: "-0.015em"
  body:
    fontFamily: "\"Inter Variable\", Inter, sans-serif"
    fontSize: "12px"
    lineHeight: 1.65
  label:
    fontFamily: "\"Inter Variable\", Inter, sans-serif"
    fontSize: "12px"
    fontWeight: 500
  button:
    fontFamily: "\"Inter Variable\", Inter, sans-serif"
    fontSize: "12px"
    fontWeight: 550
    lineHeight: 1.3
  terminal:
    fontFamily: "ui-monospace, SFMono-Regular, Consolas, monospace"
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
    textColor: "{colors.accent}"
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

**Creative North Star: "Visibilidade operacional em primeiro lugar"**

O Cloudforge usa grafite com matiz verde, tipografia Inter auto-hospedada e separações finas para manter o foco nas condições da máquina e dos projetos. A densidade da interface acompanha o trabalho de quem alterna entre verificar recursos, ler logs e agir sobre processos.

A hierarquia se apoia em texto, valores alinhados e diferenças tonais. A interface combina ícones Lucide com rótulos nas ações de execução. O verde fosco destaca a ação disponível e o estado Online, enquanto âmbar e vermelho identificam transições e falhas.

**Key Characteristics:**

- Superfícies escuras com diferenças tonais discretas e divisórias de 1px.
- Métricas com algarismos tabulares e saída de processos em fonte monoespaçada.
- Estados operacionais identificados por texto e cor, com foco de teclado visível.

As regras de interface estão em [packages/ui/styles.css](packages/ui/styles.css) e os componentes compartilhados em [packages/ui/index.tsx](packages/ui/index.tsx). A direção das telas de operação está em [docs/ui-surface.md](docs/ui-surface.md).

## Colors

- **Ação e leitura de estado.** `accent` preenche ações primárias disponíveis, indica foco e identifica Online. No gráfico, o verde identifica CPU e o azul `ram` identifica memória. As séries também têm legenda textual.
- **Transições e falhas.** `transition` acompanha Iniciando, Parando e Reiniciando. `error` acompanha Erro. Parar usa texto vermelho suave sobre o mesmo fundo do botão secundário.
- **Superfícies e texto.** `canvas`, `surface`, `sidebar` e `remote-canvas` organizam o espaço escuro. `text` destaca informações principais e `muted` mantém rótulos secundários, métricas auxiliares e o estado Offline legíveis.

## Typography

- A Inter Variable é carregada localmente nos dois aplicativos. O título de página usa 30px e peso 570 no desktop, com 25px nos layouts mais estreitos. O título de login usa 33px e peso 500.
- A visão geral reserva os números maiores para o total em execução (42px) e para as métricas da máquina (23px, chegando a 27px a partir de 1550px). A faixa do projeto usa rótulos de 10px e valores de 14px. Os cartões remotos usam rótulos de 12px e valores de 15px, reduzidos a 14px até 760px.
- Os logs usam uma pilha monoespaçada (13px com entrelinha 1,7). Hora e origem usam 11px. As mensagens conservam espaços e quebras e podem quebrar linhas longas.

## Layout

- No desktop, a barra lateral fixa (224px) e a barra superior (65px) mantêm a orientação. Na visão geral, o conteúdo cresce ao lado de um resumo de 244px, com 20px entre os painéis. A lista de projetos usa linhas tabulares e rolagem no próprio contêiner quando necessário.
- Até 1150px, a barra lateral passa a 200px, a visão geral vira uma coluna e o resumo é recolhido. Até 760px, a barra lateral passa a 68px e usa ícones, as métricas da máquina ficam em uma coluna e os recursos do projeto passam para duas. A partir de 1550px, a área principal ganha respiro e o gráfico aumenta de altura.
- Na web, a área principal tem até 1120px de largura. Os cartões passam de duas colunas para uma até 760px. As três ações dividem a largura do cartão e têm pelo menos 44px de altura nessa faixa. O formulário de login usa uma coluna de até 420px.

## Elevation & Depth

Painéis, listas e cartões em repouso usam fundos tonais e divisórias finas. O diálogo é a camada elevada, com sombra ambiente `0 24px 80px #0008` e fundo escurecido com desfoque de 3px.

## Shapes

Controles de formulário (6px), botões (7px) e elementos internos como o ícone de runtime (8px) têm cantos discretos. Painéis e cartões usam 12px, o diálogo usa 14px e a configuração inicial usa 16px. Pontos circulares indicam estado e acompanham texto.

## Components

### Ações de execução

Iniciar é primário quando disponível. Online, em transição ou durante uma operação pendente, recebe a aparência neutra de botão desativado, com opacidade de 0,45. Reiniciar usa o estilo secundário e Parar mantém o mesmo fundo com texto vermelho suave. As mudanças de fundo e borda ao passar o ponteiro duram 150ms. O foco de teclado tem contorno verde de 2px com afastamento de 3px. A preferência por movimento reduzido encurta animações e transições.

### Campos de login

Rótulos acima dos campos mantêm a identificação visível. O contorno em repouso usa `login-outline` sobre `remote-canvas` (3,92:1 na revisão de entrega). O estado de foco usa o mesmo contorno verde dos botões.

### Navegação e estado

A barra lateral usa ícones e rótulos, com fundo verde escuro e ícone verde fosco no item ativo. No detalhe do projeto, a aba selecionada usa um sublinhado verde. Status usa um ponto de 5px seguido pelo texto, em 12px e peso 500, na tabela, no detalhe e nos cartões remotos.

### Métricas, cartões e logs

Cada métrica apresenta rótulo, valor e unidade quando aplicável. No detalhe do projeto, uma faixa separa recursos e saúde do processo da área de logs. No cartão remoto, uma divisória separa CPU, RAM e tempo ativo das três ações. O terminal de leitura tem fundo mais escuro, colunas para hora, origem e mensagem e rolagem própria. A partir da variante estreita de até 760px, o terminal exibe hora e mensagem.

## Do's and Don'ts

- **Faça.** Combine o ponto de estado com um rótulo como Online, Offline ou Reiniciando.
- **Faça.** Use algarismos tabulares nas métricas e mantenha visíveis os rótulos e as unidades.
- **Faça.** Preserve os 44px de altura mínima das três ações na web até 760px.
- **Faça.** Mantenha o contorno específico dos campos de login e o foco visível com teclado.
- **Evite.** manter o preenchimento verde primário de Iniciar quando a ação estiver indisponível.
- **Evite.** preencher lacunas de telemetria com valores simulados. Mostre o marcador de ausência ou a mensagem de espera da superfície.
