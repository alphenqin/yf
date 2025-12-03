# ================================
# Dockerfile 构建说明
# ================================
# 构建此镜像前，需要准备以下文件在同一目录：
#   1. Dockerfile（本文件）
#   2. flow2ftp（已编译好的 Linux amd64 二进制文件）
#
# 构建步骤：
#   1. 编译 flow2ftp 二进制（Linux amd64）：
#      GOOS=linux GOARCH=amd64 go build -o flow2ftp ./cmd/flow2ftp
#   2. 将编译好的 flow2ftp 和 Dockerfile 放在同一目录
#   3. 在该目录执行：docker build -t yaf-flow2ftp:alpha1 .
# ================================

# ================================
# 1. 构建阶段：编译 libfixbuf / YAF / super_mediator
# ================================

FROM debian:bookworm-slim AS builder

# 可选：构建时 HTTP 代理（给 curl / apt 用）
ARG APT_PROXY

ENV DEBIAN_FRONTEND=noninteractive
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig

ARG FIXBUF_VERSION=3.0.0.alpha2
ARG YAF_VERSION=3.0.0.alpha4
ARG SM_VERSION=2.0.0.alpha3

# 配置阿里云源，并安装构建依赖
RUN set -eux; \
    rm -f /etc/apt/sources.list.d/debian.sources || true; \
    echo "deb http://mirrors.aliyun.com/debian bookworm main contrib non-free non-free-firmware" > /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian bookworm-updates main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian-security bookworm-security main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        build-essential \
        pkg-config \
        wget curl ca-certificates \
        libglib2.0-dev \
        libpcap-dev \
        libpcre3-dev \
        zlib1g-dev \
        libssl-dev \
        liblua5.3-dev \
    ; \
    rm -rf /var/lib/apt/lists/*

WORKDIR /tmp

# ----------------
# 1.1 编译 libfixbuf
# ----------------
RUN HTTP_PROXY=${APT_PROXY} HTTPS_PROXY=${APT_PROXY} \
    curl -fSL "https://tools.netsa.cert.org/releases/libfixbuf-${FIXBUF_VERSION}.tar.gz" -o libfixbuf.tar.gz && \
    tar xzf libfixbuf.tar.gz && \
    cd "libfixbuf-${FIXBUF_VERSION}" && \
    ./configure --disable-tools && \
    make -j"$(nproc)" && \
    make install && \
    ldconfig && \
    cd /tmp && \
    rm -rf "libfixbuf-${FIXBUF_VERSION}" libfixbuf.tar.gz

# ----------------
# 1.2 编译 YAF（开启 applabel + DPI）
# ----------------
RUN HTTP_PROXY=${APT_PROXY} HTTPS_PROXY=${APT_PROXY} \
    curl -fSL "https://tools.netsa.cert.org/releases/yaf-${YAF_VERSION}.tar.gz" -o yaf.tar.gz && \
    tar xzf yaf.tar.gz && \
    cd "yaf-${YAF_VERSION}" && \
    ./configure --enable-applabel --enable-dpi && \
    make -j"$(nproc)" && \
    make install && \
    ldconfig && \
    cd /tmp && \
    rm -rf "yaf-${YAF_VERSION}" yaf.tar.gz

# ----------------
# 1.3 编译 super_mediator
# ----------------
RUN HTTP_PROXY=${APT_PROXY} HTTPS_PROXY=${APT_PROXY} \
    curl -fSL "https://tools.netsa.cert.org/releases/super_mediator-${SM_VERSION}.tar.gz" -o sm.tar.gz && \
    tar xzf sm.tar.gz && \
    cd "super_mediator-${SM_VERSION}" && \
    ./configure --with-mysql=no && \
    make -j"$(nproc)" && \
    make install && \
    ldconfig && \
    cd /tmp && \
    rm -rf "super_mediator-${SM_VERSION}" sm.tar.gz

# ================================
# 2. 运行阶段：精简运行环境 + flow2ftp + 排查工具 + supervisor
# ================================

FROM debian:bookworm-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive

# 配置阿里云源，安装运行时依赖 + 排查工具 + supervisor + 时区
RUN set -eux; \
    rm -f /etc/apt/sources.list.d/debian.sources || true; \
    echo "deb http://mirrors.aliyun.com/debian bookworm main contrib non-free non-free-firmware" > /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian bookworm-updates main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
    echo "deb http://mirrors.aliyun.com/debian-security bookworm-security main contrib non-free non-free-firmware" >> /etc/apt/sources.list; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        libglib2.0-0 \
        libpcap0.8 \
        libpcre3 \
        zlib1g \
        libssl3 \
        liblua5.3-0 \
        bash \
        # --- 排查工具 ---
        procps \ 
        tcpdump \
        netcat-traditional \
        iproute2 \
        net-tools \
        dnsutils \
        psmisc \
        vim-tiny \
        less \
        # --- supervisor + 时区 ---
        supervisor \
        tzdata \
    ; \
    # 设置时区为 Asia/Shanghai（东八区）
    ln -fs /usr/share/zoneinfo/Asia/Shanghai /etc/localtime; \
    echo "Asia/Shanghai" > /etc/timezone; \
    dpkg-reconfigure -f noninteractive tzdata; \
    rm -rf /var/lib/apt/lists/*

ENV TZ=Asia/Shanghai

# 拷贝 builder 安装好的 libfixbuf + yaf + super_mediator
COPY --from=builder /usr/local/ /usr/local/

# 让动态链接器能找到 /usr/local/lib 的 so，并刷新缓存
RUN set -eux; \
    echo "/usr/local/lib"  >  /etc/ld.so.conf.d/usr-local.conf; \
    echo "/usr/local/lib64" >> /etc/ld.so.conf.d/usr-local.conf; \
    ldconfig

ENV LD_LIBRARY_PATH=/usr/local/lib:/usr/local/lib64

# 拷贝 Go 二进制文件（从构建上下文）
# 注意：构建上下文必须包含已编译好的 flow2ftp 二进制文件（Linux amd64）
COPY flow2ftp /usr/local/bin/flow2ftp

RUN chmod +x /usr/local/bin/flow2ftp && \
    file /usr/local/bin/flow2ftp || true

# 数据缓存目录（flow2ftp 写滚动压缩文件用）
RUN mkdir -p /data

# 准备 YAF 配置目录
RUN mkdir -p /etc/yaf

# supervisor 日志目录
RUN mkdir -p /var/log/supervisor /var/run/supervisor

# 创建非 root 用户（抓网卡时 docker run 加 cap）
RUN useradd -r -s /usr/sbin/nologin yaf && \
    chown -R yaf:yaf /data /etc/yaf /var/log/supervisor /var/run/supervisor

# ================================
# 2.1 启动脚本：yaf 和 pipeline 分开，交给 supervisor 管理
# ================================

# 启动 YAF 的脚本
RUN cat >/usr/local/bin/start_yaf.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

YAF_CONFIG_FILE="${YAF_CONFIG_FILE:-/etc/yaf/yaf.init}"

echo "[start_yaf] 使用 YAF 配置文件: ${YAF_CONFIG_FILE}"

if [ ! -f "${YAF_CONFIG_FILE}" ]; then
  echo "[start_yaf] ERROR: 未找到 YAF 配置文件: ${YAF_CONFIG_FILE}" >&2
  echo "[start_yaf] 请在 docker run 时用 -v 挂载，例如：" >&2
  echo "  -v /path/on/host/yaf.init:/etc/yaf/yaf.init:ro" >&2
  exit 1
fi

# 直接 exec，保证进程 PID 由 supervisor 管控
exec yaf --config "${YAF_CONFIG_FILE}" "$@"
EOF

# 启动 super_mediator(TEXT) | flow2ftp 的脚本
RUN cat >/usr/local/bin/start_pipeline.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

YAF_CONFIG_FILE="${YAF_CONFIG_FILE:-/etc/yaf/yaf.init}"
SM_LISTEN_PORT="${SM_LISTEN_PORT:-18000}"
SM_FIELDS="${SM_FIELDS:-flowStartMilliseconds,flowEndMilliseconds,sourceIPv4Address,destinationIPv4Address,sourceTransportPort,destinationTransportPort,protocolIdentifier,silkAppLabel}"

echo "[start_pipeline] 使用 YAF 配置文件: ${YAF_CONFIG_FILE}"
echo "[start_pipeline] super_mediator 监听端口: ${SM_LISTEN_PORT}"
echo "[start_pipeline] TEXT 输出字段: ${SM_FIELDS}"

if [ ! -f "${YAF_CONFIG_FILE}" ]; then
  echo "[start_pipeline] ERROR: 未找到 YAF 配置文件: ${YAF_CONFIG_FILE}" >&2
  echo "[start_pipeline] 请在 docker run 时用 -v 挂载，例如：" >&2
  echo "  -v /path/on/host/yaf.init:/etc/yaf/yaf.init:ro" >&2
  exit 1
fi

# pipeline:
#   super_mediator(TEXT) --> stdout
#   flow2ftp 读取 TEXT，按配置滚动写文件并定时上传 FTP
exec bash -c "
  super_mediator \
    --ipfix-input=tcp \
    --ipfix-port=\"${SM_LISTEN_PORT}\" \
    --output-mode=TEXT \
    --print-headers \
    --out=- \
    --fields=\"${SM_FIELDS}\" \
    localhost \
  | /usr/local/bin/flow2ftp -config \"${YAF_CONFIG_FILE}\" -data-dir \"/data\"
"
EOF

RUN chmod +x /usr/local/bin/start_yaf.sh /usr/local/bin/start_pipeline.sh

# ================================
# 2.2 supervisor 配置：高可用 + 日志滚动 + 日志统一收集
# ================================

# 生成 supervisor 主配置文件
RUN cat >/etc/supervisor/supervisord.conf <<'EOF'
[unix_http_server]
file=/var/run/supervisor.sock
chmod=0700

[supervisord]
logfile=/var/log/supervisor/supervisord.log
logfile_maxbytes=50MB
logfile_backups=5
pidfile=/var/run/supervisord.pid
nodaemon=true
minfds=10240
minprocs=200
childlogdir=/var/log/supervisor

[rpcinterface:supervisor]
supervisor.rpcinterface_factory = supervisor.rpcinterface:make_main_rpcinterface

[supervisorctl]
serverurl=unix:///var/run/supervisor.sock

; ------------------------------
; YAF 进程，由 supervisor 管理
; ------------------------------
[program:yaf]
command=/usr/local/bin/start_yaf.sh
autorestart=true
startsecs=3
startretries=5
stopasgroup=true
killasgroup=true

; 所有 stdout/stderr 日志都写到同一个文件，并做滚动
stdout_logfile=/var/log/supervisor/supervisord.log
stdout_logfile_maxbytes=50MB
stdout_logfile_backups=5
stderr_logfile=/var/log/supervisor/supervisord.log
stderr_logfile_maxbytes=50MB
stderr_logfile_backups=5

; ------------------------------
; pipeline 进程：super_mediator | flow2ftp
; ------------------------------
[program:pipeline]
command=/usr/local/bin/start_pipeline.sh
autorestart=true
startsecs=3
startretries=5
stopasgroup=true
killasgroup=true

stdout_logfile=/var/log/supervisor/supervisord.log
stdout_logfile_maxbytes=50MB
stdout_logfile_backups=5
stderr_logfile=/var/log/supervisor/supervisord.log
stderr_logfile_maxbytes=50MB
stderr_logfile_backups=5
EOF

# 确保脚本没有 Windows 换行符
RUN sed -i 's/\r$//' /usr/local/bin/start_yaf.sh /usr/local/bin/start_pipeline.sh /etc/supervisor/supervisord.conf || true

# 最终以非 root 用户运行（抓包时需要 docker run 加能力）
USER yaf

WORKDIR /

# 使用 supervisor 作为唯一入口（A 方案）
ENTRYPOINT ["/usr/bin/supervisord", "-c", "/etc/supervisor/supervisord.conf"]

CMD []
