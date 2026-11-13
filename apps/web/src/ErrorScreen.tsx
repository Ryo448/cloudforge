import { RefreshCw } from "lucide-react";
import "./error-screen.css";
import baseMascot from "./mascots/base.webp";
import cloudforgeMark from "./mascots/cloudforge-mark.webp";
import unavailableMascot from "./mascots/indisponivel.webp";
import deniedMascot from "./mascots/sem-autorizacao.webp";

type ErrorScreenProps = {
  kind: "denied" | "unavailable" | "unknown";
  onRetry: () => void;
};

type ErrorContent = {
  title: string;
  message: string;
  guidance: string;
  mascot: string;
  alt: string;
  status: string;
};

const messages = {
  denied: {
    title: "Acesso negado",
    message: "Você não tem autorização para acessar este recurso.",
    guidance:
      "Confira se está usando a conta correta ou peça acesso a quem administra este serviço.",
    mascot: deniedMascot,
    alt: "Pingo cruza os braços, irritado.",
    status: "403 · ACESSO RESTRITO",
  },
  unavailable: {
    title: "O Cloudforge não respondeu",
    message:
      "Não foi possível conectar ao serviço que controla suas aplicações.",
    guidance:
      "Confira se o Cloudforge está em execução no computador que hospeda seus projetos.",
    mascot: unavailableMascot,
    alt: "Pingo está sem saber o que aconteceu.",
    status: "SERVIÇO INDISPONÍVEL",
  },
  unknown: {
    title: "Não foi possível carregar esta página",
    message: "O Cloudforge encontrou um problema inesperado.",
    guidance:
      "Tente novamente. Se o problema continuar, confira o estado do serviço no aplicativo desktop.",
    mascot: baseMascot,
    alt: "Pingo está sorrindo e pronto para ajudar.",
    status: "ERRO DE CONEXÃO",
  },
} satisfies Record<ErrorScreenProps["kind"], ErrorContent>;

export function ErrorScreen({ kind, onRetry }: ErrorScreenProps) {
  const content = messages[kind];
  return (
    <main className="pingo-error" aria-labelledby="pingo-error-title">
      <header className="pingo-error-header">
        <a className="pingo-brand" href="/" aria-label="Cloudforge">
          <img src={cloudforgeMark} alt="" />
          <span className="pingo-brand-name">
            Cloud<span>forge</span>
          </span>
        </a>
        <span className="pingo-error-status">{content.status}</span>
      </header>
      <section className="pingo-error-content">
        <div className="pingo-error-copy">
          <h1 id="pingo-error-title">{content.title}</h1>
          <p className="pingo-error-message">{content.message}</p>
          <p className="pingo-error-guidance">{content.guidance}</p>
          <button className="pingo-error-action" onClick={onRetry}>
            <RefreshCw size={16} aria-hidden="true" />
            Tentar novamente
          </button>
        </div>
        <img
          className="pingo-error-mascot"
          src={content.mascot}
          alt={content.alt}
        />
      </section>
      <footer className="pingo-error-footer">
        Cloudforge · Controle de aplicações
      </footer>
    </main>
  );
}
