package logger

import (
	"log/slog"
	"os"
)

var L *slog.Logger

func Init() {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})
	L = slog.New(handler)
	slog.SetDefault(L)
}
