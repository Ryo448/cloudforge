import { formatDate, getLocale, t, translateMessage } from "./i18n";
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
  paused: boolean;
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
  postgresDatabase?: string;
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
  importSnapshot: (
    id: string,
    token: string,
    postgresDatabase?: string,
  ) => Promise<Job>;
  downloadSnapshot: (
    id: string,
    driveId: string,
    name: string,
  ) => Promise<Job | null>;
};

const weekdays = [0, 1, 2, 3, 4, 5, 6];
const showDate = (value: string) =>
  formatDate(value, { dateStyle: "medium", timeStyle: "short" });
const automaticAction = "Atualizando snapshots automáticos";
const errorText = (err: unknown) =>
  err instanceof Error
    ? err.message.replace(/^Error invoking remote method '[^']+': Error: /, "")
    : t("Não foi possível concluir a operação.");

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
  const [statusError, setStatusError] = useState("");
  const [action, setAction] = useState("");
  const [settings, setSettings] = useState<Schedule>();
  const [recoverable, setRecoverable] = useState<DriveSnapshot[]>();
  const [choice, setChoice] = useState<RestoreChoice>();
  const [restoreDatabase, setRestoreDatabase] = useState("");
  const [confirmLimit, setConfirmLimit] = useState(false);
  const running = summary?.job?.status === "running";
  const connected =
    !!summary?.drive.configured &&
    !!summary.drive.account &&
    !summary.drive.error &&
    !summary.drive.pending;
  const busy = !!action || running;
  const online = database.state === "running";
  const postgres = database.engine === "postgresql";
  const postgresInstance =
    postgres &&
    (choice?.file?.name || choice?.snapshot?.name)
      ?.toLowerCase()
      .endsWith(".zip");
  const choosePostgresDatabase = postgres && !postgresInstance;
  const terminalJob = running ? "" : summary?.job?.id;
  const accountKey = summary?.drive.account?.id || "";
  const configuredSchedule = summary?.schedule;
  const automaticEnabled =
    !!configuredSchedule &&
    configuredSchedule.frequency !== "manual" &&
    !configuredSchedule.paused;

  useEffect(() => {
    let active = true;
    let querying = false;
    async function status() {
      if (querying) return;
      querying = true;
      try {
        const data = await request<SnapshotState>("GET", route + "/status");
        if (active) {
          setSummary(data);
          setStatusError("");
        }
      } catch (err) {
        if (active) setStatusError(errorText(err));
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
    if (terminalJob) setRefresh((value) => value + 1);
  }, [terminalJob]);

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
  }, [accountKey, refresh, route, request]);

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
    void run(t("Salvando configurações"), async () => {
      await request("PUT", route + "/schedule", {
        schedule: settings,
        confirm: confirmed,
      });
      setSettings(undefined);
      setConfirmLimit(false);
      setSummary(await request<SnapshotState>("GET", route + "/status"));
    });
  }

  function toggleAutomatic() {
    const enabled = !automaticEnabled;
    void run(automaticAction, async () => {
      await request("PUT", route + "/automatic", { enabled });
      setSummary(await request<SnapshotState>("GET", route + "/status"));
    });
  }

  function chooseRestore(next: RestoreChoice) {
    setRestoreDatabase(next.snapshot?.postgresDatabase || "cloudforge");
    setChoice(next);
  }

  function startRestore() {
    if (!choice) return;
    void run(t("Preparando restauração"), async () => {
      showJob(
        choice.file
          ? await desktop.importSnapshot(
              database.id,
              choice.file.token,
              choosePostgresDatabase ? restoreDatabase : undefined,
            )
          : await request<Job>("POST", route + "/restore", {
              driveId: choice.snapshot.driveId,
              postgresDatabase: choosePostgresDatabase
                ? restoreDatabase
                : undefined,
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
          <h1 id="snapshots-heading">{t("Snapshots do banco de dados")}</h1>
          <p>
            {t(
              "Guarde cópias do {0} no Google Drive e recupere os dados quando precisar.",
              database.name,
            )}
          </p>
        </div>
        <div className="snapshots-actions">
          <button
            type="button"
            className="button"
            disabled={!summary || busy}
            onClick={() => {
              setError("");
              setSettings(configuredSchedule);
            }}
          >
            <Settings2 size={15} /> {t("Configurações")}
          </button>
          <button
            type="button"
            className="button primary"
            disabled={!connected || !online || busy}
            onClick={() =>
              void run(t("Preparando snapshot"), async () =>
                showJob(await request<Job>("POST", route)),
              )
            }
          >
            <Plus size={17} /> {t("Realizar snapshot")}
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
                ? t("Verificando conexão…")
                : summary.drive.account?.email ||
                  (summary.drive.account
                    ? t("Conta conectada")
                    : summary.drive.pending
                      ? t("Aguardando autorização no navegador")
                      : t("Nenhuma conta Google cadastrada."))}
            </p>
          </div>
          {summary && !connected && (
            <button className="button" onClick={onOpenDriveSettings}>
              {t("Abrir configurações do Google Drive")}
            </button>
          )}
        </div>
        {summary?.drive.error && <ErrorMessage error={summary.drive.error} />}
        <div className="snapshots-status-row snapshots-schedule">
          <CalendarClock size={19} aria-hidden="true" />
          <div>
            <strong>
              {!configuredSchedule
                ? t("Verificando agendamento…")
                : !automaticEnabled
                  ? t("Snapshots automáticos desativados")
                  : configuredSchedule.frequency === "daily"
                    ? t("Snapshots automáticos diários")
                    : t("Snapshots automáticos semanais")}
            </strong>
            <p>
              {configuredSchedule &&
                (!automaticEnabled
                  ? t("Você ainda pode realizar snapshots manuais.")
                  : !connected
                    ? t("Aguardando uma conta Google Drive.")
                    : configuredSchedule.next
                      ? t(
                          "Próxima execução {0}",
                          showDate(configuredSchedule.next),
                        )
                      : t(
                          "Os horários de execução seguem o fuso do computador.",
                        ))}
              {configuredSchedule?.last && (
                <span className="snapshots-last">
                  {t("Última execução programada")}{" "}
                  {showDate(configuredSchedule.last)}
                </span>
              )}
            </p>
            {postgres && summary && (
              <p>
                {t(
                  "O snapshot inclui todos os bancos de aplicação da instância.",
                )}
              </p>
            )}
          </div>
          <span className="snapshots-count">
            {connected && !loading && !listError
              ? t(
                  "{0} / {1} snapshots",
                  list.length,
                  summary?.schedule.limit ?? "—",
                )
              : t("Limite de {0} snapshots", summary?.schedule.limit ?? "—")}
          </span>
        </div>
      </div>

      <ErrorMessage error={error || statusError} />
      {(summary?.job || action) && (
        <div
          className={"notice snapshots-job " + (summary?.job?.status || "")}
          role="status"
        >
          {(running || action) && <Spinner />}
          <span>{translateMessage(action || summary?.job?.detail || "")}</span>
          {running && summary?.job?.progress !== undefined && (
            <>
              <progress
                aria-label={t("Progresso da operação")}
                max={100}
                value={summary.job.progress}
              />
              <span>{summary.job.progress}%</span>
            </>
          )}
        </div>
      )}

      <div className="snapshots-list-head">
        <h2>{t("Histórico de snapshots")}</h2>
        <div className="snapshots-actions">
          <button
            className="button"
            disabled={!connected || busy}
            onClick={() =>
              void run(t("Buscando backups no Drive"), async () =>
                setRecoverable(
                  await request<DriveSnapshot[]>("GET", route + "/recoverable"),
                ),
              )
            }
          >
            <Cloud size={15} /> {t("Outros backups do Drive")}
          </button>
          <button
            className="button"
            disabled={!online || busy}
            onClick={() =>
              void run(t("Selecionando arquivo"), async () => {
                const file = await desktop.pickSnapshot(
                  database.id,
                  database.engine,
                );
                if (file) chooseRestore({ file });
              })
            }
          >
            <Upload size={15} /> {t("Importar backup")}
          </button>
          <button
            className="icon-button"
            aria-label={t("Atualizar histórico")}
            title={t("Atualizar histórico")}
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
          <Spinner /> {t("Carregando snapshots…")}
        </div>
      ) : list.length === 0 ? (
        <div className="snapshots-empty">
          <History size={24} aria-hidden="true" />
          <strong>{t("Nenhum snapshot neste banco")}</strong>
          <span>
            {connected
              ? t("Use Realizar snapshot para salvar a primeira versão.")
              : t(
                  "Conecte uma conta Google nas configurações do Cloudforge para criar e consultar snapshots.",
                )}
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
                  <span className="snapshots-recent">{t("Mais recente")}</span>
                )}
                <div className="snapshots-meta">
                  <span>
                    {snapshot.mode === "automatic"
                      ? t("Automático")
                      : t("Manual")}
                  </span>
                  <span>{bytes(snapshot.size)}</span>
                  <span>
                    {engineName} {snapshot.version}
                    {snapshot.postgresDatabase && (
                      <> · {snapshot.postgresDatabase}</>
                    )}
                  </span>
                </div>
                <code title={snapshot.id}>{snapshot.id}</code>
              </div>
              <div className="snapshots-actions">
                <button
                  className="button"
                  disabled={!online || busy}
                  onClick={() => chooseRestore({ snapshot })}
                >
                  <RotateCcw size={15} /> {t("Restaurar")}
                </button>
                <button
                  className="button"
                  disabled={busy}
                  onClick={() =>
                    void run(t("Selecionando destino"), async () =>
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
                  <Download size={15} /> {t("Baixar")}
                </button>
              </div>
            </li>
          ))}
        </ol>
      )}

      {settings && !confirmLimit && (
        <Modal
          title={t("Configurações de snapshots")}
          className="snapshots-settings-modal"
          onClose={() => setSettings(undefined)}
          closeDisabled={!!action}
        >
          <ScheduleForm
            value={settings}
            change={setSettings}
            onSubmit={saveSettings}
            automaticEnabled={
              configuredSchedule?.frequency !== "manual"
                ? automaticEnabled
                : undefined
            }
            onToggleAutomatic={toggleAutomatic}
            toggling={action === automaticAction}
            cancel={() => setSettings(undefined)}
            busy={!!action}
            error={error}
          />
        </Modal>
      )}
      {confirmLimit && settings && (
        <Modal
          title={t("Reduzir o limite de versões")}
          onClose={() => setConfirmLimit(false)}
          closeDisabled={!!action}
        >
          <div className="snapshots-dialog">
            <p>
              {t(
                "Serão mantidos os {0} snapshots mais recentes deste banco. As versões excedentes serão excluídas do Google Drive.",
                settings.limit,
              )}
            </p>
            <ErrorMessage error={error} />
            <div className="snapshots-dialog-actions">
              <button
                className="button"
                onClick={() => setConfirmLimit(false)}
                disabled={!!action}
              >
                {t("Voltar")}
              </button>
              <button
                className="button danger"
                disabled={!!action}
                onClick={() => saveSettings(undefined, true)}
              >
                {t("Aplicar limite")}
              </button>
            </div>
          </div>
        </Modal>
      )}
      {recoverable && !choice && (
        <Modal
          title={t("Outros backups do Google Drive")}
          wide
          onClose={() => setRecoverable(undefined)}
        >
          <div className="snapshots-dialog">
            <p>
              {t(
                "Selecione um snapshot de {0} de outro banco ou instalação para restaurar em {1}.",
                engineName,
                database.name,
              )}
            </p>
            {recoverable.length === 0 ? (
              <p>{t("Nenhum backup compatível encontrado nesta conta.")}</p>
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
                        {snapshot.postgresDatabase && (
                          <> · {snapshot.postgresDatabase}</>
                        )}
                      </p>
                      <code title={snapshot.databaseId}>
                        {snapshot.databaseId}
                      </code>
                    </div>
                    <button
                      className="button"
                      disabled={!online || busy}
                      onClick={() => chooseRestore({ snapshot })}
                    >
                      {t("Selecionar")}
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
          title={t("Confirmar restauração")}
          onClose={() => setChoice(undefined)}
          closeDisabled={!!action}
        >
          <div className="snapshots-dialog">
            <dl className="snapshots-restore-summary">
              <dt>{t("Backup")}</dt>
              <dd>{choice.file ? choice.file.name : choice.snapshot.name}</dd>
              {choice.snapshot && (
                <>
                  <dt>{t("Criado em")}</dt>
                  <dd>{showDate(choice.snapshot.created)}</dd>
                  <dt>{t("Identificador")}</dt>
                  <dd>
                    <code>{choice.snapshot.id}</code>
                  </dd>
                </>
              )}
              <dt>
                {postgres ? t("Instância de destino") : t("Banco de destino")}
              </dt>
              <dd>
                {database.name} · {engineName}
              </dd>
            </dl>
            {choosePostgresDatabase && (
              <label className="snapshots-destination">
                {t("Banco PostgreSQL de destino")}
                <input
                  value={restoreDatabase}
                  onChange={(event) => setRestoreDatabase(event.target.value)}
                  maxLength={63}
                  disabled={busy}
                  required
                />
              </label>
            )}
            <p className="snapshots-danger">
              {postgresInstance
                ? t(
                    "Cada banco incluído no snapshot será recriado com seu nome original. Bancos de mesmo nome serão substituídos. Encerre as conexões com esses bancos antes de restaurar.",
                  )
                : t(
                    "A restauração pode substituir objetos e dados existentes. Objetos ausentes do backup podem permanecer no destino.",
                  )}
              {database.engine !== "postgresql" &&
                t(
                  " Se a operação falhar, algumas alterações podem permanecer.",
                )}
              {choice.file &&
                t(" Importe somente arquivos de uma origem confiável.")}
            </p>
            <ErrorMessage error={error} />
            <div className="snapshots-dialog-actions">
              <button
                className="button"
                disabled={!!action}
                onClick={() => setChoice(undefined)}
              >
                {t("Cancelar")}
              </button>
              <button
                className="button danger"
                disabled={
                  busy ||
                  !online ||
                  (choosePostgresDatabase && !restoreDatabase)
                }
                onClick={() => {
                  startRestore();
                  setRecoverable(undefined);
                }}
              >
                {t("Confirmar restauração")}
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
  automaticEnabled,
  onToggleAutomatic,
  toggling,
  cancel,
  busy,
  error,
}: {
  value: Schedule;
  change: (next: Schedule) => void;
  onSubmit: (event: FormEvent) => void;
  automaticEnabled?: boolean;
  onToggleAutomatic: () => void;
  toggling: boolean;
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
      {automaticEnabled !== undefined && (
        <div className="snapshots-automatic">
          <div>
            <strong>{t("Snapshots automáticos")}</strong>
            <small role="status">
              {automaticEnabled ? t("Ativados") : t("Desativados")}.{" "}
              {t(
                "Seus horários ficam salvos e os snapshots manuais continuam disponíveis.",
              )}
            </small>
          </div>
          <button
            type="button"
            className="button"
            onClick={onToggleAutomatic}
            disabled={busy}
          >
            {toggling && <Spinner />}
            {automaticEnabled
              ? t("Desativar automáticos")
              : t("Reativar automáticos")}
          </button>
        </div>
      )}
      <fieldset className="snapshots-settings-fields" disabled={busy}>
        <div className="snapshots-frequency">
          <label>
            {t("Frequência")}
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
              <option value="manual">{t("Somente manual")}</option>
              <option value="daily">{t("Diariamente")}</option>
              <option value="weekly">{t("Semanalmente")}</option>
            </select>
          </label>
          {value.frequency !== "manual" && (
            <label>
              {t("Snapshots por dia")}
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
            <legend>{t("Dias da semana")}</legend>
            <div className="snapshots-weekdays">
              {weekdays.map((index) => (
                <label
                  key={new Intl.DateTimeFormat(getLocale(), {
                    weekday: "short",
                    timeZone: "UTC",
                  }).format(Date.UTC(2024, 0, index + 7))}
                >
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
                  {new Intl.DateTimeFormat(getLocale(), {
                    weekday: "short",
                    timeZone: "UTC",
                  }).format(Date.UTC(2024, 0, index + 7))}
                </label>
              ))}
            </div>
          </fieldset>
        )}
        {value.frequency !== "manual" && (
          <fieldset>
            <legend>
              {t("Horários")} <span>{t("Fuso do computador")}</span>
            </legend>
            <div className="snapshots-times">
              {value.times.map((clock, index) => (
                <input
                  key={index}
                  type="time"
                  required
                  value={clock}
                  aria-label={t("Horário {0}", index + 1)}
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
            {t("Versões a manter")}
            <small>{t("De 1 a 50 snapshots")}</small>
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
          {t("Cancelar")}
        </button>
        <button type="submit" className="button primary" disabled={busy}>
          {busy && !toggling && <Spinner />}
          {busy && !toggling
            ? t("Salvando configurações…")
            : t("Salvar configurações")}
        </button>
      </div>
    </form>
  );
}
