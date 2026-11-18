import { t } from "../../../packages/ui/i18n";
import { Translated } from "../../../packages/ui/Translated";
import { useState, type MouseEvent } from "react";
import { ExternalLink } from "lucide-react";
import { ErrorMessage, Modal } from "../../../packages/ui";
import { message } from "./api";
import "./google-drive-tutorial.css";

const cloud = "https://console.cloud.google.com";

export function GoogleDriveTutorial({
  onClose,
  onBack,
}: {
  onClose: () => void;
  onBack: () => void;
}) {
  const [error, setError] = useState("");

  async function openLink(event: MouseEvent<HTMLAnchorElement>) {
    event.preventDefault();
    const url = event.currentTarget.href;
    setError("");
    try {
      await window.cloudforge.openExternal(url);
    } catch (err) {
      setError(message(err));
    }
  }

  return (
    <Modal
      title={t("Como integrar o Google Drive")}
      className="google-drive-tutorial"
      onClose={onClose}
    >
      <p>{t("Use o mesmo projeto do Google Cloud em todas as etapas.")}</p>
      <ol>
        <li>
          <h3>{t("Crie ou selecione um projeto")}</h3>
          <p>
            <Translated
              text="Entre com sua conta Google no console. Você pode selecionar um projeto existente ou criar um chamado {0}."
              values={[<strong>Cloudforge</strong>]}
            />
          </p>
          <a href={cloud + "/projectcreate"} onClick={openLink}>
            {t("Criar projeto no Google Cloud")} <ExternalLink size={14} />
          </a>
        </li>
        <li>
          <h3>{t("Ative a API do Google Drive")}</h3>
          <p>
            <Translated
              text="Selecione o projeto e clique em {0}."
              values={[<strong>{t("Ativar")}</strong>]}
            />
          </p>
          <a
            href={cloud + "/flows/enableapi?apiid=drive.googleapis.com"}
            onClick={openLink}
          >
            {t("Ativar a API do Google Drive")} <ExternalLink size={14} />
          </a>
        </li>
        <li>
          <h3>{t("Configure o consentimento e o acesso")}</h3>
          <p>
            <Translated
              text="Na {0}, abra {1} e clique em {2} se for a primeira vez. Informe o nome do app, o e-mail de suporte e o de contato. Para uma conta Google pessoal, selecione o público {3}."
              values={[
                <strong>{t("Plataforma de autenticação do Google")}</strong>,
                <strong>{t("Branding")}</strong>,
                <strong>{t("Iniciar")}</strong>,
                <strong>{t("Externo")}</strong>,
              ]}
            />
          </p>
          <p>
            <Translated
              text="Se o app estiver {0}, vá a {1} e adicione o e-mail da conta que armazenará os snapshots em {2}. Em {3}, adicione e salve o escopo abaixo."
              values={[
                <strong>{t("Em teste")}</strong>,
                <strong>{t("Público")}</strong>,
                <strong>{t("Usuários de teste")}</strong>,
                <strong>{t("Acesso a dados")}</strong>,
              ]}
            />
          </p>
          <p>
            <code>https://www.googleapis.com/auth/drive.file</code>
          </p>
          <div className="google-drive-tutorial-links">
            <a href={cloud + "/auth/branding"} onClick={openLink}>
              {t("Branding")} <ExternalLink size={14} />
            </a>
            <a href={cloud + "/auth/audience"} onClick={openLink}>
              {t("Público")} <ExternalLink size={14} />
            </a>
            <a href={cloud + "/auth/scopes"} onClick={openLink}>
              {t("Acesso a dados")} <ExternalLink size={14} />
            </a>
          </div>
        </li>
        <li>
          <h3>{t("Crie um cliente OAuth para computador")}</h3>
          <p>
            <Translated
              text="Em {0}, clique em {1}. Escolha {2} como tipo, dê um nome ao cliente e clique em {3}."
              values={[
                <strong>{t("Clientes")}</strong>,
                <strong>{t("Criar cliente")}</strong>,
                <strong>{t("App para computador")}</strong>,
                <strong>{t("Criar")}</strong>,
              ]}
            />
          </p>
          <a href={cloud + "/auth/clients"} onClick={openLink}>
            {t("Abrir Clientes OAuth")} <ExternalLink size={14} />
          </a>
        </li>
        <li>
          <h3>{t("Cole as credenciais no Cloudforge")}</h3>
          <p>
            <Translated
              text="Copie o ID e o segredo exibidos pelo Google. Se usar o JSON baixado ao criar o cliente, abra o arquivo e copie os valores de {0} e {1} na seção {2}."
              values={[
                <code>client_id</code>,
                <code>client_secret</code>,
                <code>installed</code>,
              ]}
            />
          </p>
          <p>
            <Translated
              text="Volte a {0}, cole cada valor nos campos {1} e {2} e clique em {3}."
              values={[
                <strong>{t("Configurações → Google Drive")}</strong>,
                <strong>{t("ID do cliente OAuth")}</strong>,
                <strong>{t("Segredo do cliente OAuth")}</strong>,
                <strong>{t("Salvar cliente OAuth")}</strong>,
              ]}
            />
          </p>
        </li>
        <li>
          <h3>{t("Conecte a conta Google")}</h3>
          <p>
            <Translated
              text="Clique em {0}, escolha no navegador a conta que vai guardar os backups e autorize o acesso. Volte ao Cloudforge. A conexão estará pronta quando o e-mail aparecer em {1}."
              values={[
                <strong>{t("Conectar conta Google")}</strong>,
                <strong>{t("Conta Google")}</strong>,
              ]}
            />
          </p>
        </li>
      </ol>
      <p>
        {t(
          "Se o app estiver em teste, a autorização do Drive pode expirar após sete dias. Nesse caso, conecte a conta novamente.",
        )}
      </p>
      <ErrorMessage error={error} />
      <div className="form-footer google-drive-tutorial-footer">
        <button className="button primary" type="button" onClick={onBack}>
          {t("Voltar à configuração")}
        </button>
      </div>
    </Modal>
  );
}
