// Package banner holds the umbra wordmark, shown by `umbra init`,
// `umbra version` and the installer. Kept tasteful and small — this is a
// server tool, not a toy.
package banner

// Tagline is umbra's one-line description.
const Tagline = "AI-native PaaS · your agent is the dashboard"

// art is the wordmark, byte-for-byte the same as the TypeScript original
// (String.raw kept the backslashes before the backticks).
const art = "\n" +
	" _   _ _ __ ___ | |__  _ __ __ _\n" +
	"| | | | '_ \\` _ \\| '_ \\| '__/ _\\` |\n" +
	"| |_| | | | | | | |_) | | | (_| |\n" +
	" \\__,_|_| |_| |_|_.__/|_|  \\__,_|\n" +
	"        " + Tagline + "\n"

// Banner returns the wordmark plus a one-line version stamp.
func Banner(version string) string {
	return art + "\n            v" + version + "\n"
}
