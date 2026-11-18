import { useSyncExternalStore } from "react";
import errorSources from "./locale-error-templates.json";

export const supportedLanguages = [
  { locale: "en-US", flag: "us", name: "English" },
  { locale: "pt-BR", flag: "br", name: "Português" },
  { locale: "es", flag: "es", name: "Español" },
  { locale: "de", flag: "de", name: "Deutsch" },
  { locale: "fr", flag: "fr", name: "Français" },
  { locale: "it", flag: "it", name: "Italiano" },
  { locale: "zh-CN", flag: "cn", name: "简体中文" },
  { locale: "ja", flag: "jp", name: "日本語" },
] as const;

export type Locale = (typeof supportedLanguages)[number]["locale"];
export type FlagCode = (typeof supportedLanguages)[number]["flag"];

declare global {
  interface Window {
    cloudforgePlatform?: { systemLanguages: readonly string[] };
  }
}

const sourceLocale: Locale = "pt-BR";
const fallbackLocale: Locale = "en-US";
const preferenceKey = "cloudforge.locale";
const catalogs = import.meta.glob<Record<string, string>>("./locales/*.json", {
  eager: true,
  import: "default",
});
const errorTemplates = errorSources.map((source) => {
  const parts = source.split(/\{\d+\}/);
  return {
    source,
    prefix: parts[0].toLowerCase(),
    pattern: new RegExp(
      "^" +
        parts
          .map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
          .join("([\\s\\S]+?)") +
        "$",
      "i",
    ),
  };
});

function catalog(language: Locale) {
  return catalogs[`./locales/${language}.json`] ?? {};
}

function isLocale(value: unknown): value is Locale {
  return supportedLanguages.some(({ locale }) => locale === value);
}

export function detectLocale(languages: readonly string[]): Locale {
  for (const language of languages) {
    const base = language.toLowerCase().split(/[-_]/, 1)[0];
    const match = supportedLanguages.find(
      ({ locale }) => locale.toLowerCase().split("-", 1)[0] === base,
    );
    if (match) return match.locale;
  }
  return fallbackLocale;
}

function preferredLocale(): Locale {
  try {
    const saved = globalThis.localStorage?.getItem(preferenceKey);
    if (isLocale(saved)) return saved;
  } catch {
    // The language can still be changed when storage is unavailable.
  }
  const system = globalThis.window?.cloudforgePlatform?.systemLanguages;
  return detectLocale(
    system?.length ? system : (globalThis.navigator?.languages ?? []),
  );
}

let currentLocale = preferredLocale();
const subscribers = new Set<() => void>();

export function getLocale() {
  return currentLocale;
}

function applyLocale(locale: Locale) {
  if (typeof document !== "undefined") document.documentElement.lang = locale;
  if (locale === currentLocale) return;
  currentLocale = locale;
  for (const notify of subscribers) notify();
}

export function setLocale(locale: Locale) {
  try {
    globalThis.localStorage?.setItem(preferenceKey, locale);
  } catch {
    // Keep the preference for this window when storage is unavailable.
  }
  applyLocale(locale);
}

function subscribe(notify: () => void) {
  subscribers.add(notify);
  return () => {
    subscribers.delete(notify);
  };
}

export function useLocale() {
  return useSyncExternalStore(subscribe, getLocale, getLocale);
}

if (typeof window !== "undefined") {
  applyLocale(currentLocale);
  window.addEventListener("storage", (event) => {
    if (event.key === preferenceKey || event.key === null)
      applyLocale(preferredLocale());
  });
}

function lookup(source: string) {
  if (currentLocale === sourceLocale) return source;
  const messages = catalog(currentLocale);
  if (Object.hasOwn(messages, source)) return messages[source];
  const fallback = catalog(fallbackLocale);
  return Object.hasOwn(fallback, source) ? fallback[source] : source;
}

export function t(
  source: string,
  ...values: (string | number | null | undefined)[]
) {
  return lookup(source).replace(/\{(\d+)\}/g, (slot, index: string) => {
    const value = values[Number(index)];
    return value == null ? slot : String(value);
  });
}

export function messageParts(source: string) {
  return lookup(source).split(/(\{\d+\})/g);
}

export function plural(count: number, one: string, other: string) {
  const singular =
    currentLocale === sourceLocale
      ? count === 1
      : new Intl.PluralRules(currentLocale).select(count) === "one";
  return t(singular ? one : other, count.toLocaleString(currentLocale));
}

export function formatDate(value: string, options: Intl.DateTimeFormatOptions) {
  const date = new Date(value);
  return Number.isNaN(date.getTime())
    ? value
    : date.toLocaleString(currentLocale, options);
}

/** Localize known Core/Electron messages while preserving diagnostic details. */
export function translateMessage(message: string) {
  if (currentLocale === sourceLocale || !message) return message;
  const exact = lookup(message);
  if (exact !== message) return exact;

  const lower = message.toLowerCase();
  for (const { source, prefix, pattern } of errorTemplates) {
    if (!lower.startsWith(prefix)) continue;
    const match = pattern.exec(message);
    if (match) return t(source, ...match.slice(1));
  }
  return message;
}
