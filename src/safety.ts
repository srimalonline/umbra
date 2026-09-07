/**
 * Safety rails, enforced in code rather than merely documented — the design
 * DNA carried over from vm-mcp:
 *   - name sanitization before any value reaches a shell / a path
 *   - output caps so a runaway `docker logs` can't flood the agent
 *   - a confirm-gate primitive for destructive operations
 *
 * umbra never stores secrets in the repo, so there is no secret registry here;
 * the proxy's ACME account key and Caddy's data live under ~/.umbra, outside git.
 */

export const OUTPUT_CAP_BYTES = 10 * 1024;

/** Truncate text to `cap` bytes (UTF-8), appending a truncation notice. */
export function capOutput(text: string, cap: number = OUTPUT_CAP_BYTES): string {
  const buf = Buffer.from(text, "utf8");
  if (buf.length <= cap) return text;
  return buf.subarray(0, cap).toString("utf8") + `\n… [output truncated at ${cap} bytes]`;
}

/** Clamp a requested log-line count into a sane bound. */
export function clampLines(lines: number | undefined, def = 200, max = 1000): number {
  if (!lines || !Number.isFinite(lines) || lines < 1) return def;
  return Math.min(Math.floor(lines), max);
}

/**
 * Sanitize an app name to the umbra character set: lowercase, digits and
 * dashes only. This is the single choke point — every path, compose project
 * name, network alias and Caddy route derives from the sanitized value, so a
 * name can never break out of ~/.umbra/apps or inject into a shell.
 *
 * Throws (rather than silently mangling) when nothing usable remains, so the
 * caller gets a clear error instead of an app called "".
 */
export function sanitizeName(raw: string): string {
  const name = String(raw)
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9-]+/g, "-") // collapse runs of illegal chars to a dash
    .replace(/^-+|-+$/g, "") // trim leading/trailing dashes
    .replace(/-{2,}/g, "-"); // collapse repeated dashes
  if (!name) {
    throw new Error(
      `Unusable app name "${raw}": names must contain at least one letter or digit ([a-z0-9-]).`
    );
  }
  if (name.length > 63) {
    throw new Error(`App name "${name}" too long: max 63 chars (DNS label limit).`);
  }
  return name;
}

/**
 * Validate a domain (hostname) before it reaches the Caddyfile. Rejects
 * anything that could inject a second directive or a shell metacharacter.
 */
export function validateDomain(domain: string): string {
  const d = String(domain).trim().toLowerCase();
  if (!/^(?=.{1,253}$)([a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(\.[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)+$/.test(d)) {
    throw new Error(`Invalid domain "${domain}": expected a fully-qualified hostname like app.example.com.`);
  }
  return d;
}

/** Validate a TCP port. */
export function validatePort(port: number): number {
  if (!Number.isInteger(port) || port < 1 || port > 65535) {
    throw new Error(`Invalid port ${port}: expected an integer 1–65535.`);
  }
  return port;
}

/** A tool result that asks the caller to retry with confirm: true. */
export interface ConfirmPreview {
  confirmRequired: true;
  action: string;
  preview: string;
  hint: string;
}

export function confirmPreview(action: string, preview: string): ConfirmPreview {
  return {
    confirmRequired: true,
    action,
    preview,
    hint: "Nothing was changed. Re-run with confirm: true to proceed.",
  };
}
