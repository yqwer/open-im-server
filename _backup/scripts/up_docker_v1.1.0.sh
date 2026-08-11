#!/usr/bin/env bash
# =============================================================================
# 版本:     1.1.0
# 修订日期: 2026-07-29
# 作者:     yqwer
# 生成者:   claude-sonnet-4-6
# 最后修订: claude-sonnet-4-6
# 变更说明: v1.0.0 初始版本
#           v1.1.0 编译时强制 CGO_ENABLED=0（静态链接），兼容 alpine 容器
# =============================================================================
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
export PATH=/usr/local/go/bin:$HOME/go/bin:$PATH
# alpine 容器无 glibc，必须静态链接
export CGO_ENABLED=0
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

CONTAINER="openim-server"
LOCAL_PLATFORMS="${PROJECT_ROOT}/_output/bin/platforms/linux/amd64"
LOCAL_TOOLS="${PROJECT_ROOT}/_output/bin/tools/linux/amd64"
CONTAINER_PLATFORMS="/openim-server/_output/bin/platforms/linux/amd64"
CONTAINER_TOOLS="/openim-server/_output/bin/tools/linux/amd64"

echo "================================================================"
echo " openIM Docker Deploy — $(date '+%Y-%m-%d %H:%M:%S')"
echo " Container : ${CONTAINER}"
echo " Source    : ${PROJECT_ROOT}"
echo "================================================================"

# 1. 编译（CGO_ENABLED=0 静态链接，兼容 alpine 容器）
echo ""
echo "--- [Step 0] 编译源码（CGO_ENABLED=0 静态链接）---"
cd "${PROJECT_ROOT}"
mage build
echo "编译完成。"

# 2. 停止宿主机上的源码进程（如有），释放端口
echo ""
echo "--- [Step 1] 停止宿主机源码进程（如有）---"
if pgrep -f "${LOCAL_PLATFORMS}/openim-api" > /dev/null 2>&1; then
    echo "检测到宿主机进程在运行，执行 mage stop ..."
    cd "${PROJECT_ROOT}" && mage stop 2>&1 | tail -3
    sleep 1
else
    echo "宿主机无 openIM 进程，跳过。"
fi

# 3. 停止 Docker 容器
echo ""
echo "--- [Step 2] 停止容器 ${CONTAINER} ---"
STATUS=$(docker inspect --format='{{.State.Status}}' "${CONTAINER}" 2>/dev/null || echo "not_found")
if [ "${STATUS}" = "not_found" ]; then
    echo "[ERROR] 容器 ${CONTAINER} 不存在，请检查 Docker 环境。"
    exit 1
fi
if [ "${STATUS}" = "running" ]; then
    docker stop "${CONTAINER}"
    echo "容器已停止。"
else
    echo "容器当前状态: ${STATUS}，无需停止。"
fi

# 4. 注入服务可执行文件
echo ""
echo "--- [Step 3] 注入服务可执行文件 → ${CONTAINER_PLATFORMS} ---"
docker cp "${LOCAL_PLATFORMS}/." "${CONTAINER}:${CONTAINER_PLATFORMS}/"
echo "服务二进制注入完成。"

# 5. 注入工具可执行文件
echo ""
echo "--- [Step 4] 注入工具可执行文件 → ${CONTAINER_TOOLS} ---"
docker cp "${LOCAL_TOOLS}/." "${CONTAINER}:${CONTAINER_TOOLS}/"
echo "工具二进制注入完成。"

# 5b. 同步 share.yml（新版字段结构与旧容器配置不兼容，需覆盖）
echo ""
echo "--- [Step 4b] 同步 share.yml（解决 imAdminUser 字段兼容问题）---"
docker cp "${PROJECT_ROOT}/config/share.yml" "${CONTAINER}:/openim-server/config/share.yml"
echo "share.yml 同步完成。"

# 6. 启动容器
echo ""
echo "--- [Step 5] 启动容器 ${CONTAINER} ---"
docker start "${CONTAINER}"
echo "容器已启动，等待健康检查..."

# 7. 等待健康检查通过（最多 120s）
TIMEOUT=120
ELAPSED=0
while [ ${ELAPSED} -lt ${TIMEOUT} ]; do
    HEALTH=$(docker inspect --format='{{.State.Health.Status}}' "${CONTAINER}" 2>/dev/null || echo "unknown")
    STATUS=$(docker inspect --format='{{.State.Status}}' "${CONTAINER}" 2>/dev/null || echo "unknown")
    if [ "${HEALTH}" = "healthy" ]; then
        echo "健康检查通过 (${ELAPSED}s)。"
        break
    elif [ "${STATUS}" != "running" ]; then
        echo "[ERROR] 容器意外退出，请查看日志: docker logs ${CONTAINER}"
        exit 1
    fi
    echo "  等待中... status=${STATUS} health=${HEALTH} (${ELAPSED}s/${TIMEOUT}s)"
    sleep 5
    ELAPSED=$((ELAPSED + 5))
done

if [ ${ELAPSED} -ge ${TIMEOUT} ]; then
    echo "[WARN] 健康检查超时 (${TIMEOUT}s)，请手动检查: docker logs ${CONTAINER}"
fi

# 8. 输出摘要
echo ""
echo "================================================================"
echo " 部署完成"
HEALTH=$(docker inspect --format='{{.State.Health.Status}}' "${CONTAINER}" 2>/dev/null || echo "unknown")
STATUS=$(docker inspect --format='{{.State.Status}}' "${CONTAINER}" 2>/dev/null || echo "unknown")
echo "  容器状态  : ${STATUS}"
echo "  健康状态  : ${HEALTH}"
echo "  查看日志  : docker logs -f ${CONTAINER}"
echo "  检查服务  : docker exec ${CONTAINER} mage check"
echo "================================================================"
