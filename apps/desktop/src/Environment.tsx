import { useEffect, useMemo, useState } from "react";
import {
  ChevronDown,
  Download,
  FileText,
  Folder,
  Plus,
  Search,
  Trash2,
  Upload,
} from "lucide-react";
import type { Project } from "../../../packages/shared-types";
import { ErrorMessage, Spinner } from "../../../packages/ui";
import { api, message } from "./api";
import { EnvironmentCopy, EnvironmentEye } from "./EnvironmentActions";
import "./environment.css";

type Variable = { key: string; value: string };
type EnvironmentData = {
  mode: "empty" | "file" | "exclusive";
  files: string[];
  file: string;
  variables: Variable[];
};
type FileEntry = { name: string; path: string; directory: boolean };
const validKey = /^[A-Za-z_][A-Za-z0-9_.-]*$/;
const hiddenValueMask = "•".repeat(15);

function quote(value: string) {
  return value === "" || /[ #\t"'\\]/.test(value)
    ? `"${value.replaceAll("\\", "\\\\").replaceAll('"', '\\"')}"`
    : value;
}

export function EnvironmentPage({ project }: { project: Project }) {
  const [data, setData] = useState<EnvironmentData | null>(null);
  const [variables, setVariables] = useState<Variable[]>([]);
  const [search, setSearch] = useState("");
  const [shown, setShown] = useState<Set<number>>(new Set());
  const [copied, setCopied] = useState<number | null>(null);
  const [importChoice, setImportChoice] = useState<"choose" | "inside" | null>(
    null,
  );
  const [folder, setFolder] = useState("");
  const [entries, setEntries] = useState<FileEntry[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function load(file?: string) {
    setBusy(true);
    setError("");
    try {
      const result = await api<EnvironmentData>(
        "GET",
        `/api/projects/${project.id}/environment${file ? `?file=${encodeURIComponent(file)}` : ""}`,
      );
      setData(result);
      setVariables(result.variables);
      setShown(new Set());
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  useEffect(() => {
    void load();
  }, [project.id]);
  async function list(path: string) {
    setBusy(true);
    setError("");
    try {
      const result = await api<FileEntry[]>(
        "GET",
        `/api/projects/${project.id}/files?path=${encodeURIComponent(path)}`,
      );
      setFolder(path.replaceAll("\\", "/"));
      setEntries(
        result.map((entry) => ({
          ...entry,
          path: entry.path.replaceAll("\\", "/"),
        })),
      );
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  function update(index: number, field: keyof Variable, value: string) {
    setVariables((items) =>
      items.map((item, i) =>
        i === index ? { ...item, [field]: value } : item,
      ),
    );
    setNotice("");
  }
  function startExclusive() {
    setData({ mode: "exclusive", files: [], file: "", variables: [] });
    setVariables([{ key: "", value: "" }]);
    setNotice("");
  }
  async function save(): Promise<boolean> {
    if (!data) return false;
    const names = variables.map((item) => item.key);
    if (names.some((key) => !validKey.test(key))) {
      setError(
        "Use nomes como DB_HOST: letras, números, _, ponto e hífen; comece com letra ou _.",
      );
      return false;
    }
    if (new Set(names).size !== names.length) {
      setError("Há nomes de variáveis repetidos.");
      return false;
    }
    if (variables.some((item) => /[\r\n\0]/.test(item.value))) {
      setError("Os valores não podem conter quebras de linha.");
      return false;
    }
    setBusy(true);
    setError("");
    try {
      await api("PUT", `/api/projects/${project.id}/environment`, {
        mode: data.mode,
        file: data.file,
        variables,
      });
      await load(data.mode === "file" ? data.file : undefined);
      setNotice(
        "Variáveis salvas. Reinicie o projeto para aplicar os novos valores.",
      );
      return true;
    } catch (e) {
      setError(message(e));
      return false;
    } finally {
      setBusy(false);
    }
  }
  async function importOutside() {
    setImportChoice(null);
    setError("");
    try {
      const file = await window.cloudforge.importEnvironment(project.id);
      if (file) {
        await load(file);
        setNotice(
          "Arquivo copiado para o projeto. Reinicie para aplicar os valores.",
        );
      }
    } catch (e) {
      setError(message(e));
    }
  }
  async function importInside(path: string) {
    setBusy(true);
    setError("");
    try {
      const result = await api<{ file: string }>(
        "POST",
        `/api/projects/${project.id}/environment-import`,
        { path },
      );
      setImportChoice(null);
      await load(result.file);
      setNotice(
        "Arquivo selecionado. Reinicie o projeto para aplicar os valores.",
      );
    } catch (e) {
      setError(message(e));
    } finally {
      setBusy(false);
    }
  }
  async function exportExclusive() {
    if (!data || data.mode !== "exclusive") return;
    if (!(await save())) return;
    try {
      const content =
        variables.map((item) => `${item.key}=${quote(item.value)}`).join("\n") +
        "\n";
      if (await window.cloudforge.exportEnvironment(project.id, content))
        setNotice("Arquivo .env exportado.");
    } catch (e) {
      setError(message(e));
    }
  }
  const grouped = useMemo(() => {
    const q = search.trim().toLocaleLowerCase("pt-BR");
    const groups = new Map<string, { index: number; variable: Variable }[]>();
    variables.forEach((variable, index) => {
      if (q && !variable.key.toLocaleLowerCase("pt-BR").includes(q)) return;
      const originalKey = data?.variables[index]?.key;
      const prefix =
        originalKey === undefined
          ? "NOVAS"
          : originalKey.includes("_")
            ? originalKey.split("_")[0].toUpperCase()
            : "OUTRAS";
      if (!groups.has(prefix)) groups.set(prefix, []);
      groups.get(prefix)!.push({ index, variable });
    });
    return [...groups.entries()].sort(([a], [b]) =>
      a.localeCompare(b, "pt-BR"),
    );
  }, [data, variables, search]);
  return (
    <section className="env-page" aria-label="Variáveis de ambiente">
      <div className="env-intro">
        <p>
          Chaves e valores disponíveis para o processo quando o projeto inicia.
        </p>
      </div>
      <ErrorMessage error={error} />
      {notice && (
        <div className="notice" role="status">
          {notice}
        </div>
      )}
      {busy && !data ? (
        <div className="env-loading">
          <Spinner /> Carregando variáveis…
        </div>
      ) : (
        data && (
          <div className="env-panel">
            {data.mode === "file" && (
              <div className="env-file-bar">
                <div>
                  <FileText size={17} />
                  <span>Arquivo de ambiente</span>
                </div>
                <label className="env-file-select">
                  <span className="sr-only">Arquivo ativo</span>
                  <select
                    value={data.file}
                    onChange={(e) => void load(e.target.value)}
                  >
                    {data.files.map((file) => (
                      <option key={file} value={file}>
                        {file}
                      </option>
                    ))}
                  </select>
                  <ChevronDown size={15} />
                </label>
              </div>
            )}
            {data.mode === "exclusive" && (
              <div className="env-file-bar">
                <div>
                  <span className="env-exclusive-dot" />{" "}
                  <span>Variáveis exclusivas do Cloudforge</span>
                </div>
                <span className="muted small">Sem arquivo no projeto</span>
              </div>
            )}
            {data.mode === "empty" ? (
              <div className="env-empty">
                <div className="env-empty-icon">
                  <FileText size={26} />
                </div>
                <h2>Nenhum arquivo de ambiente encontrado</h2>
                <p>
                  Importe um arquivo ou crie variáveis exclusivas para este
                  projeto.
                </p>
                <div className="env-empty-actions">
                  <button
                    className="button primary"
                    onClick={() => setImportChoice("choose")}
                  >
                    <Upload size={16} /> Importar arquivo
                  </button>
                  <button className="button" onClick={startExclusive}>
                    <Plus size={16} /> Criar variáveis exclusivas
                  </button>
                </div>
              </div>
            ) : (
              <>
                <div className="env-toolbar">
                  <label className="search-field">
                    <Search size={17} />
                    <input
                      aria-label="Buscar variável por chave"
                      placeholder="Buscar por chave…"
                      value={search}
                      onChange={(e) => setSearch(e.target.value)}
                    />
                  </label>
                  <span className="muted small">
                    {variables.length} variáve
                    {variables.length === 1 ? "l" : "is"}
                  </span>
                </div>
                <div className="env-columns">
                  <span>Chave</span>
                  <span>Valor</span>
                </div>
                <div className="env-groups">
                  {grouped.length ? (
                    grouped.map(([group, items]) => (
                      <div className="env-group" key={group}>
                        <h2>{group}</h2>
                        {items.map(({ index, variable }) => (
                          <div className="env-row" key={index}>
                            <input
                              aria-label={`Nome da variável ${index + 1}`}
                              value={variable.key}
                              onChange={(e) =>
                                update(index, "key", e.target.value)
                              }
                              onPaste={(e) => {
                                const pasted = e.clipboardData.getData("text");
                                const pair = pasted.split(/\r?\n/, 1)[0];
                                const separator = pair.indexOf("=");
                                if (
                                  separator > 0 &&
                                  validKey.test(pair.slice(0, separator).trim())
                                ) {
                                  e.preventDefault();
                                  setVariables((items) =>
                                    items.map((item, i) =>
                                      i === index
                                        ? {
                                            key: pair
                                              .slice(0, separator)
                                              .trim(),
                                            value: pair
                                              .slice(separator + 1)
                                              .trim(),
                                          }
                                        : item,
                                    ),
                                  );
                                }
                              }}
                              spellCheck={false}
                            />
                            <div className="env-value">
                              {!shown.has(index) && (
                                <span
                                  className="env-value-mask"
                                  aria-hidden="true"
                                >
                                  {hiddenValueMask}
                                </span>
                              )}
                              <input
                                aria-label={`Valor de ${variable.key || `variável ${index + 1}`}`}
                                type={shown.has(index) ? "text" : "password"}
                                className={
                                  shown.has(index) ? "" : "env-value-hidden"
                                }
                                value={variable.value}
                                onChange={(e) =>
                                  update(index, "value", e.target.value)
                                }
                                spellCheck={false}
                                autoComplete="off"
                              />
                              <button
                                className="env-icon"
                                title={
                                  shown.has(index)
                                    ? "Ocultar valor"
                                    : "Exibir valor"
                                }
                                aria-label={
                                  shown.has(index)
                                    ? `Ocultar ${variable.key}`
                                    : `Exibir ${variable.key}`
                                }
                                onClick={() =>
                                  setShown((old) => {
                                    const next = new Set(old);
                                    if (next.has(index)) next.delete(index);
                                    else next.add(index);
                                    return next;
                                  })
                                }
                              >
                                <EnvironmentEye visible={shown.has(index)} />
                              </button>
                              <button
                                className="env-icon"
                                title="Copiar valor"
                                aria-label={`Copiar valor de ${variable.key}`}
                                onClick={async () => {
                                  await window.cloudforge.copy(variable.value);
                                  setCopied(index);
                                  window.setTimeout(
                                    () => setCopied(null),
                                    1800,
                                  );
                                }}
                              >
                                <EnvironmentCopy done={copied === index} />
                              </button>
                            </div>
                            <button
                              className="env-icon env-remove"
                              title="Remover variável"
                              aria-label={`Remover ${variable.key}`}
                              onClick={() =>
                                setVariables((items) =>
                                  items.filter((_, i) => i !== index),
                                )
                              }
                            >
                              <Trash2 size={16} />
                            </button>
                          </div>
                        ))}
                      </div>
                    ))
                  ) : (
                    <p className="env-no-results">
                      {search
                        ? "Nenhuma variável corresponde à busca."
                        : "Nenhuma variável neste projeto. Adicione a primeira abaixo."}
                    </p>
                  )}
                </div>
                <div className="env-add">
                  <button
                    className="button"
                    onClick={() => {
                      setVariables((items) => [
                        ...items,
                        { key: "", value: "" },
                      ]);
                      setSearch("");
                    }}
                  >
                    <Plus size={16} /> Adicionar nova variável
                  </button>
                  <span>
                    Cole CHAVE=valor no campo de chave para preencher os dois
                    campos.
                  </span>
                </div>
                <div className="env-footer">
                  <div>
                    {data.mode === "exclusive" && (
                      <button
                        className="button"
                        disabled={busy}
                        onClick={() => void exportExclusive()}
                      >
                        <Download size={16} /> Exportar .env
                      </button>
                    )}
                  </div>
                  <button
                    className="button primary"
                    disabled={busy}
                    onClick={() => void save()}
                  >
                    {busy ? "Salvando…" : "Salvar"}
                  </button>
                </div>
              </>
            )}
          </div>
        )
      )}
      {importChoice && (
        <div
          className="env-dialog-backdrop"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setImportChoice(null);
          }}
        >
          <div
            className="env-dialog"
            role="dialog"
            aria-modal="true"
            aria-labelledby="env-dialog-title"
          >
            <h2 id="env-dialog-title">Onde está o arquivo?</h2>
            {importChoice === "choose" ? (
              <>
                <p>Escolha a origem do arquivo de variáveis.</p>
                <div className="env-dialog-actions">
                  <button
                    className="button"
                    onClick={() => {
                      setImportChoice("inside");
                      void list("");
                    }}
                  >
                    <Folder size={16} /> Dentro do projeto
                  </button>
                  <button
                    className="button primary"
                    onClick={() => void importOutside()}
                  >
                    <Upload size={16} /> No computador
                  </button>
                </div>
              </>
            ) : (
              <>
                <p>
                  Escolha um arquivo dentro do projeto. Outros nomes serão
                  copiados como .env.
                </p>
                <div className="env-browser-path">
                  <button className="text-button" onClick={() => void list("")}>
                    {project.name}
                  </button>
                  {folder && (
                    <>
                      <span>/ {folder}</span>
                      <button
                        className="text-button"
                        onClick={() =>
                          void list(folder.split("/").slice(0, -1).join("/"))
                        }
                      >
                        Voltar
                      </button>
                    </>
                  )}
                </div>
                <div className="env-browser-list">
                  {entries.map((entry) => (
                    <button
                      key={entry.path}
                      onClick={() =>
                        entry.directory
                          ? void list(entry.path)
                          : void importInside(entry.path)
                      }
                    >
                      {entry.directory ? (
                        <Folder size={16} />
                      ) : (
                        <FileText size={16} />
                      )}
                      {entry.name}
                    </button>
                  ))}
                </div>
              </>
            )}
            <button
              className="button env-dialog-close"
              onClick={() => setImportChoice(null)}
            >
              Cancelar
            </button>
          </div>
        </div>
      )}
    </section>
  );
}
