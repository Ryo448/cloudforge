import { useEffect, useRef, useState, type MouseEvent } from "react";
import { ArrowLeft, ExternalLink } from "lucide-react";
import { ErrorMessage } from "./index";
import { t } from "./i18n";
import { Translated } from "./Translated";
import "./databases.css";

const dashboard = "https://one.dash.cloudflare.com/";
const dnsLabel = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

const driverNames = {
  postgresql: "PostgreSQL",
  mysql: "MySQL",
  mongodb: "MongoDB",
  redis: "Redis",
};

export function DatabaseTCPTutorial({
  engine,
  subdomain,
  domain,
  onOpenExternal,
  onBack,
}: {
  engine: keyof typeof driverNames;
  subdomain: string;
  domain: string;
  onOpenExternal?: (url: string) => Promise<void>;
  onBack: () => void;
}) {
  const [error, setError] = useState("");
  const steps = useRef<HTMLOListElement>(null);
  useEffect(() => {
    steps.current?.focus();
  }, []);

  const label = subdomain.trim().toLowerCase();
  const zone = domain.trim().toLowerCase();
  const labels = zone.split(".");
  const hostname =
    dnsLabel.test(label) &&
    labels.length >= 2 &&
    labels.every((part) => dnsLabel.test(part))
      ? `${label}.${zone}`
      : "postgres.example.com";

  function openLink(event: MouseEvent<HTMLAnchorElement>) {
    if (!onOpenExternal) return;
    event.preventDefault();
    setError("");
    void onOpenExternal(event.currentTarget.href).catch((cause) => {
      setError(cause instanceof Error ? cause.message : String(cause));
    });
  }

  return (
    <>
      <ol
        className="db-tcp-tutorial-steps"
        ref={steps}
        tabIndex={0}
        aria-label={t("Como configurar o túnel TCP")}
      >
        <li>
          <h3>{t("Crie o aplicativo no Cloudflare Access")}</h3>
          <p>
            <Translated
              text="Abra o Zero Trust na conta que gerencia o túnel do Cloudforge. Se for a primeira vez, conclua a criação da equipe. Em {0}, clique em {1}, escolha {2} e selecione {3}."
              values={[
                <strong>Access controls → Applications</strong>,
                <strong>Create new application</strong>,
                <strong>Self-hosted and private</strong>,
                <strong>Add public hostname</strong>,
              ]}
            />
          </p>
          <p>
            <Translated
              text="Preencha Subdomain e Domain para chegar a {0}. Deixe Path vazio para proteger todo o endereço."
              values={[<code>{hostname}</code>]}
            />
          </p>
          <a
            href={dashboard}
            onClick={openLink}
            target="_blank"
            rel="noreferrer"
          >
            {t("Abrir Cloudflare Zero Trust")} <ExternalLink size={14} />
          </a>
        </li>
        <li>
          <h3>{t("Autorize os e-mails que vão conectar")}</h3>
          <p>
            <Translated
              text="Em {0}, crie uma política com a ação {1}. Na regra {2}, escolha {3} e informe apenas os e-mails autorizados. Selecione um método de login disponível para essas pessoas e salve o aplicativo."
              values={[
                <strong>Access policies</strong>,
                <strong>Allow</strong>,
                <strong>Include</strong>,
                <strong>Emails</strong>,
              ]}
            />
          </p>
        </li>
        <li>
          <h3>{t("Encontre a equipe e o AUD")}</h3>
          <p>
            <Translated
              text="Em {0}, copie o Team name. Se o domínio da equipe for {1}, preencha {2} no Cloudforge."
              values={[
                <strong>Zero Trust → Settings → Team name and domain</strong>,
                <code>minha-equipe.cloudflareaccess.com</code>,
                <code>minha-equipe</code>,
              ]}
            />
          </p>
          <p>
            <Translated
              text="Volte à lista de aplicações, selecione {0} no aplicativo criado e copie {1} em {2}."
              values={[
                <strong>Edit</strong>,
                <strong>Application Audience (AUD) Tag</strong>,
                <strong>Additional settings</strong>,
              ]}
            />
          </p>
        </li>
        <li>
          <h3>{t("Ative a rota no Cloudforge")}</h3>
          <p>
            <Translated
              text="No computador que hospeda o banco, volte ao formulário. Use o mesmo subdomínio e domínio do aplicativo, cole a equipe e o AUD e clique em {0}. Aguarde a reconexão do túnel."
              values={[<strong>{t("Ativar túnel TCP")}</strong>]}
            />
          </p>
          <p>
            <Translated
              text="Em {0}, copie o domínio da seção de acesso remoto e baixe {1}. Leve a CA ao computador que vai acessar o banco."
              values={[
                <strong>{t("Conexão")}</strong>,
                <strong>Certificate CA (.crt)</strong>,
              ]}
            />
          </p>
        </li>
        <DatabaseTCPClientSteps hostname={hostname} engine={engine} />
      </ol>
      <ErrorMessage error={error} />
      <div className="modal-actions">
        <button type="button" className="button" onClick={onBack}>
          <ArrowLeft size={16} />
          {t("Voltar à configuração")}
        </button>
      </div>
    </>
  );
}

export function DatabaseTCPClientSteps({
  hostname,
  engine,
}: {
  hostname?: string;
  engine?: keyof typeof driverNames;
}) {
  return (
    <>
      <li>
        <h3>{t("Abra o túnel no PC cliente")}</h3>
        <p>
          {hostname ? (
            <Translated
              text="No Cloudforge do PC cliente, abra {0} e clique em {1}. Informe {2} e uma porta local livre, como {3}. Aguarde o status Escutando."
              values={[
                <strong>
                  {t("Configurações")} → {t("Conexões TCP")}
                </strong>,
                <strong>{t("Adicionar conexão")}</strong>,
                <code>{hostname}</code>,
                <code>15432</code>,
              ]}
            />
          ) : (
            <Translated
              text="Clique em {0}. Informe o domínio exibido em {1} no computador que hospeda o banco, como {2}. Escolha uma porta local livre, como {3}, e clique em {4}. Aguarde o status Escutando."
              values={[
                <strong>{t("Adicionar conexão")}</strong>,
                <strong>{t("Acesso remoto via Cloudflare")}</strong>,
                <code>subdominio.example.com</code>,
                <code>15432</code>,
                <strong>{t("Conectar")}</strong>,
              ]}
            />
          )}
        </p>
        <p>
          {t(
            "Quando o cliente do banco abrir a conexão, o Cloudflare Access pode pedir login no navegador. Use um dos e-mails autorizados na política.",
          )}
        </p>
      </li>
      <li>
        <h3>{t("Conecte o cliente do banco")}</h3>
        <p>
          {engine ? (
            <Translated
              text="No DBeaver, escolha Nova conexão de banco de dados e selecione o driver {0}. Preencha os campos abaixo e use Testar conexão."
              values={[<strong>{driverNames[engine]}</strong>]}
            />
          ) : (
            t(
              "No DBeaver, escolha Nova conexão de banco de dados e selecione o driver do seu banco. Preencha os campos abaixo e use Testar conexão.",
            )
          )}
        </p>
        {(!engine || engine === "mongodb") && (
          <p>
            {t(
              "O driver MongoDB está no DBeaver PRO. Você também pode usar o MongoDB Compass.",
            )}
          </p>
        )}
        {(!engine || engine === "redis") && (
          <p>
            {t(
              "O driver Redis está no DBeaver PRO. Você também pode usar o Redis Insight.",
            )}
          </p>
        )}
        <dl>
          <dt>Host</dt>
          <dd>
            <code>127.0.0.1</code>
          </dd>
          <dt>Port</dt>
          <dd>{t("Porta escolhida em Conexões TCP")}</dd>
          <dt>{t("Usuário")}</dt>
          <dd>
            <code>cloudforge</code>
          </dd>
          <dt>{t("Senha")}</dt>
          <dd>{t("Senha da instância")}</dd>
          {engine !== "redis" && (
            <>
              <dt>{t("Nome do banco")}</dt>
              <dd>
                {engine ? (
                  <code>cloudforge</code>
                ) : (
                  <Translated
                    text="{0} para PostgreSQL, MySQL e MongoDB. No MongoDB, use o mesmo nome para a autenticação."
                    values={[<code>cloudforge</code>]}
                  />
                )}
              </dd>
            </>
          )}
          {(!engine || engine === "redis") && (
            <>
              <dt>
                {t("Índice do banco")}
                {!engine && " (Redis)"}
              </dt>
              <dd>
                <code>0</code>
              </dd>
            </>
          )}
          {engine === "mongodb" && (
            <>
              <dt>Authentication database</dt>
              <dd>
                <code>cloudforge</code>
              </dd>
            </>
          )}
        </dl>
        <p>
          <strong>SSL / TLS</strong>
        </p>
        <dl>
          {(!engine || engine === "postgresql") && (
            <>
              <dt>SSL Mode{!engine && " (PostgreSQL)"}</dt>
              <dd>
                <code>verify-full</code>
              </dd>
            </>
          )}
          {(!engine || engine === "mysql") && (
            <>
              <dt>Driver properties · sslMode{!engine && " (MySQL)"}</dt>
              <dd>
                <code>VERIFY_IDENTITY</code>
              </dd>
            </>
          )}
          <dt>CA Certificate</dt>
          <dd>{t("Arquivo ca.crt baixado do banco")}</dd>
        </dl>
        <p>
          {t(
            "Habilite SSL/TLS e a verificação do servidor. Se o navegador abrir durante o teste, conclua o login e teste a conexão novamente.",
          )}
        </p>
        <details>
          <summary>{t("Se a conexão falhar")}</summary>
          <p>
            {t(
              "Em caso de 403, confira o e-mail na política Allow, o subdomínio, a equipe e o AUD. Se a conexão expirar ou for recusada, confira o status em Conexões TCP, a porta no cliente e se o banco está online.",
            )}
          </p>
        </details>
      </li>
    </>
  );
}
