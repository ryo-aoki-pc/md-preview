package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testEditProcess(t *testing.T, binary string) {
	for _, mode := range []string{"file", "folder", "off"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			file := filepath.Join(root, "main.md")
			before := "\ufeff最初\r\n次\r\n"
			if err := os.WriteFile(file, []byte(before), 0644); err != nil {
				t.Fatal(err)
			}
			args := []string{"--no-open", "--no-plantuml", "--no-official-style"}
			if mode != "off" {
				args = append(args, "--edit")
			}
			target := file
			query := ""
			if mode == "folder" {
				target = root
				query = "?doc=main.md"
			}
			args = append(args, target)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, args...)
			stdout, err := command.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			var stderr bytes.Buffer
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
			line, err := bufio.NewReader(stdout).ReadString('\n')
			if err != nil {
				t.Fatal(err)
			}
			url := strings.TrimSpace(strings.TrimPrefix(line, "プレビュー: "))
			origin := strings.TrimSuffix(strings.SplitN(url[7:], "/", 2)[0], "/")
			origin = "http://" + origin
			events, err := http.Get(url + "_/events" + query)
			if err != nil {
				t.Fatal(err)
			}
			defer events.Body.Close()
			renders := make(chan map[string]any, 8)
			go func() {
				scanner := bufio.NewScanner(events.Body)
				for scanner.Scan() {
					if scanner.Text() != "event: render" {
						continue
					}
					if !scanner.Scan() {
						break
					}
					var data map[string]any
					if json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &data) == nil {
						renders <- data
					}
				}
			}()
			var first map[string]any
			select {
			case first = <-renders:
			case <-ctx.Done():
				t.Fatal("最初の描画が無い")
			}
			body := fmt.Sprintf(`{"render":%.0f,"range":"1:1-1:6","text":"変更\n追加"}`, first["render"])
			req, err := http.NewRequest("POST", url+"_/edit"+query, strings.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Origin", origin)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(response.Body)
			_ = response.Body.Close()
			wantStatus := 200
			if mode == "off" {
				wantStatus = 405
			}
			if response.StatusCode != wantStatus {
				t.Fatal(response.Status, string(b))
			}
			want := before
			if mode != "off" {
				want = "\ufeff変更\r\n追加\r\n次\r\n"
				select {
				case next := <-renders:
					if next["render"] == first["render"] {
						t.Fatal(next)
					}
				case <-ctx.Done():
					t.Fatal("編集後の描画が無い")
				}
			}
			got, err := os.ReadFile(file)
			if err != nil || string(got) != want {
				t.Fatalf("原文=%q、期待=%q、誤り=%v", got, want, err)
			}
			select {
			case extra := <-renders:
				t.Fatal("同じ編集を重ねて描いた", extra)
			case <-time.After(600 * time.Millisecond):
			}
		})
	}
}
