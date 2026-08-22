# 安装与首次运行

这条路径用于验证最小闭环：**历史会话 → 语义切片 → 检索 → 注入**。先在测试目录运行，再决定是否接入真实 agent。

## 选择安装方式

### 下载完整产品

GitHub Release 的完整包包含 coding agent 与 Semantix 内核。将 `<version>` 和 `<platform>` 替换为 Release 页面中的实际文件名：

```bash
tar -xzf semantix-agent-<version>-<platform>.tar.gz
cd semantix-agent-<version>-<platform>
./semantix-install.sh
```

Windows 用户可下载对应的 `windows-amd64` 或 `windows-arm64` 包，并把可执行文件所在目录加入 `PATH`。

### 从源码构建内核

仓库声明需要 Go 1.26 或更高版本：

```bash
git clone https://github.com/Gnosil/semantix.git
cd semantix
go build -o semantix ./cmd/semantix
```

Windows PowerShell 使用带扩展名的输出文件：

```powershell
go build -o semantix.exe ./cmd/semantix
```

先确认命令可以启动：

```bash
semantix version
semantix help
```

## 准备一段会话

`extract` 接收 JSONL；每行至少包含一条会话消息。下面是最小示例：

```json
{"role":"user","content":"修复 Go 测试失败"}
{"role":"assistant","content":"先定位失败包，再运行聚焦测试。"}
```

保存为 `session.jsonl`，然后运行：

```bash
semantix extract --input session.jsonl --db .semantix/project.db --project demo
semantix search --query "修复 Go 测试失败" --db .semantix/project.db
semantix inject --query "修复 Go 测试失败" --db .semantix/project.db
```

成功标准不是“命令没有报错”这么简单：`search` 应返回来源切片，`inject` 应只在存在合格候选时输出 `[semantix-reuse]` 块。

## 下一步

- 需要接入现有 coding agent：阅读《接入 Coding Agent》。
- 需要稳定保存项目设置：阅读《配置与作用域》。
- 需要判断真实命中率：阅读《验证、用量与健康检查》。

## 对应仓库来源

- `README.zh-CN.md` 的“快速上手”
- `docs/QUICKSTART.md`
- `cmd/semantix/extract.go`、`search.go`、`lookup.go`
