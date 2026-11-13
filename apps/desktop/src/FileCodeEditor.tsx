import { useEffect, useRef } from "react";
import { EditorView, basicSetup } from "codemirror";
import { keymap } from "@codemirror/view";
import { python } from "@codemirror/lang-python";
import { html } from "@codemirror/lang-html";
import { css } from "@codemirror/lang-css";
import { javascript } from "@codemirror/lang-javascript";
import { json } from "@codemirror/lang-json";
import { markdown } from "@codemirror/lang-markdown";
import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { tags } from "@lezer/highlight";

const codeHighlight = HighlightStyle.define([
  { tag: tags.keyword, color: "#d987df" },
  { tag: [tags.moduleKeyword, tags.definitionKeyword], color: "#d987df" },
  { tag: [tags.variableName, tags.propertyName, tags.name], color: "#82cfff" },
  { tag: [tags.typeName, tags.className, tags.namespace], color: "#82cfff" },
  {
    tag: [tags.function(tags.variableName), tags.function(tags.propertyName)],
    color: "#82cfff",
  },
  { tag: [tags.string, tags.special(tags.string)], color: "#aad98a" },
  { tag: [tags.number, tags.bool, tags.null], color: "#f0bb83" },
  { tag: tags.comment, color: "#7f8d9b", fontStyle: "italic" },
  { tag: [tags.operator, tags.punctuation], color: "#b8c9d8" },
]);

const codeTheme = EditorView.theme(
  {
    "&": { height: "100%", backgroundColor: "#050608", color: "#dce7ef" },
    ".cm-scroller": {
      overflow: "auto",
      fontFamily: '"IBM Plex Mono", monospace',
      fontSize: "13px",
      lineHeight: "1.75",
    },
    ".cm-content": { padding: "8px 0", caretColor: "#82cfff" },
    ".cm-gutters": {
      backgroundColor: "#050608",
      color: "#8798aa",
      borderRight: "none",
    },
    ".cm-lineNumbers .cm-gutterElement": { padding: "0 12px 0 10px" },
    ".cm-activeLine": { backgroundColor: "#151719" },
    ".cm-activeLineGutter": { backgroundColor: "#050608", color: "#dce7ef" },
    ".cm-selectionBackground": { backgroundColor: "#25445b" },
    "&.cm-focused .cm-selectionBackground": { backgroundColor: "#25445b" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "#82cfff" },
    ".cm-searchMatch": { backgroundColor: "#534524" },
    ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: "#756033" },
  },
  { dark: true },
);

function extension(name: string) {
  return name.split(".").pop()?.toLowerCase() ?? "";
}
function language(name: string) {
  switch (extension(name)) {
    case "py":
    case "pyw":
      return python();
    case "html":
    case "htm":
    case "xml":
    case "svg":
      return html();
    case "css":
    case "scss":
      return css();
    case "js":
    case "jsx":
    case "mjs":
    case "cjs":
      return javascript({ jsx: true });
    case "ts":
    case "tsx":
      return javascript({ typescript: true, jsx: name.endsWith("x") });
    case "json":
    case "jsonc":
      return json();
    case "md":
    case "markdown":
      return markdown();
    default:
      return [];
  }
}

export default function FileCodeEditor({
  name,
  initial,
  onChange,
  onSave,
}: {
  name: string;
  initial: string;
  onChange: (value: string) => void;
  onSave: () => void;
}) {
  const host = useRef<HTMLDivElement>(null);
  const save = useRef(onSave);
  const change = useRef(onChange);
  save.current = onSave;
  change.current = onChange;
  useEffect(() => {
    if (!host.current) return;
    const lineEnding = initial.includes("\r\n") ? "\r\n" : "\n";
    const view = new EditorView({
      doc: initial,
      extensions: [
        basicSetup,
        codeTheme,
        syntaxHighlighting(codeHighlight),
        language(name),
        EditorView.lineWrapping,
        EditorView.updateListener.of((update) => {
          if (update.docChanged)
            change.current(
              update.state.doc.toString().replace(/\r?\n/g, lineEnding),
            );
        }),
        keymap.of([
          {
            key: "Mod-s",
            run: () => {
              save.current();
              return true;
            },
          },
        ]),
      ],
      parent: host.current,
    });
    return () => view.destroy();
  }, [name]);
  return (
    <div
      className="project-file-code"
      ref={host}
      aria-label={`Editor de ${name}`}
    />
  );
}
