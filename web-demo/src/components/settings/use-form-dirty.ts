import { useMemo } from "react"

/**
 * Dirty-state tracking for inline forms with an explicit Save button: the
 * Save control stays disabled until the form actually differs from where it
 * started, so "nothing changed" can never be submitted and a dirty form is
 * visible at a glance (a disabled primary button = saved state).
 *
 * Compare the form's CURRENT state against a snapshot of the form's OWN
 * initial state — never the raw server response. Several GETs deliberately
 * omit secrets (OIDC client secret, Gotify token, SMTP password), so form
 * state is not shape-identical to server data; the snapshot is whatever the
 * form state was right after load, e.g. `{ ...initial, clientSecret: "" }`.
 *
 * The comparison serializes both sides to JSON, so the fresh object literals
 * every `setForm({ ...form, field })` spread produces still compare equal to
 * the snapshot when nothing actually changed.
 *
 * Forms remounted via the `key={JSON.stringify(query.data)}` pattern (see
 * OIDCSettingsCard) reset for free — the remount re-initializes both the
 * state and its snapshot. Non-remounting forms must reset their state (or
 * re-snapshot) on save success themselves.
 */
export function useFormDirty<T>(current: T, initial: T): boolean {
  const currentJson = JSON.stringify(current)
  const initialJson = JSON.stringify(initial)
  return useMemo(() => currentJson !== initialJson, [currentJson, initialJson])
}
