/** Saves a Blob as a file via a throwaway object-URL link. The revoke is
 * deferred: revoking in the same tick as click() cancels the download in
 * some browsers (Safari, older Firefox) before it has started reading. */
export function downloadBlob(filename: string, blob: Blob): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  setTimeout(() => URL.revokeObjectURL(url), 1000)
}

/** One CSV field: quoted when it contains a delimiter, quote, or newline.
 * A leading = + - @ is prefixed with a quote mark so a spreadsheet opens it
 * as text instead of evaluating it — audit rows carry user-controlled text
 * (usernames, targets), which makes formula injection a real concern here. */
export function csvField(value: unknown): string {
  if (value === null || value === undefined) return ""
  let text = typeof value === "object" ? JSON.stringify(value) : String(value)
  if (/^[=+\-@\t\r]/.test(text)) text = `'${text}`
  return /[",\n\r]/.test(text) ? `"${text.replace(/"/g, '""')}"` : text
}

export function toCsv(header: string[], rows: unknown[][]): string {
  return [header, ...rows].map((row) => row.map(csvField).join(",")).join("\r\n")
}

/** BOM-prefixed so Excel reads the file as UTF-8 rather than the system codepage. */
export function downloadCsv(filename: string, header: string[], rows: unknown[][]): void {
  downloadBlob(filename, new Blob(["﻿", toCsv(header, rows)], { type: "text/csv;charset=utf-8" }))
}
