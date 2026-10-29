package aihelpme

import (
	"log/slog"
	"os"
	"strings"
)

// SetupLogging installs the default slog logger for the daemon: JSON records
// on stderr with source locations (where), and the level taken from
// AIHELPME_LOG_LEVEL (debug/info/warn/error, default info). Call it once at
// daemon startup before serving; daemon and caller code log through
// slog.Default, so nothing else needs a logger passed around.
func SetupLogging() {
	var level slog.Level
	switch strings.ToLower(os.Getenv("AIHELPME_LOG_LEVEL")) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level, AddSource: true})
	slog.SetDefault(slog.New(h))
}
