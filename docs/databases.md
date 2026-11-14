# Bancos de dados locais

O Cloudforge gerencia PostgreSQL, MySQL, MongoDB e Redis no Docker da própria máquina. Use Docker Engine 28 ou superior em modo Linux. Cada instância tem um container, um volume e uma rede Docker próprios. Cada conta pode criar uma instância de cada tecnologia.

## Preparar as imagens

As imagens precisam estar no Docker local antes da criação. Obtenha as imagens abaixo em uma máquina com acesso ao registro. Para uma instalação offline, exporte as imagens com `docker save` e importe-as na máquina do Cloudforge com `docker load`.

```powershell
docker pull postgres:17.9-bookworm
docker pull mysql:8.4.8
docker pull mongo:8.0.20-noble
docker pull redis:7.4.8-bookworm
```

## Criar e conectar

1. Abra **Bancos de dados**, clique em **Criar banco de dados**, escolha a tecnologia e informe o limite de memória em MB. Use **0** para deixar a memória sem limite; o valor inicial é 1024 MB.
2. Aguarde o status **Online**, guarde a senha exibida uma única vez e baixe **Certificate CA (.crt)** no modal de credenciais.
3. Em **Conexão**, copie a porta local e a URL. Abra o DBeaver ou outro cliente na máquina que executa o Core, use o host `127.0.0.1` e configure a CA baixada como confiável para a conexão.

O usuário é `cloudforge`. Use o banco `cloudforge` no PostgreSQL, MySQL e MongoDB. No MongoDB, use também `cloudforge` como banco de autenticação. No Redis, use o índice `0` e o usuário ACL `cloudforge`. As quatro engines exigem TLS nas conexões TCP; não é necessário certificado de cliente.

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

A porta atribuída permanece a mesma após parar, iniciar ou reiniciar o container ou o Core. Containers autorizados podem conectar pela rede Docker da instância usando o nome do container e a porta interna da engine. A opção **Redefinir senha** invalida a senha anterior. A exclusão só prossegue após a confirmação pelo nome do banco.

## Atualizar instalações existentes

O Cloudforge emite automaticamente o par de cliente para bancos que já usam TLS, preservando o certificado do servidor e a CA. Bancos em execução ganham suporte a mTLS no próprio container; bancos parados recebem a configuração na próxima inicialização. Para instalações antigas sem TLS, continua valendo a migração original: o Core adequa o container, usando a imagem local, o volume, as redes e as credenciais existentes. Use a porta mostrada na aba **Conexão** e configure a CA no cliente antes de reconectar.

## Teste com Docker

Com as quatro imagens carregadas localmente, execute no diretório `services/core`.

```powershell
$env:CLOUDFORGE_TEST_DATABASES = "1"
go test ./internal/databases -run TestDockerDatabases -v
```
