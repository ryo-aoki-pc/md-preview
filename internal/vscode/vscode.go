// Package vscode は、検証したフォルダーで VS Code の Agents Window を開く。
package vscode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"
)

var (
	// ErrNotFound は、PATH に利用できる code コマンドがないことを表す。
	ErrNotFound = errors.New("VS Code の code コマンドが見つかりません。VS Code を入れて、PATH に bin のフォルダーを入れてください。")
	// ErrLaunch は、コマンドの起動・終了・待機に失敗したことを表す。
	ErrLaunch = errors.New("VS Code の Agents Window を開けませんでした")
)

const launchTimeout = 30 * time.Second

// Command は、シェルを介さずに起こす実行ファイルと、その引数・子の環境を持つ。
type Command struct {
	Path string   // 起こす実行ファイルの絶対パス。
	Args []string // Path を除いた引数。
	Env  []string // 子の環境。親の環境とは別の配列にする。
}

// Launcher は、試験用にプロセスの起動を差し替えられる起動器。
// Run が nil なら、実際のプロセスを起こして終了を待つ。
type Launcher struct {
	Run func(context.Context, Command) error
}

// Open は、root を VS Code の Agents Window で開き、起動コマンドの終了を待つ。
func Open(ctx context.Context, root string) error {
	return (Launcher{}).Open(ctx, root)
}

// Open は、PATH から起動コマンドを組み、30秒以内に実行する。
// 親の ctx の期限が短ければ、その期限を使う。
func (l Launcher) Open(ctx context.Context, root string) error {
	command, err := AgentsCommand(runtime.GOOS, os.Getenv("PATH"), os.Environ(), root)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	run := l.Run
	if run == nil {
		run = runCommand
	}
	if err := run(ctx, command); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		reason := "コマンドを開始できませんでした"
		var exit *exec.ExitError
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			reason = "起動の待ち時間を超えました"
		case errors.Is(err, context.Canceled):
			reason = "起動の待機が取り消されました"
		case errors.As(err, &exit):
			reason = fmt.Sprintf("起動コマンドが終了コード %d で終了しました", exit.ExitCode())
		}
		return &launchError{cause: err, reason: reason}
	}
	return nil
}

type launchError struct {
	cause  error
	reason string
}

func (e *launchError) Error() string        { return ErrLaunch.Error() + "（" + e.reason + "）。" }
func (e *launchError) Unwrap() error        { return e.cause }
func (e *launchError) Is(target error) bool { return target == ErrLaunch }

// Message は端末のパスを含まない起動の知らせを返す。未知のエラーの原文は出さない。
func Message(err error) string {
	for _, known := range []error{ErrNotFound, errRootAbsolute, errRootDirectory} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	var failure *launchError
	if errors.As(err, &failure) {
		return failure.Error()
	}
	return ErrLaunch.Error() + "。"
}

func runCommand(ctx context.Context, command Command) error {
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()
	cmd := exec.CommandContext(ctx, command.Path, command.Args...)
	cmd.Env = command.Env
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	prepareCommand(cmd)
	return cmd.Run()
}
