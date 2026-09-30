package command

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/opskat/opskat/internal/model/entity/asset_entity"
	policyent "github.com/opskat/opskat/internal/model/entity/policy"
	"github.com/opskat/opskat/internal/repository/asset_repo"
	"github.com/opskat/opskat/internal/service/command_review_svc"
)

func mustPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	return r, w
}

func mustOpen(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path) // #nosec G304 -- path is os.DevNull or a file this test created under t.TempDir.
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func TestInspectStdin(t *testing.T) {
	Convey("判断 stdin 有没有要转发给远端命令的内容", t, func() {
		Convey("管道里有内容：算有输入，转发时一个字节不少", func() {
			r, w := mustPipe(t)
			_, _ = w.WriteString("line1\nline2\n")
			_ = w.Close()

			in, has := inspectStdin(r, time.Second)
			So(has, ShouldBeTrue)
			b, err := io.ReadAll(in)
			So(err, ShouldBeNil)
			So(string(b), ShouldEqual, "line1\nline2\n")
		})

		Convey("管道已经关闭、没有内容（调用方关掉了 stdin）：没有输入，不用等", func() {
			r, w := mustPipe(t)
			_ = w.Close()

			start := time.Now()
			in, has := inspectStdin(r, 5*time.Second)
			So(has, ShouldBeFalse)
			So(in, ShouldBeNil)
			So(time.Since(start), ShouldBeLessThan, time.Second)
		})

		Convey("管道开着一直不写：分不清，按有输入处理；之后写进来的内容照样转发", func() {
			r, w := mustPipe(t)

			in, has := inspectStdin(r, 50*time.Millisecond)
			So(has, ShouldBeTrue)
			_, _ = w.WriteString("late\n")
			_ = w.Close()
			b, err := io.ReadAll(in)
			So(err, ShouldBeNil)
			So(string(b), ShouldEqual, "late\n")
		})

		Convey("空设备（< /dev/null）：没有输入", func() {
			in, has := inspectStdin(mustOpen(t, os.DevNull), time.Second)
			So(has, ShouldBeFalse)
			So(in, ShouldBeNil)
		})

		Convey("重定向的普通文件：有内容才算有输入", func() {
			dir := t.TempDir()
			full := filepath.Join(dir, "full.txt")
			empty := filepath.Join(dir, "empty.txt")
			So(os.WriteFile(full, []byte("data"), 0o600), ShouldBeNil)
			So(os.WriteFile(empty, nil, 0o600), ShouldBeNil)

			in, has := inspectStdin(mustOpen(t, full), time.Second)
			So(has, ShouldBeTrue)
			b, err := io.ReadAll(in)
			So(err, ShouldBeNil)
			So(string(b), ShouldEqual, "data")

			in, has = inspectStdin(mustOpen(t, empty), time.Second)
			So(has, ShouldBeFalse)
			So(in, ShouldBeNil)
		})
	})
}

// passReviewer 让每条命令都审核通过，并记下送审了几条。
type passReviewer struct{ reviewed int }

func (r *passReviewer) Review(ctx context.Context, in command_review_svc.Input) command_review_svc.Result {
	return r.ReviewBatch(ctx, []command_review_svc.Input{in})[0]
}

func (r *passReviewer) ReviewBatch(_ context.Context, ins []command_review_svc.Input) []command_review_svc.Result {
	r.reviewed += len(ins)
	out := make([]command_review_svc.Result, len(ins))
	for i := range out {
		out[i] = command_review_svc.Result{Outcome: command_review_svc.OutcomePass}
	}
	return out
}

func (r *passReviewer) TestModel(context.Context, command_review_svc.Config) (string, error) {
	return "", nil
}
func (r *passReviewer) Status() command_review_svc.Status   { return command_review_svc.Status{} }
func (r *passReviewer) SetConfigErrorListener(func(string)) {}

func setAssetPermissionMode(t *testing.T, env *opsctlExecTestEnv, name, mode string) {
	t.Helper()
	assets, err := asset_repo.Asset().List(env.ctx, asset_repo.ListOptions{})
	if err != nil {
		t.Fatalf("list test assets: %v", err)
	}
	for _, asset := range assets {
		if asset.Name == name {
			asset.PermissionMode = mode
			return
		}
	}
	t.Fatalf("test asset %q not found", name)
}

// setupPipedExec 搭一个 web-1 为 Autopilot、审核一律通过的环境，stdin 由参数指定，
// 并记下转发给 ssh 执行的 stdin 内容。
func setupPipedExec(t *testing.T, stdin io.Reader, piped bool) (*opsctlExecTestEnv, *passReviewer, *string) {
	t.Helper()
	restoreAssetRepoAfter(t)
	env := setupOpsctlExecAssets(t)
	isolateApprovers(t)
	setAssetPermissionMode(t, env, "web-1", policyent.PermissionModeAutopilot)

	reviewer := &passReviewer{}
	origReviewer := command_review_svc.Default()
	command_review_svc.Register(reviewer)
	t.Cleanup(func() { command_review_svc.Register(origReviewer) })

	origStdin := execStdinFn
	execStdinFn = func() (io.Reader, bool) { return stdin, piped }
	t.Cleanup(func() { execStdinFn = origStdin })

	forwarded := new(string)
	stub := execSSHStreamFn
	execSSHStreamFn = func(ctx context.Context, auditCtx context.Context, asset *asset_entity.Asset, command string, in io.Reader, result ApprovalResult) int {
		if in != nil {
			b, _ := io.ReadAll(in)
			*forwarded = string(b)
		}
		return stub(ctx, auditCtx, asset, command, in, result)
	}
	t.Cleanup(func() { execSSHStreamFn = stub })
	return env, reviewer, forwarded
}

func TestCmdExec_AutopilotPipedInput(t *testing.T) {
	t.Run("有管道输入：模型看不到那部分，不送审，直接拒绝，命令不发到远端", func(t *testing.T) {
		env, reviewer, _ := setupPipedExec(t, strings.NewReader("rm -rf /srv/data\n"), true)

		var code int
		stderr := captureStderr(t, func() {
			code = cmdExec(env.ctx, env.handlers, []string{"web-1", "--type", "ssh", "--", "bash"}, "")
		})

		if code == 0 {
			t.Fatal("piped input on an Autopilot asset must be refused")
		}
		if env.sshStreamCalls != 0 {
			t.Fatal("refused command reached the remote shell")
		}
		if reviewer.reviewed != 0 {
			t.Fatalf("reviewed %d commands, want 0: the model cannot see the piped input", reviewer.reviewed)
		}
		if !strings.Contains(stderr, "piped input") || !strings.Contains(stderr, "/dev/null") {
			t.Fatalf("stderr must explain the piped input and how to avoid it, got:\n%s", stderr)
		}
	})

	t.Run("没有管道输入：照常送审，通过后执行", func(t *testing.T) {
		env, reviewer, _ := setupPipedExec(t, nil, false)

		code := cmdExec(env.ctx, env.handlers, []string{"web-1", "--type", "ssh", "--", "bash"}, "")

		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if reviewer.reviewed != 1 || env.sshStreamCalls != 1 {
			t.Fatalf("reviewed = %d, ssh stream calls = %d; want 1 and 1", reviewer.reviewed, env.sshStreamCalls)
		}
	})

	t.Run("有管道输入但规则放行：和原来一样执行，管道内容原样转发", func(t *testing.T) {
		env, reviewer, forwarded := setupPipedExec(t, strings.NewReader("key: value\n"), true)
		setAssetCommandPolicy(t, env, "web-1", asset_entity.CommandPolicy{AllowList: []string{"tee *"}})

		code := cmdExec(env.ctx, env.handlers, []string{"web-1", "--type", "ssh", "--", "tee /etc/app/config.yml"}, "")

		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if reviewer.reviewed != 0 || env.sshStreamCalls != 1 {
			t.Fatalf("reviewed = %d, ssh stream calls = %d; want 0 and 1", reviewer.reviewed, env.sshStreamCalls)
		}
		if *forwarded != "key: value\n" {
			t.Fatalf("forwarded stdin = %q, want the piped content", *forwarded)
		}
	})
}
