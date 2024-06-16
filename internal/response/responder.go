package response

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

type Responder struct {
	DebugMode bool
}

// RespondAndLogError will respond with generic error code (500) and log with slog.LevelError level
func (rr *Responder) RespondAndLogError(w http.ResponseWriter, ctx context.Context, err error) {
	errId := uuid.NewString()
	log(ctx, slog.LevelError, err.Error(), slog.String("err_id", errId))
	rr.renderError(w, ctx, http.StatusInternalServerError, err.Error(), errId)
}

func (rr *Responder) RespondAndLogCustom(w http.ResponseWriter, ctx context.Context, err error, lvl slog.Level, status int) {
	errId := uuid.NewString()
	log(ctx, lvl, err.Error(), slog.String("err_id", errId))
	rr.renderError(w, ctx, status, err.Error(), errId)
}

func (rr *Responder) SendJson(w http.ResponseWriter, ctx context.Context, data any) {
	bs, err := json.Marshal(data)
	if err != nil {
		rr.RespondAndLogError(w, ctx, err)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = io.Copy(w, bytes.NewReader(bs))
}

func (rr *Responder) renderError(w http.ResponseWriter, ctx context.Context, status int, message, errId string) {
	data := map[string]any{}

	if rr.DebugMode {
		r, s := utf8.DecodeRuneInString(message)
		data["error"] = string(unicode.ToUpper(r)) + message[s:]
	} else {
		data["error"] = "Unknown error occurred while processing your request. Error ID: " + errId
	}

	bs, err := json.Marshal(data)
	if err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
	} else {
		log(ctx, slog.LevelError, "cannot marshall error response body: "+err.Error())
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		bs = []byte("unknown error")
	}

	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.Copy(w, bytes.NewReader(bs))
}

// Needed because it skips one more frame item than the slog.Log
func log(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	l := slog.Default()

	if !l.Enabled(ctx, level) {
		return
	}

	var pc uintptr
	var pcs [1]uintptr
	// skip [runtime.Callers, this function, this function's caller]
	runtime.Callers(3, pcs[:])
	pc = pcs[0]

	r := slog.NewRecord(time.Now(), level, msg, pc)
	r.AddAttrs(attrs...)
	_ = l.Handler().Handle(ctx, r)
}
