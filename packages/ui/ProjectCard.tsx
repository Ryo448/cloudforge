import { useId, useState } from "react";
import {
  ArrowUpRight,
  ChevronRight,
  Cpu,
  MemoryStick,
  Timer,
} from "lucide-react";
import type { RemoteProject, UsageSample } from "../shared-types";
import pythonLogo from "../../assets/runtime/python.png";
import nodeLogo from "../../assets/runtime/nodejs.svg";
import {
  Actions,
  bytes,
  ErrorMessage,
  Modal,
  percent,
  Spinner,
  Status,
  uptime,
} from "./index";

const historyWindow = 120_000;
const chartWidth = 480;
const chartHeight = 64;

function usagePaths(
  samples: UsageSample[],
  metric: "cpu" | "memory",
  now: number,
) {
  const scale = Math.max(
    metric === "cpu" ? 100 : 128 * 1024 ** 2,
    ...samples.map((s) => s[metric] ?? 0),
  );
  const segments: [number, number][][] = [];
  let segment: [number, number][] = [];
  let previousTime = 0;

  for (const sample of samples) {
    const value = sample[metric];
    if (value == null || sample.time - previousTime > 6000) {
      segment = [];
    }
    if (value == null) continue;
    if (!segment.length) segments.push(segment);
    segment.push([
      chartWidth * (1 - (now - sample.time) / historyWindow),
      chartHeight - 2 - ((chartHeight - 8) * value) / scale,
    ]);
    previousTime = sample.time;
  }
  return segments;
}

function ProjectUsage({
  history,
  online,
}: {
  history?: UsageSample[];
  online: boolean;
}) {
  const fill = useId();
  const now = Math.max(Date.now(), history?.at(-1)?.time ?? 0);
  const samples =
    history?.filter((sample) => now - sample.time <= historyWindow) ?? [];
  const cpu = usagePaths(samples, "cpu", now);
  const memory = usagePaths(samples, "memory", now);
  if (!cpu.length && !memory.length) {
    return (
      <div className="project-chart project-chart-empty">
        {online ? "Coletando métricas…" : "Sem amostras recentes"}
      </div>
    );
  }
  return (
    <svg
      className="project-chart"
      viewBox={`0 0 ${chartWidth} ${chartHeight}`}
      preserveAspectRatio="none"
      role="img"
      aria-label="Histórico de CPU e RAM nos últimos 2 minutos"
    >
      <defs>
        <linearGradient id={fill} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" className="project-chart-fill-top" />
          <stop offset="100%" className="project-chart-fill-bottom" />
        </linearGradient>
      </defs>
      <path
        className="project-chart-guide"
        d={`M0 22H${chartWidth} M0 44H${chartWidth}`}
      />
      {memory.map((segment) => {
        const first = segment[0];
        const last = segment[segment.length - 1];
        return (
          segment.length > 1 && (
            <polygon
              key={first[0]}
              fill={`url(#${fill})`}
              points={`${first[0]},${chartHeight} ${segment.join(" ")} ${last[0]},${chartHeight}`}
            />
          )
        );
      })}
      {(
        [
          ["memory", memory],
          ["cpu", cpu],
        ] as const
      ).map(([metric, segments]) =>
        segments.map((segment) => {
          const [x, y] = segment[0];
          return segment.length === 1 ? (
            <circle
              className={`project-chart-point ${metric}`}
              key={x}
              cx={x}
              cy={y}
              r="2"
            />
          ) : (
            <polyline
              className={`project-chart-line ${metric}`}
              key={x}
              points={segment.join(" ")}
            />
          );
        }),
      )}
    </svg>
  );
}

export function RuntimeLogo({
  runtime,
}: {
  runtime: RemoteProject["runtime"];
}) {
  return (
    <img
      className="project-runtime-logo"
      src={runtime === "node" ? nodeLogo : pythonLogo}
      alt=""
    />
  );
}

export function ProjectCard({
  project,
  onAction,
  onOpen,
  onOpenDomain,
}: {
  project: RemoteProject;
  onAction: (action: string) => Promise<void>;
  onOpen?: () => void;
  onOpenDomain?: (url: string) => Promise<void>;
}) {
  const title = useId();
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const { execution, hostname } = project;
  const runtime = project.runtime === "node" ? "Node.js" : "Python";
  const domain = hostname ? `https://${hostname}` : "";
  async function run(action: string) {
    setConfirm("");
    setError("");
    setBusy(true);
    try {
      await onAction(action);
    } catch (e) {
      setError(
        e instanceof Error ? e.message : "Não foi possível executar a ação.",
      );
    } finally {
      setBusy(false);
    }
  }
  return (
    <article className="project-card" aria-labelledby={title}>
      <div className="project-card-header">
        <RuntimeLogo runtime={project.runtime} />
        <div className="project-card-identity">
          <h2 id={title}>
            {onOpen ? (
              <button
                className="project-card-open"
                type="button"
                onClick={onOpen}
              >
                {project.name}
                <ChevronRight size={15} aria-hidden="true" />
              </button>
            ) : (
              project.name
            )}
          </h2>
          <span className="muted">{runtime}</span>
        </div>
        {busy && <Spinner />}
      </div>
      <div className="project-card-chart-header">
        {domain && (
          <a
            className="project-card-domain"
            href={domain}
            target="_blank"
            rel="noopener noreferrer"
            title={hostname}
            onClick={
              onOpenDomain
                ? (event) => {
                    event.preventDefault();
                    setError("");
                    void onOpenDomain(domain).catch(() =>
                      setError("Não foi possível abrir o domínio."),
                    );
                  }
                : undefined
            }
          >
            <span>{hostname}</span>
            <ArrowUpRight size={13} aria-hidden="true" />
          </a>
        )}
        <span className="project-card-window">Últimos 2 min</span>
      </div>
      <ProjectUsage
        history={project.usageHistory}
        online={execution.actualState === "online"}
      />
      <div className="project-card-metrics">
        <span className="project-card-metric">
          <Cpu className="cpu" size={14} aria-hidden="true" /> CPU{" "}
          <strong>{percent(execution.cpu)}</strong>
        </span>
        <span className="project-card-metric">
          <MemoryStick className="memory" size={14} aria-hidden="true" /> RAM{" "}
          <strong>
            {execution.memory == null ? "—" : bytes(execution.memory)}
          </strong>
        </span>
        <span
          className="project-card-metric"
          aria-label={`Tempo ativo ${uptime(execution.startedAt)}`}
        >
          <Timer size={14} aria-hidden="true" />
          <strong>{uptime(execution.startedAt)}</strong>
        </span>
        <Status state={execution.actualState} />
      </div>
      <div className="project-card-controls">
        <ErrorMessage error={error || execution.lastError} />
        <Actions
          state={execution.actualState}
          busy={busy}
          onAction={(action) =>
            action === "start" ? void run(action) : setConfirm(action)
          }
        />
      </div>
      {confirm && (
        <Modal
          title={
            confirm === "stop" ? "Parar aplicação?" : "Reiniciar aplicação?"
          }
          onClose={() => setConfirm("")}
        >
          <p>
            {project.name}{" "}
            {confirm === "stop"
              ? "será encerrado."
              : "ficará indisponível durante a reinicialização."}
          </p>
          <div className="form-footer">
            <button className="button" onClick={() => setConfirm("")}>
              Cancelar
            </button>
            <button
              className="button primary"
              onClick={() => void run(confirm)}
            >
              {confirm === "stop" ? "Parar" : "Reiniciar"}
            </button>
          </div>
        </Modal>
      )}
    </article>
  );
}
