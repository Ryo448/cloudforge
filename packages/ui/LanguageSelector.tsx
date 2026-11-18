import { Check, ChevronDown } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import {
  setLocale,
  supportedLanguages,
  t,
  useLocale,
  type FlagCode,
} from "./i18n";
import "./language-selector.css";

const star =
  "M0 -1 .2245 -.309 .951 -.309 .363 .118 .588 .809 0 .382 -.588 .809 -.363 .118 -.951 -.309 -.2245 -.309Z";

function Flag({ code }: { code: FlagCode }) {
  const stripes = (
    <path
      stroke="#be2535"
      strokeWidth={1.08}
      d="M0 .54h20m-20 2.15h20m-20 2.16h20m-20 2.15h20m-20 2.15h20m-20 2.16h20m-20 2.15h20"
    />
  );
  const contents = {
    us: (
      <>
        <path fill="#fff" d="M0 0h20v14H0z" />
        {stripes}
        <path fill="#263a73" d="M0 0h8.6v7.54H0z" />
        {Array.from({ length: 9 }, (_, row) =>
          Array.from({ length: row % 2 ? 5 : 6 }, (_, column) => (
            <circle
              key={`${row}-${column}`}
              cx={0.78 + column * 1.4 + (row % 2 ? 0.7 : 0)}
              cy={0.68 + row * 0.77}
              r={0.24}
              fill="#fff"
            />
          )),
        )}
      </>
    ),
    br: (
      <>
        <path fill="#039647" d="M0 0h20v14H0z" />
        <path fill="#ffdb37" d="M10 1.8 18.3 7 10 12.2 1.7 7Z" />
        <circle cx="10" cy="7" r="3.4" fill="#17388c" />
        <path
          fill="#fff"
          d="M6.7 6.35a3.4 3.4 0 0 1 .39-1.01 10 10 0 0 1 6.3 2.12 3.4 3.4 0 0 1-.28 1.01 9 9 0 0 0-6.41-2.12Z"
        />
        <path
          fill="#fff"
          d="M8.4 7.64h.38v.39h-.38zm1.6.96h.35V9H10zm1.1-1h.3v.3h-.3zm.64 1.41h.3v.3h-.3z"
        />
      </>
    ),
    es: (
      <>
        <path fill="#bd2333" d="M0 0h20v14H0z" />
        <path fill="#ffca39" d="M0 3.5h20v7H0z" />
        <path
          fill="#bd2333"
          d="M5 5.7h3.25v2.2L7.4 9H5.85L5 7.9zm.45-.57.5-.55h1.35l.5.55v.3H5.45z"
        />
        <path fill="#fff0c9" d="M5.45 6.2h.8v.85h-.8zm1.55 1.2h.8v.85H7z" />
        <path fill="#fff0c9" d="M4.25 6.15h.4v2.45h-.4zm4.4 0h.4v2.45h-.4z" />
      </>
    ),
    de: (
      <>
        <path fill="#151515" d="M0 0h20v4.67H0z" />
        <path fill="#dc222c" d="M0 4.67h20v4.66H0z" />
        <path fill="#ffce38" d="M0 9.33h20V14H0z" />
      </>
    ),
    fr: (
      <>
        <path fill="#143e87" d="M0 0h6.67v14H0z" />
        <path fill="#fff" d="M6.67 0h6.66v14H6.67z" />
        <path fill="#e83e46" d="M13.33 0H20v14h-6.67z" />
      </>
    ),
    it: (
      <>
        <path fill="#079367" d="M0 0h6.67v14H0z" />
        <path fill="#fff" d="M6.67 0h6.66v14H6.67z" />
        <path fill="#cd343e" d="M13.33 0H20v14h-6.67z" />
      </>
    ),
    cn: (
      <>
        <path fill="#e33125" d="M0 0h20v14H0z" />
        <g fill="#ffde3d">
          <path d={star} transform="translate(4 4.3) scale(2.2)" />
          <path
            d={star}
            transform="translate(7.45 2.04) rotate(15) scale(.65)"
          />
          <path
            d={star}
            transform="translate(8.85 3.52) rotate(35) scale(.65)"
          />
          <path
            d={star}
            transform="translate(8.85 5.44) rotate(15) scale(.65)"
          />
          <path
            d={star}
            transform="translate(7.45 6.85) rotate(35) scale(.65)"
          />
        </g>
      </>
    ),
    jp: (
      <>
        <path fill="#fff" d="M0 0h20v14H0z" />
        <circle fill="#bc1944" cx="10" cy="7" r="4.1" />
      </>
    ),
  };
  return (
    <svg className="language-flag" viewBox="0 0 20 14" aria-hidden="true">
      {contents[code]}
    </svg>
  );
}

export function LanguageSelector() {
  const locale = useLocale();
  const current = supportedLanguages.find(
    (language) => language.locale === locale,
  )!;
  const id = useId();
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const label = t("Selecionar idioma");

  useEffect(() => {
    if (!open) return;
    menu.current
      ?.querySelector<HTMLButtonElement>('[aria-checked="true"]')
      ?.focus();
    function onOutside(event: PointerEvent) {
      if (!root.current?.contains(event.target as Node)) setOpen(false);
    }
    document.addEventListener("pointerdown", onOutside);
    return () => document.removeEventListener("pointerdown", onOutside);
  }, [open]);

  function closeAndFocusTrigger() {
    setOpen(false);
    trigger.current?.focus();
  }

  return (
    <div className="language-selector" ref={root}>
      <button
        ref={trigger}
        className="language-trigger"
        type="button"
        aria-label={`${label} · ${current.name}`}
        title={label}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => setOpen(!open)}
        onKeyDown={(event) => {
          if (event.key === "ArrowDown" || event.key === "ArrowUp") {
            event.preventDefault();
            setOpen(true);
          }
        }}
      >
        <Flag code={current.flag} />
        <ChevronDown size={13} aria-hidden="true" />
      </button>
      {open && (
        <div
          className="language-menu"
          id={id}
          ref={menu}
          role="menu"
          aria-label={label}
          onBlur={(event) => {
            if (
              event.relatedTarget &&
              !root.current?.contains(event.relatedTarget)
            )
              setOpen(false);
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.preventDefault();
              closeAndFocusTrigger();
              return;
            }
            if (event.key === "Tab") {
              closeAndFocusTrigger();
              return;
            }
            const items = Array.from(
              event.currentTarget.querySelectorAll<HTMLButtonElement>("button"),
            );
            const index = items.indexOf(
              document.activeElement as HTMLButtonElement,
            );
            const next = {
              ArrowDown: (index + 1) % items.length,
              ArrowUp: (index - 1 + items.length) % items.length,
              Home: 0,
              End: items.length - 1,
            }[event.key];
            if (next !== undefined) {
              event.preventDefault();
              items[next]?.focus();
            }
          }}
        >
          <div className="language-menu-label" aria-hidden="true">
            {label}
          </div>
          {supportedLanguages.map((language) => (
            <button
              key={language.locale}
              type="button"
              role="menuitemradio"
              tabIndex={-1}
              aria-checked={language.locale === locale}
              onClick={() => {
                setLocale(language.locale);
                closeAndFocusTrigger();
              }}
            >
              <Flag code={language.flag} />
              <span lang={language.locale}>{language.name}</span>
              {language.locale === locale && (
                <Check size={15} aria-hidden="true" />
              )}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
