import { useCallback, useEffect, useState, type FormEvent } from "react";
import {
  CalendarClock,
  Cloud,
  Download,
  History,
  Plus,
  RefreshCw,
  RotateCcw,
  Settings2,
  Upload,
} from "lucide-react";
import { bytes, ErrorMessage, Modal, Spinner } from "./index";
import type { GoogleDriveState } from "../shared-types";
import "./database-snapshots.css";

type Job = {
  id: string;
  status: "running" | "success" | "error" | "warning";
  detail: string;
  progress?: number;
};
type Schedule = {
  frequency: "manual" | "daily" | "weekly";
  days: number[];
  times: string[];
  limit: number;
  last?: string;
  next?: string;
};
type DriveSnapshot = {
  id: string;
  driveId: string;
  databaseId: string;
  engine: string;
  created: string;
  mode: "automatic" | "manual";
  size: number;
  name: string;
  version: string;
  machine: string;
};
type SnapshotState = {
  drive: GoogleDriveState;
  schedule: Schedule;
  job?: Job;
  snapshots: DriveSnapshot[] | null;
  listError?: string;
};
type ChosenFile = { name: string; token: string };
type RestoreChoice =
  | { snapshot: DriveSnapshot; file?: never }
  | { file: ChosenFile; snapshot?: never };

export type SnapshotDesktop = {
  pickSnapshot: (id: string, engine: string) => Promise<ChosenFile | null>;
  importSnapshot: (id: string, token: string) => Promise<Job>;
  downloadSnapshot: (
    id: string,
    driveId: string,
    name: string,
  ) => Promise<Job | null>;
};

const weekdays = ["Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"];
const dateTime = new Intl.DateTimeFormat("pt-BR", {
  dateStyle: "medium",
  timeStyle: "short",
});
const showDate = (value: string) => dateTime.format(new Date(value));
const errorText = (err: unknown) =>
  err instanceof Error
    ? err.message.replace(/^Error invoking remote method '[^']+': Error: /, "")
    : "Não foi possível concluir a operação.";

export function DatabaseSnapshots({
  database,
  engineName,
  request,
  desktop,
  onOpenDriveSettings,
}: {
  database: { id: string; name: string; engine: string; state: string };
  engineName: string;
  request: <T>(method: string, path: string, body?: unknown) => Promise<T>;
  desktop: SnapshotDesktop;
  onOpenDriveSettings: () => void;
}) {
  const route = "/api/databases/" + database.id + "/snapshots";
  const [summary, setSummary] = useState<SnapshotState>();
  const [list, setList] = useState<DriveSnapshot[]>([]);
  const [listError, setListError] = useState("");
  const [loading, setLoading] = useState(true);
  const [refresh, setRefresh] = useState(0);
  const [error, setError] = useState("");
  const [action, setAction] = useState("");
  const [settings, setSettings] = useState<Schedule>();
  const [recoverable, setRecoverable] = useState<DriveSnapshot[]>();
  const [choice, setChoice] = useState<RestoreChoice>();
  const [confirmLimit, setConfirmLimit] = useState(false);
  const running = summary?.job?.status === "running";
  const connected =
    !!summary?.drive.configured &&
    !!summary.drive.account &&
    !summary.drive.error &&
    !summary.drive.pending;
  const busy = !!action || running;
  const online = database.state === "running";
  const terminalJob = running ? "" : summary?.job?.id;
  const accountKey = summary?.drive.account?.id || "";

  useEffect(() => {
    let active = true;
    let querying = false;
    async function status() {
      if (querying) return;
      querying = true;
      try {
        const data = await request<SnapshotState>("GET", route + "/status");
        if (active) setSummary(data);
      } catch (err) {
        if (active) setError(errorText(err));
      } finally {
        querying = false;
      }
    }
    void status();
    const timer = window.setInterval(status, 1400);
    return () => {
      active = false;
      window.clearInterval(timer);
    };
  }, [route, request]);

  useEffect(() => {
    let active = true;
    if (!accountKey) {
      setList([]);
      setListError("");
      setLoading(false);
      return;
    }
    setLoading(true);
    setListError("");
    request<SnapshotState>("GET", route)
      .then((data) => {
        if (!active) return;
        setList(data.snapshots || []);
        setListError(data.listError || "");
      })
      .catch((err: unknown) => {
        if (active) setListError(errorText(err));
      })
      .finally(() => {
        if (active) setLoading(false);
      });
    return () => {
      active = false;
    };
  }, [accountKey, refresh, route, request, terminalJob]);

  const run = useCallback(
    async (label: string, operation: () => Promise<void>) => {
      setError("");
      setAction(label);
      try {
        await operation();
      } catch (err) {
        setError(errorText(err));
      } finally {
        setAction("");
      }
    },
    [],
  );

  function showJob(job: Job | null) {
    if (job) setSummary((prev) => prev && { ...prev, job });
  }

  function saveSettings(event?: FormEvent, confirmed = false) {
    event?.preventDefault();
    if (!settings || !summary) return;
    if (settings.limit < summary.schedule.limit && !confirmed) {
      setConfirmLimit(true);
      return;
    }
    void run("Salvando configurações", async () => {
      await request("PUT", route + "/schedule", {
        schedule: settings,
        confirm: confirmed,
      });
      setSettings(undefined);
      setConfirmLimit(false);
      setSummary(await request<SnapshotState>("GET", route + "/status"));
    });
  }

  function startRestore() {
    if (!choice) return;
    void run("Preparando restauração", async () => {
      showJob(
        choice.file
          ? await desktop.importSnapshot(database.id, choice.file.token)
          : await request<Job>("POST", route + "/restore", {
              driveId: choice.snapshot.driveId,
              confirm: true,
            }),
      );
      setChoice(undefined);
    });
  }

  return (
    <section className="snapshots" aria-labelledby="snapshots-heading">
      <header className="snapshots-header">
        <div>
          <h1 id="snapshots-heading">Snapshots do banco de dados</h1>
          <p>
            Guarde cópias do {database.name} no Google Drive e recupere os dados
            quando precisar.
          </p>
        </div>
        <div className="snapshots-actions">
          <button
            type="button"
            className="button"
            disabled={!summary || busy}
            onClick={() => setSettings(summary?.schedule)}
          >
            <Settings2 size={15} /> Configurações
          </button>
          <button
            type="button"
            className="button primary"
            disabled={!connected || !online || busy}
            onClick={() =>
              void run("Preparando snapshot", async () =>
                showJob(await request<Job>("POST", route)),
              )
            }
          >
            <Plus size={17} /> Realizar snapshot
          </button>
        </div>
      </header>

      <div className="snapshots-panel">
        <div className="snapshots-status-row">
          <Cloud size={19} aria-hidden="true" />
          <div>
            <strong>Google Drive</strong>
            <p>
              {!summary
                ? "Verificando conexão…"
                : summary.drive.account?.email ||
                  (summary.drive.account
                    ? "Conta conectada"
                    : summary.drive.pending
                      ? "Aguardando autorização no navegador"
                      : "Nenhuma conta Google cadastrada.")}
            </p>
          </div>
          {summary && !connected && (
            <button className="button" onClick={onOpenDriveSettings}>
              Abrir configurações do Google Drive
            </button>
          )}
        </div>
        {summary?.drive.error && <ErrorMessage error={summary.drive.error} />}
        <div className="snapshots-status-row snapshots-schedule">
          <CalendarClock size={19} aria-hidden="true" />
          <div>
            <strong>
              {summary?.schedule.frequency === "daily"
                ? "Snapshots automáticos diários"
                : summary?.schedule.frequency === "weekly"
                  ? "Snapshots automáticos semanais"
                  : "Snapshots automáticos desativados"}
            </strong>
            <p>
              {summary && summary.schedule.frequency !== "manual" && !connected
                ? "Aguardando uma conta Google Drive."
                : summary?.schedule.next
                  ? "Próxima execução " + showDate(summary.schedule.next)
                  : "Os horários de execução seguem o fuso do computador."}
              {summary?.schedule.last && (
                <span className="snapshots-last">
                  Última execução programada {showDate(summary.schedule.last)}
                </span>
              )}
            </p>
          </div>
          <span className="snapshots-count">
            {connected && !loading && !listError
              ? list.length + " / "
              : "Limite de "}
            {summary?.schedule.limit ?? "—"} snapshots
          </span>
        </div>
      </div>

      <ErrorMessage error={error} />
      {(summary?.job || action) && (
        <div
          className={"notice snapshots-job " + (summary?.job?.status || "")}
          role="status"
        >
          {(running || action) && <Spinner />}
          <span>{action || summary?.job?.detail}</span>
          {running && summary?.job?.progress !== undefined && (
            <>
              <progress
                aria-label="Progresso da operação"
                max={100}
                value={summary.job.progress}
              />
              <span>{summary.job.progress}%</span>
            </>
          )}
        </div>
      )}

      <div className="snapshots-list-head">
        <h2>Histórico de snapshots</h2>
        <div className="snapshots-actions">
          <button
            className="button"
            disabled={!connected || busy}
            onClick={() =>
              void run("Buscando backups no Drive", async () =>
                setRecoverable(
                  await request<DriveSnapshot[]>("GET", route + "/recoverable"),
                ),
              )
            }
          >
            <Cloud size={15} /> Outros backups do Drive
          </button>
          <button
            className="button"
            disabled={!online || busy}
            onClick={() =>
              void run("Selecionando arquivo", async () => {
                const file = await desktop.pickSnapshot(
                  database.id,
                  database.engine,
                );
                if (file) setChoice({ file });
              })
            }
          >
            <Upload size={15} /> Importar backup
          </button>
          <button
            className="icon-button"
            aria-label="Atualizar histórico"
            title="Atualizar histórico"
            disabled={!connected || loading}
            onClick={() => setRefresh((value) => value + 1)}
          >
            <RefreshCw size={15} />
          </button>
        </div>
      </div>

      {listError ? (
        <ErrorMessage error={listError} />
      ) : loading || !summary ? (
        <div className="snapshots-empty" role="status">
          <Spinner /> Carregando snapshots…
        </div>
      ) : list.length === 0 ? (
        <div className="snapshots-empty">
          <History size={24} aria-hidden="true" />
          <strong>Nenhum snapshot neste banco</strong>
          <span>
            {connected
              ? "Use Realizar snapshot para salvar a primeira versão."
              : "Conecte uma conta Google nas configurações do Cloudforge para criar e consultar snapshots."}
          </span>
        </div>
      ) : (
        <ol className="snapshots-timeline">
          {list.map((snapshot, index) => (
            <li className="snapshots-item" key={snapshot.driveId}>
              <div className="snapshots-item-info">
                <strong>
                  <time dateTime={snapshot.created}>
                    {showDate(snapshot.created)}
                  </time>
                </strong>
                {index === 0 && (
                  <span className="snapshots-recent">Mais recente</span>
                )}
                <div className="snapshots-meta">
                  <span>
                    {snapshot.mode === "automatic" ? "Automático" : "Manual"}
                  </span>
                  <span>{bytes(snapshot.size)}</span>
                  <span>
                    {engineName} {snapshot.version}
                  </span>
                </div>
                <code title={snapshot.id}>{snapshot.id}</code>
              </div>
              <div className="snapshots-actions">
                <button
                  className="button"
                  disabled={!online || busy}
                  onClick={() => setChoice({ snapshot })}
                >
                  <RotateCcw size={15} /> Restaurar
                </button>
                <button
                  className="button"
                  disabled={busy}
                  onClick={() =>
                    void run("Selecionando destino", async () =>
                      showJob(
                        await desktop.downloadSnapshot(
                          database.id,
                          snapshot.driveId,
                          snapshot.name,
                        ),
                      ),
                    )
                  }
                >
                  <Download size={15} /> Baixar
                </button>
              </div>
            </li>
          ))}
        </ol>
      )}

      {settings && !confirmLimit && (
        <Modal
          title="Configurações de snapshots"
          className="snapshots-settings-modal"
          onClose={() => setSettings(undefined)}
          closeDisabled={!!action}
        >
          <ScheduleForm
            value={settings}
            change={setSettings}
            onSubmit={saveSettings}
            cancel={() => setSettings(undefined)}
            busy={!!action}
            error={error}
          />
        </Modal>
      )}
      {confirmLimit && settings && (
        <Modal
          title="Reduzir o limite de versões"
          onClose={() => setConfirmLimit(false)}
          closeDisabled={!!action}
        >
          <div className="snapshots-dialog">
            <p>
              Serão mantidos os {settings.limit} snapshots mais recentes deste
              banco. As versões excedentes serão excluídas do Google Drive.
            </p>
            <ErrorMessage error={error} />
            <div className="snapshots-dialog-actions">
              <button
                className="button"
                onClick={() => setConfirmLimit(false)}
                disabled={!!action}
              >
                Voltar
              </button>
              <button
                className="button danger"
                disabled={!!action}
                onClick={() => saveSettings(undefined, true)}
              >
                Aplicar limite
              </button>
            </div>
          </div>
        </Modal>
      )}
      {recoverable && !choice && (
        <Modal
          title="Outros backups do Google Drive"
          wide
          onClose={() => setRecoverable(undefined)}
        >
          <div className="snapshots-dialog">
            <p>
              Selecione um snapshot de {engineName} de outro banco ou instalação
              para restaurar em {database.name}.
            </p>
            {recoverable.length === 0 ? (
              <p>Nenhum backup compatível encontrado nesta conta.</p>
            ) : (
              <ul className="snapshots-recovery">
                {recoverable.map((snapshot) => (
                  <li key={snapshot.driveId}>
                    <div>
                      <strong>
                        {showDate(snapshot.created)} · {bytes(snapshot.size)}
                      </strong>
                      <p>
                        {snapshot.machine} · {engineName} {snapshot.version}
                      </p>
                      <code title={snapshot.databaseId}>
                        {snapshot.databaseId}
                      </code>
                    </div>
                    <button
                      className="button"
                      disabled={!online || busy}
                      onClick={() => setChoice({ snapshot })}
                    >
                      Selecionar
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </Modal>
      )}
      {choice && (
        <Modal
          title="Confirmar restauração"
          onClose={() => setChoice(undefined)}
          closeDisabled={!!action}
        >
          <div className="snapshots-dialog">
            <dl className="snapshots-restore-summary">
              <dt>Backup</dt>
              <dd>{choice.file ? choice.file.name : choice.snapshot.name}</dd>
              {choice.snapshot && (
                <>
                  <dt>Criado em</dt>
                  <dd>{showDate(choice.snapshot.created)}</dd>
                  <dt>Identificador</dt>
                  <dd>
                    <code>{choice.snapshot.id}</code>
                  </dd>
                </>
              )}
              <dt>Banco de destino</dt>
              <dd>
                {database.name} · {engineName}
              </dd>
            </dl>
            <p className="snapshots-danger">
              A restauração pode substituir objetos e dados existentes. Objetos
              ausentes do backup podem permanecer no destino.
              {database.engine !== "postgresql" &&
                " Se a operação falhar, algumas alterações podem permanecer."}
              {choice.file &&
                " Importe somente arquivos de uma origem confiável."}
            </p>
            <ErrorMessage error={error} />
            <div className="snapshots-dialog-actions">
              <button
                className="button"
                disabled={!!action}
                onClick={() => setChoice(undefined)}
              >
                Cancelar
              </button>
              <button
                className="button danger"
                disabled={busy || !online}
                onClick={() => {
                  startRestore();
                  setRecoverable(undefined);
                }}
              >
                Confirmar restauração
              </button>
            </div>
          </div>
        </Modal>
      )}
    </section>
  );
}

function ScheduleForm({
  value,
  change,
  onSubmit,
  cancel,
  busy,
  error,
}: {
  value: Schedule;
  change: (next: Schedule) => void;
  onSubmit: (event: FormEvent) => void;
  cancel: () => void;
  busy: boolean;
  error: string;
}) {
  function changeCount(count: number) {
    if (!Number.isInteger(count) || count < 1 || count > 1440) return;
    const times = value.times.slice(0, count);
    const existing = new Set(times);
    for (let minute = 0; times.length < count; minute++) {
      const hour = String(Math.floor(minute / 60)).padStart(2, "0");
      const withinHour = String(minute % 60).padStart(2, "0");
      const clock = hour + ":" + withinHour;
      if (!existing.has(clock)) times.push(clock);
    }
    change({ ...value, times });
  }

  return (
    <form className="snapshots-settings" onSubmit={onSubmit}>
      <fieldset className="snapshots-settings-fields" disabled={busy}>
        <div className="snapshots-frequency">
          <label>
            Frequência
            <select
              value={value.frequency}
              onChange={(event) => {
                const frequency = event.target.value as Schedule["frequency"];
                change({
                  ...value,
                  frequency,
                  times: value.times.length ? value.times : ["03:00"],
                });
              }}
            >
              <option value="manual">Somente manual</option>
              <option value="daily">Diariamente</option>
              <option value="weekly">Semanalmente</option>
            </select>
          </label>
          {value.frequency !== "manual" && (
            <label>
              Snapshots por dia
              <input
                type="number"
                min={1}
                max={1440}
                value={value.times.length}
                onChange={(event) => changeCount(event.target.valueAsNumber)}
                required
              />
            </label>
          )}
        </div>
        {value.frequency === "weekly" && (
          <fieldset>
            <legend>Dias da semana</legend>
            <div className="snapshots-weekdays">
              {weekdays.map((day, index) => (
                <label key={day}>
                  <input
                    type="checkbox"
                    checked={value.days.includes(index)}
                    onChange={(event) =>
                      change({
                        ...value,
                        days: event.target.checked
                          ? [...value.days, index]
                          : value.days.filter((v) => v !== index),
                      })
                    }
                  />
                  {day}
                </label>
              ))}
            </div>
          </fieldset>
        )}
        {value.frequency !== "manual" && (
          <fieldset>
            <legend>
              Horários <span>Fuso do computador</span>
            </legend>
            <div className="snapshots-times">
              {value.times.map((clock, index) => (
                <input
                  key={index}
                  type="time"
                  required
                  value={clock}
                  aria-label={"Horário " + (index + 1)}
                  onChange={(event) => {
                    const times = [...value.times];
                    times[index] = event.target.value;
                    change({ ...value, times });
                  }}
                />
              ))}
            </div>
          </fieldset>
        )}
        <label className="snapshots-retention">
          <span>
            Versões a manter
            <small>De 1 a 50 snapshots</small>
          </span>
          <input
            type="number"
            min={1}
            max={50}
            step={1}
            value={value.limit || ""}
            onChange={(event) =>
              change({ ...value, limit: event.target.valueAsNumber })
            }
            required
          />
        </label>
      </fieldset>
      <ErrorMessage error={error} />
      <div className="form-footer">
        <button
          type="button"
          className="button"
          disabled={busy}
          onClick={cancel}
        >
          Cancelar
        </button>
        <button type="submit" className="button primary" disabled={busy}>
          {busy && <Spinner />}
          {busy ? "Salvando configurações…" : "Salvar configurações"}
        </button>
      </div>
    </form>
  );
}
