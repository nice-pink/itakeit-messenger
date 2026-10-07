package classify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatAsk(t *testing.T) {
	var gotAuth, gotBody, reply string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotAuth, gotBody = r.Header.Get("Authorization"), string(b)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	defer srv.Close()
	var dest struct {
		Task bool `json:"task"`
	}
	ask := chatAsk("openai", srv.URL, "m1", "k1")
	reply = `{"choices":[{"finish_reason":"stop","message":{"content":"{\"task\":true}"}}]}`
	if err := ask(context.Background(), "sys", "usr", &dest); err != nil || !dest.Task {
		t.Fatalf("got %+v, %v", dest, err)
	}
	if gotAuth != "Bearer k1" || !strings.Contains(gotBody, `"model":"m1"`) || !strings.Contains(gotBody, `"strict":true`) {
		t.Fatalf("request: %s %s", gotAuth, gotBody)
	}
	for body, want := range map[string]string{
		`{"choices":[{"finish_reason":"stop","message":{"refusal":"no"}}]}`:   "model refused",
		`{"choices":[{"finish_reason":"length","message":{"content":"{"}}]}`:  "cut off",
		`{"choices":[{"finish_reason":"stop","message":{"content":"nope"}}]}`: "parse answer",
		`{"choices":[]}`: "openai: unexpected answer",
	} {
		reply = body
		if err := ask(context.Background(), "s", "u", &dest); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: got %v, want %q", body, err, want)
		}
	}
	reply = `{"choices":[{"finish_reason":"stop","message":{"content":"<think>\nhm {\"task\":false}\n</think>\n\n{\"task\":true}"}}]}`
	dest.Task = false
	if err := ask(context.Background(), "s", "u", &dest); err != nil || !dest.Task {
		t.Fatalf("think block: %+v, %v", dest, err)
	}
	var sawAuth bool
	open := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawAuth = r.Header["Authorization"]
		io.WriteString(w, reply)
	}))
	defer open.Close()
	if err := NewOpenAI(open.URL+"/v1/", "m", "")(context.Background(), "s", "u", &dest); err != nil || sawAuth {
		t.Fatalf("keyless: auth sent %v, err %v", sawAuth, err)
	}
	status, reply = http.StatusUnauthorized, "bad key"
	if err := ask(context.Background(), "s", "u", &dest); err == nil || !strings.Contains(err.Error(), "openai: 401") {
		t.Fatalf("status: got %v", err)
	}
}
