package source

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/nice-pink/itakeit-messenger/pkg/messenger"
)

// Stdin handles one message per line in order, so `tail -f notes.txt | itakeit-messenger`
// and quick tests work. It returns at end of input.
type Stdin struct{ R io.Reader }

func (s *Stdin) Name() string { return "stdin" }

func (s *Stdin) Run(ctx context.Context, sink messenger.Sink) error {
	r := bufio.NewReaderSize(s.R, 4096)
	for n := 1; ; n++ {
		line, err := readLine(r)
		if t := strings.TrimSpace(line); t != "" {
			m := messenger.Message{Source: "stdin", ID: fmt.Sprintf("%d:%s", n, t), Text: t, Origin: "stdin"}
			if _, err := sink.Handle(ctx, m); err != nil {
				slog.Warn("stdin message failed", "line", n, "err", err)
			}
		}
		if err != nil || ctx.Err() != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return err
		}
	}
}

// readLine returns the next line cut to maxBody bytes. The rest of a longer line
// is skipped, so one huge line cannot end the source or the process.
func readLine(r *bufio.Reader) (string, error) {
	var b []byte
	for {
		part, isPrefix, err := r.ReadLine()
		if room := maxBody - len(b); room > 0 {
			b = append(b, part[:min(len(part), room)]...)
		}
		if err != nil || !isPrefix {
			return string(b), err
		}
	}
}
