#!/usr/bin/env bash
# =============================================================================
# 版本:     2.0.0
# 修订日期: 2026-07-29
# 作者:     yqwer
# 生成者:   claude-sonnet-4-6
# 最后修订: claude-sonnet-4-6
# 变更说明: v1.0.0 初始版本
#           v1.1.0 编译时强制 CGO_ENABLED=0（静态链接），兼容 alpine 容器
#           v2.0.0 职责精简：仅负责注入二进制 + 重启容器，不含编译和宿主机进程管理
#                  使用前请先手动运行 scripts/build.sh 完成编译
# =============================================================================
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"

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

# 1. 检查编译产物是否存在
if [ ! -f "${LOCAL_PLATFORMS}/openim-api" ]; then
    echo "[ERROR] 未找到编译产物: ${LOCAL_PLATFORMS}/openim-api"
    echo "        请先执行: bash scripts/build.sh"
    exit 1
fi

# 2. 检查容器是否存在
STATUS=$(docker inspect --format='{{.State.Status}}' "${CONTAINER}" 2>/dev/null || echo "not_found")
if [ "${STATUS}" = "not_found" ]; then
    echo "[ERROR] 容器 ${CONTAINER} 不存在，请检查 Docker 环境。"
    exit 1
fi

# 3. 停止容器
echo ""
echo "--- [Step 1] 停止容器 ${CONTAINER} ---"
if [ "${STATUS}" = "running" ]; then
    docker stop "${CONTAINER}"
    echo "容器已停止。"
else
    echo "容器当前状态: ${STATUS}，无需停止。"
fi

# 4. 注入服务可执行文件
echo ""
echo "--- [Step 2] 注入服务二进制 → ${CONTAINER_PLATFORMS} ---"
docker cp "${LOCAL_PLATFORMS}/." "${CONTAINER}:${CONTAINER_PLATFORMS}/"
echo "服务二进制注入完成。"

# 5. 注入工具可执行文件
echo ""
echo "--- [Step 3] 注入工具二进制 → ${CONTAINER_TOOLS} ---"
docker cp "${LOCAL_TOOLS}/." "${CONTAINER}:${CONTAINER_TOOLS}/"
echo "工具二进制注入完成。"

# 6. 同步 share.yml（新版字段 imAdminUser 结构与旧容器配置不兼容）
echo ""
echo "--- [Step 4] 同步 share.yml ---"
docker cp "${PROJECT_ROOT}/config/share.yml" "${CONTAINER}:/openim-server/config/share.yml"
echo "share.yml 同步完成。"

# 7. 启动容器
echo ""
echo "--- [Step 5] 启动容器 ${CONTAINER} ---"
docker start "${CONTAINER}"
echo "容器已启动，等待服务就绪..."

# 8. 等待服务就绪（通过 mage check 验证，最多 180s）
TIMEOUT=180
ELAPSED=0
while [ ${ELAPSED} -lt ${TIMEOUT} ]; do
    CUR_STATUS=$(docker inspect --format='{{.State.Status}}' "${CONTAINER}" 2>/dev/null || echo "unknown")
    if [ "${CUR_STATUS}" != "running" ]; then
        echo "[ERROR] 容器意外退出，请查看日志: docker logs ${CONTAINER}"
        exit 1
    fi
    if docker exec "${CONTAINER}" mage check > /dev/null 2>&1; then
        echo "服务就绪 (${ELAPSED}s)。"
        break
    fi
    echo "  等待中... (${ELAPSED}s/${TIMEOUT}s)"
    sleep 10
    ELAPSED=$((ELAPSED + 10))
done

if [ ${ELAPSED} -ge ${TIMEOUT} ]; then
    echo "[WARN] 等待超时 (${TIMEOUT}s)，请手动检查:"
    echo "       docker logs ${CONTAINER}"
    echo "       docker exec ${CONTAINER} mage check"
fi

# 9. 输出摘要
echo ""
echo "================================================================"
echo " 部署完成 — $(date '+%Y-%m-%d %H:%M:%S')"
CUR_STATUS=$(docker inspect --format='{{.State.Status}}' "${CONTAINER}" 2>/dev/null || echo "unknown")
CUR_HEALTH=$(docker inspect --format='{{.State.Health.Status}}' "${CONTAINER}" 2>/dev/null || echo "unknown")
echo "  容器状态  : ${CUR_STATUS}"
echo "  健康状态  : ${CUR_HEALTH}"
echo "  查看日志  : docker logs -f ${CONTAINER}"
echo "  检查服务  : docker exec ${CONTAINER} mage check"
echo "================================================================"
