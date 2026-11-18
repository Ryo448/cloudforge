import { Fragment, type ReactNode } from "react";
import { messageParts } from "./i18n";

export function Translated({
  text,
  values,
}: {
  text: string;
  values: ReactNode[];
}) {
  return messageParts(text).map((part, index) => {
    const slot = /^\{(\d+)\}$/.exec(part);
    return (
      <Fragment key={index}>{slot ? values[Number(slot[1])] : part}</Fragment>
    );
  });
}
