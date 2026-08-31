<div align="center">
  <img src="./Logo.png" alt="SimpleWebShell-RemoteUpdate" width="100" />
  <h2>SimpleWebShell-RemoteUpdate</h2>
  <h3>基于 SimpleWebShell 的非侵入式远程更新工作流</h3>
</div>

### 一、项目简介

- 独立 Go CLI，通过远端已经运行的 [SimpleWebShell](../SimpleWebShell/) HTTP API 完成应用初始化安装、版本更新、软链接切换、健康检查、自动回滚和旧版本清理。
- **非侵入式**：不修改 SimpleWebShell、不向远端安装常驻 Agent、不要求开放 SSH，只调用现有 `/post`、`/file_send`、Session 等公开接口。
- 固定版本目录与原子软链接流程，适合 Linux 边缘设备、树莓派、Jetson 和普通服务器的可审计发布。
- 密钥建议只通过 `SIMPLEWEBSHELL_KEY` 环境变量传入，避免写入配置与命令历史。

### 二、固定更新流程

```text
<app.root>/
├── current  -> releases/<当前版本>[/install_subdir]
├── previous -> releases/<上一版本>[/install_subdir]
├── releases/
│   ├── v1/
│   └── v2/
├── incoming/                    # 上传暂存，成功解包后删除
└── .remoteupdate-app            # 应用标识
```

每次 `update` 固定执行：

1. 探测 SimpleWebShell、校验密钥，可选创建隔离 Session。
2. 初始化 `releases/`、`incoming/`。
3. 流式上传发布包到 `incoming/`。
4. 本地计算 SHA-256，并在远端使用 `sha256sum` / `shasum` 二次校验。
5. 解包到新的 `releases/<version>/`，绝不覆盖当前版本目录。
6. 执行可选 `pre_switch`。
7. 保存旧链接为 `previous`，原子切换 `current`。
8. 执行可选 `post_switch` 与 `health_check`。
9. 失败时自动恢复旧链接并执行可选 `rollback`；首次安装失败则移除 `current`。
10. 清理超出保留策略的非活动旧版本。

### 三、命令

| 命令 | 作用 |
|------|------|
| `probe` | 验证 SimpleWebShell 连通性与密钥 |
| `init` | 初始化远端目录，不切换版本 |
| `install` | 首次安装；已有 `current` 时拒绝覆盖 |
| `update` | 上传、校验、安装并切换新版本 |
| `status` | 输出 current / previous / releases JSON |
| `rollback` | 回滚到 previous 或指定版本 |
| `cleanup` | 清理旧的非活动版本 |

### 四、快速开始

```bash
make build
cp remoteupdate.example.json remoteupdate.json
export SIMPLEWEBSHELL_KEY='你的SimpleWebShell密码'

./simplewebshell-remoteupdate -config remoteupdate.json probe
./simplewebshell-remoteupdate -config remoteupdate.json init
./simplewebshell-remoteupdate -config remoteupdate.json install \
  -artifact ./my-service-v1.tar.gz -release v1
./simplewebshell-remoteupdate -config remoteupdate.json update \
  -artifact ./my-service-v2.tar.gz -release v2
./simplewebshell-remoteupdate -config remoteupdate.json status
./simplewebshell-remoteupdate -config remoteupdate.json rollback
```

只审阅流程、不改变远端：

```bash
./simplewebshell-remoteupdate -config remoteupdate.json -dry-run update \
  -artifact ./my-service-v2.tar.gz -release v2
```

> Go 标准 `flag` 规则要求全局参数（`-config`、`-dry-run`）放在子命令之前。

### 五、配置说明

```json
{
  "remote": {
    "url": "http://192.168.1.10:8878",
    "timeout": "10m",
    "session_enabled": true,
    "insecure_tls": false
  },
  "app": {
    "name": "my-service",
    "root": "/opt/my-service",
    "artifact_type": "tar.gz",
    "install_subdir": "",
    "pre_switch": "",
    "post_switch": "systemctl restart my-service",
    "health_check": "curl -fsS http://127.0.0.1:8080/health",
    "rollback": "systemctl restart my-service"
  },
  "policy": {
    "keep_releases": 5,
    "require_health_check": true,
    "disable_auto_rollback": false
  }
}
```

| 配置 | 说明 |
|------|------|
| `remote.url` | 已运行的 SimpleWebShell 地址 |
| `remote.timeout` | 单次 HTTP 操作超时，默认 `10m`，需覆盖大包上传时间 |
| `remote.session_enabled` | 工作流期间创建临时 Session，结束后删除 |
| `app.root` | 远端应用绝对路径 |
| `artifact_type` | `tar.gz` / `tgz` / `tar` / `zip` / `file` |
| `app.install_subdir` | 解包后 current 指向的安全相对子目录，例如 `dist` |
| `pre_switch` | 软链接切换前命令 |
| `post_switch` | 切换后启动或重启命令 |
| `health_check` | 切换后的健康检查命令，返回非 0 触发失败 |
| `rollback` | 恢复旧链接后的服务恢复命令 |
| `keep_releases` | 保留的非活动版本数量，current/previous 永不清理 |
| `require_health_check` | 强制要求配置健康检查 |
| `disable_auto_rollback` | 默认自动回滚；设为 true 才关闭 |

`remote.key` 可以写入 JSON，但不推荐。优先使用：

```bash
export SIMPLEWEBSHELL_KEY='secret'
```

### 六、软件包与生命周期示例

**压缩包：**

```bash
tar -czf my-service-v2.tar.gz my-service config/
```

配置：

```json
{
  "artifact_type": "tar.gz",
  "post_switch": "systemctl restart my-service",
  "health_check": "systemctl is-active --quiet my-service"
}
```

systemd 的 `ExecStart` 应直接引用稳定路径：

```ini
ExecStart=/opt/my-service/current/my-service
```

这样更新器只切换 `current`，服务配置不随版本变化。

### 七、Docker

```bash
docker build -t simplewebshell-remoteupdate:latest .
docker run --rm \
  -e SIMPLEWEBSHELL_KEY="$SIMPLEWEBSHELL_KEY" \
  -v "$PWD:/work" -w /work \
  simplewebshell-remoteupdate:latest \
  -config remoteupdate.json status
```

容器只运行本地更新 CLI，不需要特权模式；它通过网络访问远端 SimpleWebShell。

### 八、安全与适用范围

- 本工具拥有与 SimpleWebShell 密钥相同的远程命令权限，只能用于已授权设备。
- 推荐通过 VPN、内网或 HTTPS 反向代理访问 SimpleWebShell；不要把未加密 WebShell 直接暴露到公网。
- 版本号仅允许字母、数字、点、下划线、横线；远程路径统一进行 POSIX shell 单引号转义。
- `pre_switch` / `post_switch` / `health_check` / `rollback` 是管理员明确配置的可信 shell 命令。
- 软链接更新流程面向 Unix/Linux；Windows 不支持此固定软链接发布布局。
- SimpleWebShell 当前单条命令执行上限为 60 秒，因此生命周期命令应快速返回，长期进程应交给 systemd 等服务管理器。

### 九、目录结构

```text
SimpleWebShell-RemoteUpdate/
├── main.go
├── internal/
│   ├── config/                 # JSON 配置与安全校验
│   ├── webshell/               # SimpleWebShell HTTP API 客户端
│   └── workflow/               # 安装、更新、切换、回滚、清理
├── remoteupdate.example.json
├── Dockerfile
├── Makefile
├── LICENSE
├── README.md
└── README_en.md
```

### 十、License

Apache License 2.0
