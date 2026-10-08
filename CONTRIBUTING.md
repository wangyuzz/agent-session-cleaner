# Contributing / 参与贡献

This fork welcomes reproducible bug reports and focused improvements. Describe the operating system, agent, application version, steps to reproduce, and expected behavior. Use synthetic session fixtures when sharing examples; remove private conversation content and credentials from reports.

欢迎提交可复现的问题与明确的功能改进。报告中请注明操作系统、Agent、程序版本、复现步骤及预期行为。示例优先使用人工构造的会话，提交前移除私人对话与凭据。

## Build / 构建

Use Go 1.25.8 or newer, matching the minimum in `go.mod`.

使用 Go 1.25.8 或更新版本，最低版本以 `go.mod` 为准。

```bash
go build -o dist/asc ./cmd/agent-session-cleaner
```

On Windows / Windows：

```powershell
go build -o dist/asc.exe ./cmd/agent-session-cleaner
```

## Validate / 验证

Run the checks used by CI before proposing a change:

提交变更前请运行 CI 使用的检查：

```bash
gofmt -l cmd internal
go vet ./...
go tool staticcheck ./...
go test ./...
```

`gofmt -l` should produce no output. For concurrency changes, also run `go test -race ./...` on a platform with a race-enabled Go toolchain and C compiler. The tests use temporary directories and synthetic agent data. To opt into read-only tests of real installed agent data, set `ASC_TEST_REAL_SESSIONS=1`; see `internal/agent/real_test.go`.

`gofmt -l` 应没有输出。并发相关变更还应在支持竞态检测、具备 C 编译器的平台运行 `go test -race ./...`。测试使用临时目录与人工构造的数据；设置 `ASC_TEST_REAL_SESSIONS=1` 可主动开启真实 Agent 数据的只读测试，见 `internal/agent/real_test.go`。

Keep English and Chinese user-facing text in sync. Prefix commit messages with a type such as `feat:`, `fix:`, or `docs:`. Before a release tag is created, update `internal/buildinfo/buildinfo.go` to match the tag without its leading `v`.

用户可见的英文与中文内容应同步更新。提交信息请使用 `feat:`、`fix:`、`docs:` 等前缀。创建发布标签前，更新 `internal/buildinfo/buildinfo.go`，使版本与去掉 `v` 前缀的标签一致。
