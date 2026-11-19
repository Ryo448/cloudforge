import { useEffect, useRef, useState, type MouseEvent } from "react";
import { ArrowLeft, ExternalLink } from "lucide-react";
import { ErrorMessage } from "./index";
import { t } from "./i18n";
import { Translated } from "./Translated";

const dashboard = "https://one.dash.cloudflare.com/";
const downloads =
  "https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/";
const dnsLabel = /^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/;

export function PostgresTCPTutorial({
  subdomain,
  domain,
  onOpenExternal,
  onBack,
}: {
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
              text="Em {0}, copie o comando da seção de acesso remoto e baixe {1}. Leve a CA ao computador que vai rodar o pgAdmin."
              values={[
                <strong>{t("Conexão")}</strong>,
                <strong>Certificate CA (.crt)</strong>,
              ]}
            />
          </p>
        </li>
        <li>
          <h3>{t("Abra o túnel no PC cliente")}</h3>
          <p>
            {t(
              "Instale o cloudflared no PC cliente e execute o comando copiado depois de ativar a rota. Mantenha o terminal aberto enquanto usar o banco.",
            )}
          </p>
          <a
            href={downloads}
            onClick={openLink}
            target="_blank"
            rel="noreferrer"
          >
            {t("Baixar cloudflared")} <ExternalLink size={14} />
          </a>
          <code className="db-tcp-tutorial-command">
            {`cloudflared access tcp --hostname ${hostname} --url 127.0.0.1:15432`}
          </code>
          <p>
            <Translated
              text="Se baixou somente o executável no Windows, abra o PowerShell na pasta do arquivo e substitua {0} no início do comando pelo nome do arquivo, por exemplo {1}."
              values={[
                <code>cloudflared</code>,
                <code>{String.raw`.\cloudflared-windows-amd64.exe`}</code>,
              ]}
            />
          </p>
          <p>
            {t(
              "Se o navegador pedir login, use um dos e-mails autorizados no Access. Se a porta 15432 estiver ocupada, troque-a no comando e no pgAdmin.",
            )}
          </p>
        </li>
        <li>
          <h3>{t("Cadastre o servidor no pgAdmin")}</h3>
          <p>
            <Translated
              text="Em {0}, dê um nome à conexão e preencha os campos abaixo."
              values={[<strong>Register → Server</strong>]}
            />
          </p>
          <p>
            <strong>Connection</strong>
          </p>
          <dl>
            <dt>Host name/address</dt>
            <dd>
              <code>127.0.0.1</code>
            </dd>
            <dt>Port</dt>
            <dd>
              <code>15432</code>
            </dd>
            <dt>Maintenance database / Username</dt>
            <dd>
              <code>cloudforge</code>
            </dd>
            <dt>Password</dt>
            <dd>{t("Senha da instância PostgreSQL")}</dd>
          </dl>
          <p>
            <strong>Parameters</strong>
          </p>
          <dl>
            <dt>SSL mode</dt>
            <dd>
              <code>verify-full</code>
            </dd>
            <dt>Root certificate</dt>
            <dd>{t("Arquivo ca.crt baixado do banco")}</dd>
            <dt>Connection timeout</dt>
            <dd>{t("10 segundos")}</dd>
          </dl>
          <p>
            {t(
              "Salve a conexão e conclua o login no navegador, se solicitado.",
            )}
          </p>
          <details>
            <summary>{t("Se a conexão falhar")}</summary>
            <p>
              {t(
                "Em caso de 403, confira o e-mail na política Allow, o subdomínio, a equipe e o AUD do aplicativo. Se a conexão expirar ou for recusada, confira se o terminal está aberto, se a porta corresponde à do comando e se o PostgreSQL está online.",
              )}
            </p>
          </details>
        </li>
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
