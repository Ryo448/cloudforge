# Bancos de dados locais

O Cloudforge gerencia PostgreSQL, MySQL, MongoDB e Redis no Docker da própria máquina. Use Docker Engine 28 ou superior em modo Linux. Cada instância tem um container, um volume e uma rede Docker próprios. Você pode criar várias instâncias da mesma tecnologia e recuperar dados entre bancos compatíveis.

## Preparar o Docker

No Windows, siga o [guia de instalação do Docker Desktop](https://docs.docker.com/desktop/setup/install/windows-install/). Na primeira abertura, conclua a configuração do Docker Desktop e mantenha o modo de containers Linux. No Linux, siga o [guia de instalação do Docker Engine](https://docs.docker.com/engine/install/) para sua distribuição e configure o acesso ao Docker para o usuário que executa o Core.

O Core tenta iniciar o Docker Desktop no Windows ou o serviço Docker no Linux quando o Docker local instalado está indisponível. A tela **Bancos de dados** mostra o progresso e exibe os guias de instalação quando o Docker está ausente.

O Cloudforge baixa do Docker Hub a imagem da tecnologia escolhida na primeira criação, caso ainda não esteja no Docker local. Em instalações offline, exporte as imagens `postgres:17.9-bookworm`, `mysql:8.4.8`, `mongo:8.0.20-noble` e `redis:7.4.8-bookworm` de outra máquina com `docker save` e importe-as com `docker load`.

## Criar e conectar

1. Abra **Bancos de dados**, clique em **Criar banco de dados**, escolha a tecnologia e informe o limite de memória em MB. Use **0** para deixar a memória sem limite; o valor inicial é 1024 MB.
2. Aguarde o status **Online**, guarde a senha exibida uma única vez e baixe **Certificate CA (.crt)** no modal de credenciais.
3. Em **Conexão**, copie a porta local e a URL. Abra o DBeaver ou outro cliente na máquina que executa o Core, use o host `127.0.0.1` e configure a CA baixada como confiável para a conexão.

O usuário é `cloudforge`. Use o banco `cloudforge` no PostgreSQL, MySQL e MongoDB. No MongoDB, use também `cloudforge` como banco de autenticação. No Redis, use o índice `0` e o usuário ACL `cloudforge`. As quatro engines exigem TLS nas conexões TCP; não é necessário certificado de cliente.

No PostgreSQL, a conta `cloudforge` é superusuária da instância e pode executar comandos como `CREATE DATABASE`.

### Conectar pela rede local

Em **Conexão → Acesso pela rede local**, copie o IPv4 do hospedeiro na mesma sub-rede do computador cliente. Abra a conexão com esse endereço, a porta exibida no painel, o usuário `cloudforge` e a senha da instância. Configure o TLS com a CA baixada e as opções do seu banco na tabela abaixo.

No Windows, o aplicativo desktop explica a alteração do firewall e pede sua autorização. Ao escolher **Permitir**, confirme também a solicitação do Windows, se aparecer. A regra é limitada às portas TCP dos bancos registrados e à sub-rede local. O Cloudforge encaminha o tráfego TLS para o banco e exibe o endereço depois de validar esse caminho no hospedeiro. No Linux, o Cloudforge abre o encaminhador e respeita as regras do firewall do sistema.

Instâncias existentes são migradas para a nova configuração na mesma porta. O aplicativo volta a pedir autorização se as portas da regra precisarem mudar. Se o IPv4 mudar, o Cloudforge inclui o novo endereço no certificado do servidor automaticamente. A instância pode reiniciar brevemente e a CA continua válida.

### Conectar pelo túnel Cloudflare

No **Cloudflare Zero Trust → Access controls → Applications**, crie um aplicativo auto-hospedado com o subdomínio desejado, por exemplo `postgres.example.com`. Defina uma política **Allow** para os e-mails autorizados e copie a tag **AUD** do aplicativo e o nome da equipe.

No Cloudforge Desktop do hospedeiro, abra **Bancos de dados**, selecione a instância e abra **Conexão → Acesso remoto via Cloudflare → Configurar túnel TCP**. O botão **Ver tutorial** abre o guia no próprio formulário. Configure o subdomínio em um domínio já autorizado pelo Cloudforge, a equipe e o AUD. O Cloudforge usa o túnel do painel e valida o token do Access no conector. Ao carregar a rota, o túnel reconecta e pode interromper brevemente o painel e os projetos. O banco continua usando TLS e senha, e a rota para de encaminhar quando a instância está indisponível.

No computador cliente, inclusive em outra rede, abra **Configurações → Conexões TCP → Adicionar conexão** no Cloudforge Desktop. Informe o domínio do Access e uma porta local livre, como `15432`. Aguarde o status **Escutando**. Cadastre outras conexões com portas locais diferentes. O Cloudforge mantém cada conexão ativa de forma independente e reabre as conexões ativas ao iniciar novamente.

No DBeaver, crie uma conexão com o driver do banco, use o host `127.0.0.1` e a porta local escolhida, informe o usuário e a senha da instância e configure o TLS com sua CA. Ao testar a conexão, conclua o login no navegador se o Cloudflare Access solicitar e teste novamente. O DBeaver Community inclui PostgreSQL e MySQL. Para MongoDB e Redis, use o DBeaver PRO ou um cliente próprio, como MongoDB Compass ou Redis Insight.

Em um computador sem Cloudforge Desktop, instale o [cloudflared](https://developers.cloudflare.com/cloudflare-one/access-controls/applications/non-http/cloudflared-authentication/arbitrary-tcp/) e mantenha o comando mostrado em **Conexão → Comando para terminal** em execução. Por exemplo:

```powershell
cloudflared access tcp --hostname postgres.example.com --url 127.0.0.1:15432
```

Ao desativar a rota pelo Cloudforge, revise a aplicação e as permissões no Cloudflare Access caso tenham sido criadas só para esse banco.

### DBeaver integrado

No Cloudforge Desktop, abra uma instância PostgreSQL ou MySQL e selecione **DBeaver**, logo abaixo de **Visão geral**. Com o banco online e o TLS validado, a aba conecta automaticamente e disponibiliza o navegador de objetos, o editor SQL e a grade de dados. Exporte scripts e resultados que queira guardar antes de sair da aba. A sessão do editor é temporária.

O instalador x64 carrega o motor do DBeaver e a interface CloudBeaver Community a partir do pacote local no Docker já usado pelas instâncias. O pacote inclui o runtime Java e os drivers JDBC. A conexão verifica a CA e a identidade TLS do banco. Os drivers de MongoDB e Redis do DBeaver pertencem à edição comercial. Para usar essas instâncias com outro cliente, use os dados da seção **Conexão**.

Para preparar o recurso no ambiente de desenvolvimento, execute `npm run bundle:dbeaver`. O comando de empacotamento já inclui essa etapa. A preparação do instalador pode buscar a imagem oficial pela revisão fixada se ela estiver ausente do Docker local. [Fontes, licenças e atribuições](../third_party/dbeaver/README.md).

| Banco | Configuração no cliente |
| --- | --- |
| PostgreSQL | Use `sslmode=verify-full` e informe o arquivo `.crt` em `sslrootcert`. |
| MySQL | Use verificação de identidade (`sslMode=VERIFY_IDENTITY` no Connector/J, `--ssl-mode=VERIFY_IDENTITY` no cliente de linha de comando) e informe a CA na configuração de confiança do cliente. |
| MongoDB | Habilite `tls=true` e informe o arquivo da CA em `tlsCAFile` ou na configuração de confiança do driver. |
| Redis | Use uma URL `rediss://` e configure a CA no driver. Com `redis-cli`, use `--tls --cacert caminho/da-ca.crt`. |

O modal de criação e **Conexão → Certificado SSL/TLS** oferecem três downloads: **PEM (.pem)** (`client.pem`), **Certificate CA (.crt)** (`ca.crt`) e **Private Key (.key)** (`client.key`). A CA verifica a identidade do servidor. Para apresentar também uma identidade de cliente durante o handshake TLS, use o PEM e a chave em conjunto e continue usando o usuário e a senha do banco. Guarde a chave com acesso exclusivo ao usuário, por exemplo com `chmod 600 client.key` em Linux.

| Banco | Certificado de cliente opcional |
| --- | --- |
| PostgreSQL | Informe `sslcert=client.pem` e `sslkey=client.key` no libpq. O servidor verifica o certificado apresentado e mantém a autenticação SCRAM. |
| MySQL | No cliente de linha de comando, adicione `--ssl-cert=client.pem --ssl-key=client.key`. Em drivers que usam um keystore, importe o par no keystore do cliente. |
| MongoDB | Se o driver usa `tlsCertificateKeyFile`, reúna o certificado e a chave em um PEM com acesso restrito (`umask 077; cat client.pem client.key > client-combined.pem` em Linux). Use o PEM combinado em `tlsCertificateKeyFile` com as credenciais SCRAM existentes. |
| Redis | Adicione `--cert client.pem --key client.key` ao `redis-cli` com TLS. O Redis verifica o certificado apresentado e mantém a autenticação ACL. |

**Redefinir certificado** reinicia o banco, renova os pares do servidor e de cliente e preserva a CA, a porta, as credenciais e os dados. Certificados e chaves anteriores poderão deixar de funcionar; baixe o novo par e atualize a configuração dos clientes. Como a CA permanece a mesma, essa operação não revoga automaticamente certificados de cliente antigos.

A porta atribuída permanece a mesma após parar, iniciar ou reiniciar o container ou o Core. Containers autorizados podem conectar pela rede Docker da instância usando o nome do container e a porta interna da engine. A opção **Ver senha** exibe novamente a senha e a URL de conexão após validar o usuário e a senha configurados no painel do Core. A opção **Redefinir senha** invalida a senha anterior. A exclusão só prossegue após a confirmação pelo nome do banco.

## Atualizar instalações existentes

O Cloudforge emite automaticamente o par de cliente para bancos que já usam TLS, preservando o certificado do servidor e a CA. Bancos em execução ganham suporte a mTLS no próprio container; bancos parados recebem a configuração na próxima inicialização. Para instalações antigas sem TLS, continua valendo a migração original: o Core adequa o container, usando a imagem local, o volume, as redes e as credenciais existentes. Use a porta mostrada na aba **Conexão** e configure a CA no cliente antes de reconectar.

O Core atualizado promove automaticamente a conta `cloudforge` das instâncias PostgreSQL existentes. Para habilitar o acesso pela rede local em todos os motores, o Core ajusta a publicação da porta legada quando necessário e pode renovar o certificado do servidor para incluir os endereços LAN. Preserva o volume, a porta, as credenciais, a CA e o par de cliente existente. Instâncias em execução podem passar por uma breve interrupção. Instâncias paradas são migradas quando iniciadas.

Para configurar o Google Drive e operar backups, consulte [Snapshots dos bancos](snapshots.md).

## Teste com Docker

Com o Docker disponível, execute no diretório `services/core`. O teste baixa as imagens ausentes durante o provisionamento.

```powershell
$env:CLOUDFORGE_TEST_DATABASES = "1"
go test ./internal/databases -run TestDockerDatabases -v
```
