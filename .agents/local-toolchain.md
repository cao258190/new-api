# 本机临时工具链

本项目在当前 Windows 电脑上使用以下临时安装，不需要全局安装 Go 或 Bun。执行安装前先检查路径是否存在，避免重复下载。

| 工具 | 版本 | 可执行文件 |
| --- | --- | --- |
| Go | 1.26.10 | `C:\Users\25819\.codex\tmp\go1.26.10\go\bin\go.exe` |
| Bun | 1.4.4 | `C:\Users\25819\.codex\tmp\subscription-test\node_modules\bun\bin\bun.exe` |

PowerShell 调用示例：

```powershell
& 'C:\Users\25819\.codex\tmp\go1.26.10\go\bin\go.exe' version
& 'C:\Users\25819\.codex\tmp\subscription-test\node_modules\bun\bin\bun.exe' --version
```

这些路径位于 Codex 临时目录，可能被清理。若检查发现文件不存在，再按需安装到项目或用户目录；不要为本项目执行全局安装。

数据库验证也可复用以下已有的便携安装，不需要 Docker 或系统服务安装：

| 数据库 | 版本 | 可执行文件目录 | 本地测试端口 |
| --- | --- | --- | --- |
| MySQL | 5.7.44 | `C:\Users\25819\.codex\tmp\subscription-db\mysql\mysql-5.7.44-winx64\bin` | `13317` |
| PostgreSQL | 16.10 | `C:\Users\25819\.codex\tmp\subscription-db\postgres\pgsql\bin` | `15437` |

数据目录分别是同一 `subscription-db` 目录下的 `mysql-data` 和 `pg-data`。测试实例仅监听 `127.0.0.1`，使用独立测试库；执行前先检查现有进程，验证后关闭测试实例。
