// Package source holds the input sources.
package source

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/nice-pink/itakeit-messenger/pkg/messenger"
)

// HTTP accepts POST /messages with {"text": "...", "id": "...", "source": "...",
// "author": "...", "url": "..."} and a bearer token, and answers with the
// decision once the message is classified. Only text is required. Without id the
// text's hash is the ID, so the same text posted twice is posted once.
type HTTP struct {
	Listen string
	Token  string
}

const maxBody = 64 << 10

func (h *HTTP) Name() string { return "http" }

func (h *HTTP) Run(ctx context.Context, sink messenger.Sink) error {
	if len(h.Token) < 16 {
		return errors.New("http source: MESSENGER_HTTP_TOKEN must be at least 16 characters")
	}
	srv := &http.Server{Addr: h.Listen, Handler: h.Handler(sink), ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second}
	ln, err := net.Listen("tcp", h.Listen)
	if err != nil {
		return err
	}
	slog.Info("http source listening", "addr", ln.Addr().String())
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(sc)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (h *HTTP) Handler(sink messenger.Sink) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /messages", func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		gotSum, wantSum := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(h.Token))
		if subtle.ConstantTimeCompare(gotSum[:], wantSum[:]) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var in struct {
			Text, ID, Source, Author, URL string
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
		if err := dec.Decode(&in); err != nil || strings.TrimSpace(in.Text) == "" {
			http.Error(w, `body must be JSON with a non-empty "text"`, http.StatusBadRequest)
			return
		}
		name := "http"
		if in.Source != "" {
			name = "http:" + in.Source
		}
		if in.ID == "" {
			in.ID = fmt.Sprintf("%x", sha256.Sum256([]byte(in.Text)))
		}
		m := messenger.Message{Source: name, ID: in.ID, Text: in.Text, Author: in.Author, Origin: messenger.Escape(in.Source)}
		if in.URL != "" {
			m.Link = func() string { return in.URL }
		}
		d, err := sink.Handle(r.Context(), m)
		if errors.Is(err, messenger.ErrInFlight) {
			http.Error(w, "the same message is still being processed: retry later", http.StatusConflict)
			return
		}
		if err != nil {
			slog.Warn("http message failed", "source", name, "err", err)
			http.Error(w, "classification or posting failed", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(d)
	})
	return mux
}
