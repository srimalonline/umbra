/**
 * The umbra wordmark. Shown by `umbra init`, `umbra --version`, and the
 * installer. Kept tasteful and small — this is a server tool, not a toy.
 */
export const TAGLINE = "headless platform · no ui, ever";

export const BANNER = String.raw`
 _   _ _ __ ___ | |__  _ __ __ _
| | | | '_ \` _ \| '_ \| '__/ _\` |
| |_| | | | | | | |_) | | | (_| |
 \__,_|_| |_| |_|_.__/|_|  \__,_|
        ${TAGLINE}
`;

/** Banner plus a one-line version stamp. */
export function banner(version: string): string {
  return `${BANNER}\n            v${version}\n`;
}
