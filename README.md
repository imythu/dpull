# dpull

[![Beta Release](https://github.com/imythu/dpull/actions/workflows/beta-release.yml/badge.svg)](https://github.com/imythu/dpull/actions/workflows/beta-release.yml)

`dpull` 是一个面向代理网络、Compose 项目和重复部署场景的 `docker pull` 增强替代工具。它使用
[`crane`](https://github.com/google/go-containerregistry/tree/main/cmd/crane) 下载镜像归档，
再通过 `docker load` 导入 Docker；既不重复实现复杂的 OCI 协议，也不牺牲 Docker 的使用习惯。

一句话概括：**让受限网络中的镜像拉取更稳定，让 Compose 项目的镜像准备从一串手工命令变成一次可靠执行。**

## 为什么 dpull 值得使用

`docker pull` 很优秀，但它的职责是通用镜像拉取，不负责替你解析整个 Compose 项目、管理代理层级、
清理临时归档，或判断同一个 tag 的远端内容是否已经变化。`dpull` 正好补上了这段工程化空白：

- **代理场景真正开箱即用。** 内置、本机、用户、环境变量和单次命令五级代理配置有明确优先级，
  HTTP、HTTPS、SOCKS5 均可使用；`dpull proxy` 还能直接解释最终生效来源。代理问题不再靠猜。
- **不是“看到 tag 就跳过”。** 每次先用 `crane digest` 获取注册表最新 manifest digest，再与 Docker
  本地 `RepoDigests` 对比。即使仍叫 `latest` 或版本号没变，只要内容变了就会重新拉取；内容没变则省掉下载。
- **Compose 使用体验非常直接。** 无参数即可发现标准 Compose 文件、提取所有 `image`、忽略纯 `build`
  服务并去重。配合 `--up`，从准备镜像到启动服务是一条完整且失败可见的流水线。
- **可靠性来自成熟组件，而不是重新造协议栈。** 注册表认证、OCI 清单和镜像下载交给广泛使用的
  `crane`；导入和启动交给 Docker。dpull 只编排边界清晰、容易审计的流程，因此小而可靠。
- **缓存不会悄悄失控。** 每次运行使用纳秒时间戳独立目录，可并发执行；一小时过期缓存自动回收，
  `dpull clean` 还能一键清除完成或中断的下载。
- **适合自动化。** 全程使用 `context.Context`、明确的 0/1 退出码、原样透传 crane 输出，并在最后给出
  成功、失败和耗时汇总。脚本和 CI 可以准确判断结果，人也能迅速定位失败镜像。
- **安装后的体验完整。** crane 缺失时可自动下载官方 Release，代理同样生效；Bash、Zsh、Fish、
  PowerShell 补全均可一条命令安装。

## 典型使用场景

- **开发机通过本地代理访问镜像仓库：** 设置一次代理，此后直接像使用 `docker pull` 一样运行 dpull。
- **一台新服务器部署 Compose 项目：** 将 Compose 文件放到目录，执行 `dpull --up`，自动准备镜像并启动。
- **同一 tag 被持续发布：** dpull 对比真实 digest，不会因为本地已有同名 tag 而漏掉远端更新。
- **多镜像批量预热：** `dpull nginx redis mysql` 统一拉取、导入、清理并汇总结果。
- **不稳定网络或临时中断：** 独立缓存避免并发任务互相破坏，遗留文件会自动过期，也能主动清理。
- **CI/CD 和运维脚本：** 确定的退出码、可读日志、Compose 支持和无隐藏下载逻辑，便于集成与审计。

dpull 并不试图替代 Docker 或 crane。它的优势恰恰在于把二者已经验证过的能力组合成一条更顺手、
更透明、更适合真实部署环境的工作流。对于完全不需要代理、只偶尔拉取单个固定 digest 的用户，
直接使用 `docker pull` 已经足够；而一旦涉及 Compose、批量镜像、代理或可变 tag，dpull 会显著减少重复操作和人为失误。

## 功能

- 拉取一个或多个镜像，并自动去重
- 自动发现并解析 Compose 文件中的 `services.*.image`
- 忽略仅包含 `build`、没有 `image` 的服务
- 为每次运行创建独立缓存目录，支持并发执行
- 启动时清理超过一小时的缓存目录
- 为 `crane` 单独配置 HTTP、HTTPS 或 SOCKS5 代理
- 可选择保留下载的 tar 文件
- 可在导入完成后执行 `docker compose up -d`
- 拉取前使用 crane 获取远端最新 digest，与本地镜像 digest 一致时自动跳过
- 使用 `dpull clean` 清除完成或未完成的全部下载缓存
- 缺少 crane 时自动询问并从官方 Release 下载

## 安装

运行前需要安装 Docker。`dpull` 会使用 PATH 中已有的 crane；如果未找到，会询问是否自动下载官方版本到
`~/.dpull/bin`。Compose 启动功能需要 Docker Compose v2。

每次向 `master` 推送代码后，GitHub Actions 会自动运行测试，并为 Linux、Windows、macOS 的
amd64/arm64 架构生成带 SHA-256 校验文件的 Beta Pre-release。预编译包可从
[Releases](https://github.com/imythu/dpull/releases) 下载。

```bash
go install github.com/imythu/dpull@latest
```

## 编译

需要 Go 1.24 或更高版本：

```bash
git clone https://github.com/imythu/dpull.git
cd dpull
go build -o dpull .
```

Windows 可将输出文件改为 `dpull.exe`。

## 使用示例

```bash
dpull
dpull nginx
dpull nginx redis mysql
dpull --proxy socks5://127.0.0.1:7890 nginx
dpull proxy
dpull proxy set socks5://127.0.0.1:7890
sudo dpull proxy set -g http://proxy.example.com:8080
dpull clean
dpull crane install
dpull crane install --proxy socks5://127.0.0.1:7890
dpull completion install
dpull completion install bash
dpull --keep nginx
dpull -f docker-compose.yml
dpull --up
dpull -f compose.yaml --up
```

不传镜像时，程序依次查找 `docker-compose.yml`、`docker-compose.yaml`、`compose.yml`、
`compose.yaml`，并使用第一个存在的文件。

## CLI 参数

| 参数 | 说明 |
| --- | --- |
| `-f, --file FILE` | 指定 Compose 文件 |
| `--proxy URL` | 为 crane 设置 `ALL_PROXY`、`HTTP_PROXY` 和 `HTTPS_PROXY` |
| `--keep` | 导入后保留 tar 归档 |
| `--up` | 全部镜像成功后执行 `docker compose up -d` |
| `-h, --help` | 显示帮助 |

子命令 `dpull clean`（别名 `dpull cleanup`）会删除 `~/.crane` 下的全部缓存内容，
包括 `--keep` 保留的归档和下载中断产生的文件。请勿在其他 `dpull` 实例下载时运行该命令。

## 安装 crane

执行镜像拉取前，`dpull` 会检查 PATH 和 `~/.dpull/bin/crane`。未找到时会询问是否从
`google/go-containerregistry` 官方 GitHub Release 下载。也可以主动安装：

```bash
dpull crane install
# 等价的快捷命令
dpull install-crane
```

crane 下载使用与镜像拉取相同的代理优先级，也可以仅为本次安装指定代理：

```bash
dpull crane install --proxy http://127.0.0.1:7890
```

安装位置为 `~/.dpull/bin/crane`，Windows 下为 `~/.dpull/bin/crane.exe`。

## Shell 补全

支持 Bash、Zsh、Fish 和 PowerShell。自动识别当前 shell 并安装：

```bash
dpull completion install
```

也可以显式指定 shell：

```bash
dpull completion install bash
dpull completion install zsh
dpull completion install fish
dpull completion install powershell
```

使用 `dpull completion SHELL` 可将补全脚本输出到 stdout，而不执行安装。Zsh 和
PowerShell 安装后会显示一次性的启用提示，重新启动 shell 后即可获得命令、子命令和参数补全。

## 代理配置

`dpull` 按以下顺序读取代理，后面的有效值覆盖前面的值：

1. 内置默认值 `http://127.0.0.1:7890`
2. `/etc/dpull.conf`
3. `~/.dpull/dpull.conf`
4. 环境变量 `DPULL_PROXY`
5. 本次运行显式指定的 `--proxy`

配置文件格式为：

```ini
proxy=socks5://127.0.0.1:7890
```

支持 `http://`、`https://` 和 `socks5://` 代理 URL，也可以包含用户名和密码，例如
`http://user:password@proxy.example.com:8080`。

使用 `dpull proxy` 可以查看当前生效的代理、来源以及各层配置。使用
`dpull proxy set URL` 写入当前用户配置，使用 `dpull proxy set -g URL` 写入全局配置。
写入 `/etc/dpull.conf` 通常需要 `sudo`。

## 工作流程

1. 创建 `~/.crane`，并清理其中修改时间超过一小时的一级子目录。
2. 读取命令行镜像；没有镜像时，从 Compose 文件读取并去重。
3. 创建 `~/.crane/<UnixNano>/` 作为本次运行的独立缓存。
4. 执行 `crane digest IMAGE` 获取注册表中的最新 manifest digest。
5. 读取完整镜像引用的 Docker `RepoDigests`；digest 一致时跳过，标签相同但 digest 变化时重新拉取。
6. 对本地不存在或 digest 已变化的镜像执行 `crane pull IMAGE TAR`。
7. 执行 `docker load -i TAR`，未指定 `--keep` 时删除归档。
8. 指定 `--up` 且所有镜像成功时，执行 `docker compose up -d`。

任意镜像或 Compose 启动失败时退出码为 1；全部成功时退出码为 0。

## 常见问题

**为什么提示找不到 crane 或 docker？**

请先安装对应程序，并确认可执行文件位于当前 shell 的 `PATH` 中。

**代理会影响 docker 吗？**

不会。`--proxy` 只覆盖启动 `crane` 时的三个代理环境变量，Docker 保持原有环境。

**失败后 tar 为什么仍然存在？**

下载或导入失败时会保留可能有助于排查问题的文件。超过一小时后，下次启动 `dpull` 会清理对应缓存目录。

**`--up` 会在部分镜像失败后启动服务吗？**

不会。只有所有镜像都成功下载并导入后才会启动 Compose 服务。

## 为什么使用 crane

镜像协议、注册表认证和多平台清单处理有大量边界情况。`crane` 已经提供成熟的 OCI 镜像下载能力，
`dpull` 专注于命令行体验、Compose 解析、缓存生命周期和 Docker 导入，不重复实现这套协议栈。

## License

本项目使用 MIT License，详见 [LICENSE](LICENSE)。
