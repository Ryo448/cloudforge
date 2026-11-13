import {
  lazy,
  Suspense,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  ChevronDown,
  ChevronRight,
  FileAudio,
  FileCode2,
  FileImage,
  FileText,
  FileVideo,
  Folder,
  FolderOpen,
  RefreshCw,
  Save,
  X,
} from "lucide-react";
import type { FileEntry, Project } from "../../../packages/shared-types";
import { api, message } from "./api";
import "./project-files.css";

const FileCodeEditor = lazy(() => import("./FileCodeEditor"));

type OpenFile =
  | { kind: "text"; entry: FileEntry; original: string; version: string }
  | {
      kind: "media";
      entry: FileEntry;
      url: string;
      type: "image" | "video" | "audio";
    };

const mediaExtensions: Record<"image" | "video" | "audio", string[]> = {
  image: [
    "png",
    "jpg",
    "jpeg",
    "gif",
    "webp",
    "avif",
    "bmp",
    "svg",
    "ico",
    "tif",
    "tiff",
    "apng",
    "heic",
    "heif",
    "jp2",
    "jxl",
  ],
  video: [
    "mp4",
    "m4v",
    "mov",
    "webm",
    "ogv",
    "mkv",
    "avi",
    "wmv",
    "flv",
    "3gp",
    "ts",
  ],
  audio: [
    "mp3",
    "wav",
    "wave",
    "m4a",
    "aac",
    "flac",
    "opus",
    "ogg",
    "oga",
    "aiff",
    "aif",
    "wma",
    "amr",
    "mid",
    "midi",
  ],
};

function extension(name: string) {
  return name.split(".").pop()?.toLowerCase() ?? "";
}
function mediaKind(name: string): "image" | "video" | "audio" | null {
  const ext = extension(name);
  return (
    (Object.keys(mediaExtensions) as Array<keyof typeof mediaExtensions>).find(
      (kind) => mediaExtensions[kind].includes(ext),
    ) ?? null
  );
}
function iconFor(entry: FileEntry) {
  if (entry.directory) return Folder;
  const kind = mediaKind(entry.name);
  if (kind === "image") return FileImage;
  if (kind === "video") return FileVideo;
  if (kind === "audio") return FileAudio;
  return /\.(py|pyw|html?|css|scss|[cm]?jsx?|tsx?|jsonc?|xml)$/i.test(
    entry.name,
  )
    ? FileCode2
    : FileText;
}

export function ProjectFiles({
  project,
  onDirtyChange,
}: {
  project: Project;
  onDirtyChange: (dirty: boolean) => void;
}) {
  const [folders, setFolders] = useState<Record<string, FileEntry[]>>({});
  const [folderErrors, setFolderErrors] = useState<Record<string, string>>({});
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [loadingFolder, setLoadingFolder] = useState<string | null>(null);
  const [selected, setSelected] = useState<OpenFile | null>(null);
  const [draft, setDraft] = useState("");
  const [loadingFile, setLoadingFile] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const selectionSerial = useRef(0);
  const mediaUrl = useRef<string | null>(null);
  const dirty = selected?.kind === "text" && draft !== selected.original;
  useEffect(() => {
    onDirtyChange(Boolean(dirty));
  }, [dirty, onDirtyChange]);
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);
  useEffect(() => {
    if (!dirty) return;
    const preventUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", preventUnload);
    return () => window.removeEventListener("beforeunload", preventUnload);
  }, [dirty]);

  async function loadFolder(path: string) {
    setLoadingFolder(path);
    try {
      const entries = await api<FileEntry[]>(
        "GET",
        `/api/projects/${project.id}/files?path=${encodeURIComponent(path)}`,
      );
      setFolders((current) => ({ ...current, [path]: entries }));
      setFolderErrors((current) => {
        const next = { ...current };
        delete next[path];
        return next;
      });
      setError("");
    } catch (e) {
      const detail = message(e);
      setFolderErrors((current) => ({ ...current, [path]: detail }));
      setError(detail);
    } finally {
      setLoadingFolder(null);
    }
  }
  useEffect(() => {
    setFolders({});
    setFolderErrors({});
    setExpanded({});
    setSelected(null);
    setDraft("");
    void loadFolder("");
    return () => {
      ++selectionSerial.current;
      if (mediaUrl.current) URL.revokeObjectURL(mediaUrl.current);
      mediaUrl.current = null;
    };
  }, [project.id]);

  async function toggleFolder(path: string) {
    if (expanded[path]) {
      setExpanded((current) => ({ ...current, [path]: false }));
      return;
    }
    setExpanded((current) => ({ ...current, [path]: true }));
    if (!folders[path]) await loadFolder(path);
  }
  async function refreshTree() {
    for (const path of [
      "",
      ...Object.keys(expanded).filter((path) => expanded[path]),
    ]) {
      await loadFolder(path);
    }
  }
  function clearMedia() {
    if (mediaUrl.current) URL.revokeObjectURL(mediaUrl.current);
    mediaUrl.current = null;
  }
  function mayLeave() {
    return (
      !saving &&
      (!dirty ||
        window.confirm(
          "Há alterações não salvas. Descartar e abrir outro arquivo?",
        ))
    );
  }
  async function openFile(entry: FileEntry) {
    if (!mayLeave()) return;
    const serial = ++selectionSerial.current;
    clearMedia();
    setSelected(null);
    setDraft("");
    setError("");
    setNotice("");
    setLoadingFile(true);
    try {
      const kind = mediaKind(entry.name);
      if (kind) {
        const result = await window.cloudforge.readMedia(
          project.id,
          entry.path,
        );
        if (serial !== selectionSerial.current) return;
        const url = URL.createObjectURL(
          new Blob([new Uint8Array(result.data)], { type: result.type }),
        );
        mediaUrl.current = url;
        setSelected({ kind: "media", entry, url, type: kind });
      } else {
        const result = await api<{ content: string; version: string }>(
          "GET",
          `/api/projects/${project.id}/file?path=${encodeURIComponent(entry.path)}`,
        );
        if (serial !== selectionSerial.current) return;
        setDraft(result.content);
        setSelected({
          kind: "text",
          entry,
          original: result.content,
          version: result.version,
        });
      }
    } catch (e) {
      if (serial === selectionSerial.current) setError(message(e));
    } finally {
      if (serial === selectionSerial.current) setLoadingFile(false);
    }
  }
  function closeFile() {
    if (!mayLeave()) return;
    ++selectionSerial.current;
    clearMedia();
    setSelected(null);
    setDraft("");
    setError("");
    setNotice("");
    setLoadingFile(false);
  }
  async function saveFile() {
    if (!selected || selected.kind !== "text" || !dirty || saving) return;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      const savedContent = draft;
      const result = await api<{ version: string }>(
        "PUT",
        `/api/projects/${project.id}/file`,
        {
          path: selected.entry.path,
          content: savedContent,
          version: selected.version,
        },
      );
      setSelected({
        ...selected,
        original: savedContent,
        version: result.version,
      });
      setNotice("Arquivo salvo.");
    } catch (e) {
      setError(message(e));
    } finally {
      setSaving(false);
    }
  }
  function renderFolder(path: string, depth: number): ReactNode {
    return (folders[path] ?? []).map((entry) => {
      const Icon = iconFor(entry);
      const open = !!expanded[entry.path];
      const active = selected?.entry.path === entry.path;
      return (
        <div key={entry.path}>
          <button
            type="button"
            className={`project-file-row${active ? " active" : ""}`}
            style={{ paddingLeft: `${12 + depth * 16}px` }}
            onClick={() =>
              entry.directory
                ? void toggleFolder(entry.path)
                : void openFile(entry)
            }
            aria-expanded={entry.directory ? open : undefined}
            title={entry.path}
          >
            {entry.directory ? (
              open ? (
                <ChevronDown size={14} />
              ) : (
                <ChevronRight size={14} />
              )
            ) : (
              <span className="project-file-spacer" />
            )}
            <Icon size={16} aria-hidden="true" />
            <span className="project-file-row-name">{entry.name}</span>
            {active && dirty && (
              <span
                className="project-file-dirty"
                aria-label="Alterações não salvas"
              />
            )}
          </button>
          {entry.directory &&
            open &&
            (loadingFolder === entry.path && !folders[entry.path] ? (
              <div
                className="project-file-folder-status"
                style={{ paddingLeft: `${30 + depth * 16}px` }}
              >
                Carregando…
              </div>
            ) : folderErrors[entry.path] ? (
              <div
                className="project-file-folder-status"
                style={{ paddingLeft: `${30 + depth * 16}px` }}
                role="alert"
              >
                Não foi possível abrir. Feche e tente novamente.
              </div>
            ) : folders[entry.path]?.length ? (
              renderFolder(entry.path, depth + 1)
            ) : (
              <div
                className="project-file-folder-status"
                style={{ paddingLeft: `${30 + depth * 16}px` }}
              >
                Pasta vazia
              </div>
            ))}
        </div>
      );
    });
  }
  return (
    <section className="project-files" aria-label="Arquivos do projeto">
      <div className="project-files-toolbar">
        <div className="project-files-location">
          <FolderOpen size={17} />
          <span>{project.name}</span>
          <span className="muted">/</span>
          <span className="muted">Arquivos</span>
        </div>
        <div className="project-files-actions">
          <button
            type="button"
            className="button"
            onClick={() => void refreshTree()}
            title="Atualizar arquivos"
          >
            <RefreshCw size={15} /> Atualizar
          </button>
          <button
            type="button"
            className="button"
            onClick={() =>
              void window.cloudforge
                .openProject(project.id)
                .catch((e) => setError(message(e)))
            }
          >
            <FolderOpen size={15} /> Abrir no explorador
          </button>
        </div>
      </div>
      <div className="project-files-body">
        <aside className="project-files-tree" aria-label="Árvore de arquivos">
          <div className="project-files-tree-heading">
            ARQUIVOS <span>{folders[""]?.length ?? ""}</span>
          </div>
          <div className="project-files-tree-scroll">
            {loadingFolder === "" && !folders[""] ? (
              <p className="project-file-folder-status">Carregando arquivos…</p>
            ) : folderErrors[""] ? (
              <p className="project-file-folder-status" role="alert">
                Não foi possível carregar a árvore. Use Atualizar para tentar
                novamente.
              </p>
            ) : folders[""]?.length ? (
              renderFolder("", 0)
            ) : (
              <p className="project-file-folder-status">
                Este projeto não contém arquivos.
              </p>
            )}
          </div>
        </aside>
        <div className="project-files-viewer">
          <div className="project-files-viewer-header">
            <div className="project-files-viewer-title">
              {selected ? (
                <>
                  <button
                    className="project-file-close"
                    type="button"
                    onClick={closeFile}
                    aria-label="Fechar arquivo"
                  >
                    <X size={16} />
                  </button>
                  <span title={selected.entry.path}>{selected.entry.name}</span>
                  {dirty && (
                    <span className="project-files-unsaved">Não salvo</span>
                  )}
                </>
              ) : (
                <span>Visualização</span>
              )}
            </div>
            {selected?.kind === "text" && (
              <button
                type="button"
                className="button primary"
                onClick={() => void saveFile()}
                disabled={!dirty || saving}
              >
                <Save size={15} />
                {saving ? "Salvando…" : "Salvar"}
              </button>
            )}
          </div>
          {error && (
            <div className="project-files-message error" role="alert">
              {error}
            </div>
          )}
          {notice && (
            <div className="project-files-message" role="status">
              {notice}
            </div>
          )}
          <div className="project-files-stage">
            {loadingFile ? (
              <div className="project-files-empty">Carregando arquivo…</div>
            ) : !selected ? (
              <div className="project-files-empty">
                <FolderOpen size={34} />
                <h2>Nenhum arquivo aberto</h2>
                <p>Selecione um arquivo na árvore para visualizar ou editar.</p>
              </div>
            ) : selected.kind === "text" ? (
              <Suspense
                fallback={
                  <div className="project-files-empty">Preparando editor…</div>
                }
              >
                <FileCodeEditor
                  key={selected.entry.path}
                  name={selected.entry.name}
                  initial={selected.original}
                  onChange={(value) => {
                    setDraft(value);
                    setNotice("");
                  }}
                  onSave={() => void saveFile()}
                />
              </Suspense>
            ) : selected.type === "image" ? (
              <div className="project-files-media">
                <img
                  src={selected.url}
                  alt={selected.entry.name}
                  onError={() =>
                    setError(
                      "Este formato de imagem não pode ser exibido pelo Electron.",
                    )
                  }
                />
              </div>
            ) : selected.type === "video" ? (
              <div className="project-files-media">
                <video
                  src={selected.url}
                  controls
                  onError={() =>
                    setError(
                      "Este formato de vídeo não pode ser reproduzido pelo Electron.",
                    )
                  }
                />
              </div>
            ) : (
              <div className="project-files-media">
                <FileAudio size={42} />
                <strong>{selected.entry.name}</strong>
                <audio
                  src={selected.url}
                  controls
                  onError={() =>
                    setError(
                      "Este formato de áudio não pode ser reproduzido pelo Electron.",
                    )
                  }
                />
              </div>
            )}
          </div>
          {selected?.kind === "text" && (
            <div className="project-files-status">
              <span>{selected.entry.path}</span>
              <span>
                {extension(selected.entry.name).toUpperCase() || "TEXTO"} ·
                UTF-8 · Ctrl+S para salvar
              </span>
            </div>
          )}
        </div>
      </div>
    </section>
  );
}
