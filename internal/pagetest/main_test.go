package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestProtocolAndEOF(t *testing.T) {
	input, commands := io.Pipe()
	responses, stdout := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(input, stdout)
		_ = stdout.Close()
	}()
	defer commands.Close()
	defer responses.Close()
	encoder := json.NewEncoder(commands)
	decoder := json.NewDecoder(responses)
	request := func(value any) map[string]any {
		t.Helper()
		if err := encoder.Encode(value); err != nil {
			t.Fatal(err)
		}
		var response map[string]any
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	ready := request(map[string]any{"id": 1, "op": "start", "render": map[string]any{"html": "<p>初回</p>", "rev": 1}})
	if ready["type"] != "ready" || ready["id"] != float64(1) {
		t.Fatal(ready)
	}
	url, ok := ready["url"].(string)
	if !ok || !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.HasSuffix(url, "/"+ready["token"].(string)+"/") {
		t.Fatal(ready)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	page, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if err != nil || page.StatusCode != 200 || !strings.Contains(string(body), "gp-content") {
		t.Fatalf("ページの応答: status=%d, error=%v", page.StatusCode, err)
	}
	events, err := client.Get(url + "_/events")
	if err != nil {
		t.Fatal(err)
	}
	defer events.Body.Close()
	if events.StatusCode != 200 || !strings.HasPrefix(events.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatal(events.Status, events.Header)
	}
	reader := bufio.NewScanner(events.Body)
	next := func(name string) map[string]any {
		t.Helper()
		for reader.Scan() {
			if reader.Text() != "event: "+name {
				continue
			}
			if !reader.Scan() {
				break
			}
			var data map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(reader.Text(), "data: ")), &data); err != nil {
				t.Fatal(err)
			}
			return data
		}
		t.Fatalf("SSE の %s が届きません: %v", name, reader.Err())
		return nil
	}
	if data := next("render"); data["html"] != "<p>初回</p>" {
		t.Fatal(data)
	}
	response := request(map[string]any{"id": 2, "op": "broadcast", "name": "render", "data": map[string]any{"html": "<p>更新</p>", "rev": 2, "extra": "<>&日本語"}})
	if response["type"] != "ok" || response["id"] != float64(2) {
		t.Fatal(response)
	}
	if data := next("render"); data["html"] != "<p>更新</p>" || data["rev"] != float64(2) || data["extra"] != "<>&日本語" {
		t.Fatal(data)
	}
	if err := commands.Close(); err != nil {
		t.Fatal(err)
	}
	if data := next("stop"); data["reason"] != "exit" {
		t.Fatal(data)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("標準入力が閉じてもサーバーが終了しません")
	}
	if response, err := client.Get(url); err == nil {
		_ = response.Body.Close()
		t.Fatal("終了後もサーバーが待ち受けています")
	}
}

func TestFixtureDiagram(t *testing.T) {
	diagram := &fixtureDiagram{done: make(chan struct{})}
	defer diagram.Close()
	for _, source := range []string{"日本語", "multiple", "error200"} {
		svg, err := diagram.Render(context.Background(), source)
		if err != nil || string(svg) != diagramSVG {
			t.Fatalf("成功する原文 %q: %v", source, err)
		}
	}
	for _, source := range []string{"plain", "png", "invalid-utf8", "oversize", "empty"} {
		if _, err := diagram.Render(context.Background(), source); err == nil {
			t.Fatalf("失敗する原文 %q が成功しました", source)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := diagram.Render(ctx, "slow"); err == nil {
		t.Fatal("遅い応答の取り消しが届きません")
	}
	waitCtx, cancelWait := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelWait()
	if _, err := diagram.Render(waitCtx, "hang"); err == nil {
		t.Fatal("応答待ちの取り消しが届きません")
	}
}
