# Snapshots dos bancos de dados

Abra **Bancos de dados → Seu banco → Snapshots** no Cloudforge desktop. Um snapshot manual pode ser criado sempre que o banco estiver online e o Google Drive estiver conectado, inclusive quando os agendamentos estiverem desativados. Para agendar, abra **Configurações** e selecione a frequência, os dias, os horários locais e o número de versões a manter.

## Configurar o Google Drive

Em **Configurações → Google Drive**, abra **Tutorial de integração** para acompanhar os passos e acessar os links do Google Cloud no navegador.

1. No Google Cloud, habilite a **Google Drive API** e configure a tela de consentimento. Crie um cliente OAuth do tipo **App para computador**. Esse tipo aceita retorno HTTP em loopback com porta dinâmica. [OAuth para apps desktop](https://developers.google.com/identity/protocols/oauth2/native-app)
2. No desktop, abra **Configurações → Google Drive**. Se o cliente OAuth ainda não estiver configurado, cole o ID e o segredo fornecidos pelo Google e clique em **Salvar cliente OAuth**. Instalações pré-configuradas com `CLOUDFORGE_GOOGLE_CLIENT_ID` e `CLOUDFORGE_GOOGLE_CLIENT_SECRET` já oferecem o botão de conexão.
3. Clique em **Conectar conta Google**, autorize no navegador padrão e volte ao Cloudforge. O Core solicita drive.file com PKCE e acesso offline. As credenciais cadastradas no aplicativo e o refresh token ficam no cofre do sistema operacional. No Linux, o Core precisa de acesso ao Secret Service da sessão do usuário. O SQLite guarda os identificadores da conta, da instalação, das pastas e dos arquivos.
4. Se o app OAuth estiver como **Externo** e **Em teste**, adicione a conta como usuária de teste. Para o escopo do Drive, o refresh token emitido nesse modo expira em sete dias. Para uso contínuo, configure a publicação conforme as exigências do Google para o seu projeto. [Expiração de tokens OAuth](https://developers.google.com/identity/protocols/oauth2#expiration)

Ao abrir **Snapshots** sem uma conta cadastrada, use **Abrir configurações do Google Drive** para ir diretamente à seção de autenticação.

A pasta **cloudforge - modelo do computador** é criada no primeiro upload. Cada banco recebe uma subpasta **SGBD - ID do cadastro**. O modelo visível é apurado pelo sistema operacional e o identificador persistente da instalação distingue computadores do mesmo modelo.

Os arquivos usam a cota do Google Drive da conta. Os dumps são enviados por HTTPS e ficam no formato nativo. A implementação não aplica criptografia no cliente antes do envio.

## Agendamento e retenção

O Core faz os backups com a janela fechada enquanto estiver ativo, com Docker disponível e o banco online. Se um banco ficar indisponível durante vários horários, ao voltar o Core tenta o horário programado mais recente que ainda não foi registrado. Horários inexistentes na mudança de horário de verão são pulados. Um horário repetido é executado uma vez.

Backups manuais e automáticos compartilham o limite de 1 a 50 versões por banco. Depois de verificar tamanho e checksum do novo arquivo, o Core remove os excedentes mais antigos da mesma pasta. A redução do limite exige confirmação. Se a manutenção falhar, ela fica pendente para nova tentativa. Excluir um cadastro local ou desconectar o Drive mantém os backups remotos.

## Baixar, restaurar ou importar

- **Baixar** grava o formato original no destino escolhido e verifica a integridade antes de concluir.
- **Restaurar** mostra a origem, o destino e os efeitos da operação antes de confirmar.
- **Importar backup** permite escolher um arquivo local compatível. Use arquivos de origem confiável.
- **Outros backups do Drive** lista os arquivos compatíveis acessíveis ao cliente OAuth, inclusive de cadastros ou instalações perdidos. Para recuperá-los, conecte a mesma conta usando o mesmo cliente OAuth. O escopo drive.file limita o acesso aos arquivos autorizados para o aplicativo. Se o arquivo foi criado por outro cliente e não estiver acessível, baixe-o no próprio Google Drive e use **Importar backup**. [Escopos do Google Drive](https://developers.google.com/workspace/drive/api/guides/api-specific-auth)

| SGBD | Escopo e formato | Efeito no destino |
| --- | --- | --- |
| PostgreSQL | Banco cloudforge inteiro, em formato customizado de pg_dump. Importação também aceita SQL simples. | O dump customizado restaura em uma transação e recria os objetos do archive. Usa o usuário do destino e mantém as permissões e papéis globais fora do backup. |
| MySQL | Banco cloudforge, em SQL compactado; também aceita SQL simples. O dump inclui rotinas, triggers e eventos. A leitura transacional garante consistência para tabelas transacionais, como InnoDB. | Executa o SQL com a conta do banco. Uma falha pode deixar alterações parciais, pois operações DDL podem efetivar alterações independentemente da transação. |
| MongoDB | Banco cloudforge, em archive gzip do mongodump. Nas instâncias standalone, gravações concorrentes podem produzir diferenças de ponto no tempo entre coleções. | Recria as coleções presentes no archive. Para uma cópia consistente entre coleções, suspenda as gravações durante o backup. |
| Redis | RDB nativo dos bancos lógicos do contêiner. | Valida e abre a cópia em uma instância temporária, depois transfere chaves e vencimentos para os bancos lógicos correspondentes. Chaves homônimas são substituídas. Uma falha pode deixar alterações parciais. |

Para recuperar um banco inteiro com o menor risco, crie um destino vazio da mesma tecnologia e versão. Objetos ausentes do backup podem permanecer em um destino preenchido.

## Validar

A suíte Go exercita PKCE, renovação e revogação OAuth, interrupções de upload, checksum, retenção e isolamento entre pastas usando servidores de teste locais. Os testes opcionais a seguir exigem Docker e as [imagens do Cloudforge](databases.md). Eles criam bancos descartáveis e usam apenas dados de exemplo.

~~~powershell
cd services/core
$env:CLOUDFORGE_SNAPSHOT_DOCKER_TEST = "1"
go test -v -timeout 14m ./internal/databases ./internal/snapshots -run 'TestSnapshotDockerRoundTrip|TestScheduledAndManualSnapshotsDocker'
~~~
