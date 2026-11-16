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
      title="Como integrar o Google Drive"
      className="google-drive-tutorial"
      onClose={onClose}
    >
      <p>Use o mesmo projeto do Google Cloud em todas as etapas.</p>
      <ol>
        <li>
          <h3>Crie ou selecione um projeto</h3>
          <p>
            Entre com sua conta Google no console. Você pode selecionar um
            projeto existente ou criar um chamado <strong>Cloudforge</strong>.
          </p>
          <a href={cloud + "/projectcreate"} onClick={openLink}>
            Criar projeto no Google Cloud <ExternalLink size={14} />
          </a>
        </li>
        <li>
          <h3>Ative a API do Google Drive</h3>
          <p>
            Selecione o projeto e clique em <strong>Ativar</strong>.
          </p>
          <a
            href={cloud + "/flows/enableapi?apiid=drive.googleapis.com"}
            onClick={openLink}
          >
            Ativar a API do Google Drive <ExternalLink size={14} />
          </a>
        </li>
        <li>
          <h3>Configure o consentimento e o acesso</h3>
          <p>
            Na <strong>Plataforma de autenticação do Google</strong>, abra
            <strong> Branding</strong> e clique em <strong>Iniciar</strong> se
            for a primeira vez. Informe o nome do app, o e-mail de suporte e o
            de contato. Para uma conta Google pessoal, selecione o público
            <strong> Externo</strong>.
          </p>
          <p>
            Se o app estiver <strong>Em teste</strong>, vá a{" "}
            <strong>Público</strong> e adicione o e-mail da conta que armazenará
            os snapshots em
            <strong> Usuários de teste</strong>. Em{" "}
            <strong>Acesso a dados</strong>, adicione e salve o escopo abaixo.
          </p>
          <p>
            <code>https://www.googleapis.com/auth/drive.file</code>
          </p>
          <div className="google-drive-tutorial-links">
            <a href={cloud + "/auth/branding"} onClick={openLink}>
              Branding <ExternalLink size={14} />
            </a>
            <a href={cloud + "/auth/audience"} onClick={openLink}>
              Público <ExternalLink size={14} />
            </a>
            <a href={cloud + "/auth/scopes"} onClick={openLink}>
              Acesso a dados <ExternalLink size={14} />
            </a>
          </div>
        </li>
        <li>
          <h3>Crie um cliente OAuth para computador</h3>
          <p>
            Em <strong>Clientes</strong>, clique em{" "}
            <strong>Criar cliente</strong>. Escolha{" "}
            <strong>App para computador</strong> como tipo, dê um nome ao
            cliente e clique em <strong>Criar</strong>.
          </p>
          <a href={cloud + "/auth/clients"} onClick={openLink}>
            Abrir Clientes OAuth <ExternalLink size={14} />
          </a>
        </li>
        <li>
          <h3>Cole as credenciais no Cloudforge</h3>
          <p>
            Copie o ID e o segredo exibidos pelo Google. Se usar o JSON baixado
            ao criar o cliente, abra o arquivo e copie os valores de{" "}
            <code>client_id</code> e <code>client_secret</code> na seção{" "}
            <code>installed</code>.
          </p>
          <p>
            Volte a <strong>Configurações → Google Drive</strong>, cole cada
            valor nos campos <strong>ID do cliente OAuth</strong> e
            <strong> Segredo do cliente OAuth</strong> e clique em
            <strong> Salvar cliente OAuth</strong>.
          </p>
        </li>
        <li>
          <h3>Conecte a conta Google</h3>
          <p>
            Clique em <strong>Conectar conta Google</strong>, escolha no
            navegador a conta que vai guardar os backups e autorize o acesso.
            Volte ao Cloudforge. A conexão estará pronta quando o e-mail
            aparecer em <strong>Conta Google</strong>.
          </p>
        </li>
      </ol>
      <p>
        Se o app estiver em teste, a autorização do Drive pode expirar após sete
        dias. Nesse caso, conecte a conta novamente.
      </p>
      <ErrorMessage error={error} />
      <div className="form-footer google-drive-tutorial-footer">
        <button className="button primary" type="button" onClick={onBack}>
          Voltar à configuração
        </button>
      </div>
    </Modal>
  );
}
