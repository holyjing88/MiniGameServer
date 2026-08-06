# 服务器脚本：仅在 Linux 执行

`deployments/` 下的构建、打包、安装、部署、启停脚本均为 **Linux 专用**，必须在 Linux 服务器上执行，**不要在 Windows PC 上跑**。

## 为什么

- 脚本内有 `uname` / root / systemd 等 Linux 环境检查。
- Windows 编辑若带上 CRLF，Linux 上会出现 `: No such file or directory`（shebang 被 `\r` 污染）。
- 安装路径、备份、systemd 等行为只针对 Linux 部署机。

## 正确做法

在 Linux 服务器上同步代码后执行，例如：

```bash
cd /path/to/minigamesvr
./deployments/linux/build.sh
./deployments/linux/package.sh
cd dist
sudo ./install.sh ./minigamesvr_YYYYMMDD_HHMMSS.tar.gz
```

默认安装为**全量更新**：二进制、配置、`minigamesvr.env` 都会按安装包刷新（旧目录会先备份）。若要保留线上旧 env：

```bash
sudo ./install.sh ./minigamesvr_xxx.tar.gz --keep-env
```

## 在 PC 上可以做什么

- 改脚本源码、查日志、用 Cursor 编辑。
- 改完用 LF 保存（仓库已配置 `.gitattributes`：`*.sh` → `eol=lf`）。
- 把改动同步到服务器后再执行。

## 相关脚本

| 脚本 | 用途 |
|---|---|
| `deployments/linux/build.sh` | 编译 Linux 二进制 |
| `deployments/linux/package.sh` | 打 tar.gz，并输出 `dist/install.sh` |
| `dist/install.sh` / `deployments/linux/install.sh` | 安装到 `/app/minigamesvr`（默认全量更新 env；`--keep-env` 可保留旧 env） |
| `deployments/linux/deploy*.sh` | 部署相关 |
| `release/bin/{start,stop,restart,status}.sh` | 进程启停（装机后） |
