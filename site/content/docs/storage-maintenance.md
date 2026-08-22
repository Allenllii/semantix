# 存储、维护与安全

Semantix 的切片库是本地文件资产。运维目标是可备份、可审计、可回滚，而不是让缓存无限增长。

## 备份

默认项目库位于 `.semantix/project.db`。在写入停止后复制主文件；如果存在归档文件，也一起保留。

```bash
cp .semantix/project.db .semantix/project.db.backup
cp .semantix/project.db.archive.jsonl .semantix/project.db.archive.jsonl.backup
```

Windows PowerShell 可使用 `Copy-Item`。恢复前先备份当前版本，不要用未经检查的旧库覆盖唯一副本。

## GC 与归档

```bash
semantix gc --db .semantix/project.db
```

GC 会重算价值并按类型、时效与配置上限选择淘汰项。超限切片归档到 `<db>.archive.jsonl`；归档不是删除证明，也不等于敏感数据已经清除。

## 数据权限

实现会为本地库使用受限权限并防御符号链接替换，但运维仍需保证：

- 数据目录只对需要的系统用户开放。
- 会话导入前完成凭据和个人信息脱敏。
- 备份介质使用同等级访问控制。
- 不把 `.semantix/` 提交到版本库。

## 注入安全

历史切片是数据，不是高优先级指令。宿主 agent 应把注入块与 system prompt 分隔，并继续执行当前权限检查。

## 维护前检查

1. 记录当前库路径和文件大小。
2. 创建可恢复备份。
3. 使用 `dashboard` 或 `--json` 记录 GC 前状态。
4. 运行 GC。
5. 聚焦验证常用查询仍能命中。

## 对应仓库来源

- `kernel/slice/file_store.go`
- `kernel/slice/maintenance.go`
- `kernel/slice/score.go`
- `docs/specs/slice-store-append-journal.md`
- `SECURITY.md`
