#!/bin/bash
set -euo pipefail

# 配置
IMAGE_NAME="b3log/siyuan"
CONTAINER_NAME="siyuan"
BACKUP_TAG="${IMAGE_NAME}:backup"
QQ_RECIPIENT="E98B6AF209C31C546A7DFA9FD9959176"

# 获取更新开始时间
UPDATE_START_TIME=$(date '+%Y年%m月%d日 %H:%M')

# 1. 获取旧镜像信息
OLD_IMAGE_ID=""
if OLD_IMAGE_ID=$(docker images -q "${IMAGE_NAME}:latest" 2>/dev/null) && [[ -n "$OLD_IMAGE_ID" ]]; then
    echo "当前镜像ID: ${OLD_IMAGE_ID:0:12}..."
else
    OLD_IMAGE_ID=""
    echo "未找到当前镜像"
fi

# 2. 停止并删除旧容器（如果存在）
if docker ps -a --format '{{.Names}}' | grep -qw "$CONTAINER_NAME"; then
    echo "Stopping and removing old container: $CONTAINER_NAME..."
    docker stop "$CONTAINER_NAME" || true
    docker rm -f "$CONTAINER_NAME" || true
fi

# 3. 备份旧镜像（如果存在）
if [[ -n "$OLD_IMAGE_ID" ]]; then
    echo "Backing up old image: ${IMAGE_NAME}:latest -> $BACKUP_TAG"
    docker tag "${IMAGE_NAME}:latest" "$BACKUP_TAG"
fi

# 4. 拉取新镜像
echo "Pulling latest image: $IMAGE_NAME..."
docker pull "$IMAGE_NAME"

# 5. 获取新镜像信息
NEW_IMAGE_ID=$(docker images -q "${IMAGE_NAME}:latest")
echo "新镜像ID: ${NEW_IMAGE_ID:0:12}..."

# 6. 启动新容器
echo "Running new container: $CONTAINER_NAME..."
docker run -d \
  --name "$CONTAINER_NAME" \
  -v /siyuan/workspace:/siyuan/workspace \
  -p 6806:6806 \
  -e PUID=1001 \
  -e PGID=1002 \
  --restart always \
  "$IMAGE_NAME" \
  serve \
  --workspace=/siyuan/workspace \
  --lang=zh_CN \
  --accessAuthCode=Shiep20083406

# 7. 等待容器启动
echo "等待容器启动..."
sleep 3

# 8. 检查容器状态
CONTAINER_STATUS=$(docker inspect --format='{{.State.Status}}' "$CONTAINER_NAME" 2>/dev/null || echo "未知")
echo "容器状态: $CONTAINER_STATUS"

# 9. 生成QQ消息
echo "生成QQ通知消息..."
OLD_IMAGE_SHORT=""
if [[ -n "$OLD_IMAGE_ID" ]]; then
    OLD_IMAGE_SHORT="${OLD_IMAGE_ID:0:12}..."
fi

QQ_MESSAGE=$(cat << EOF
【思源笔记更新完成】🔄

📅 更新时间：${UPDATE_START_TIME}

📊 版本信息：
• 容器名称：${CONTAINER_NAME}
• 镜像名称：${IMAGE_NAME}
• 旧镜像ID：${OLD_IMAGE_SHORT:-无}
• 新镜像ID：${NEW_IMAGE_ID:0:12}...

✅ 更新状态：成功
🔗 访问地址：http://www.yessirpopesama.cc:6806/

📈 当前状态：
$(docker ps --filter "name=${CONTAINER_NAME}" --format "└─ {{.Names}}: {{.Status}} (运行{{.RunningFor}})" 2>/dev/null || echo "└─ 状态获取中...")

🛠️ 常用命令：
• 查看日志：docker logs ${CONTAINER_NAME}
• 重启服务：docker restart ${CONTAINER_NAME}
• 进入终端：docker exec -it ${CONTAINER_NAME} sh

💡 提示：
1. 数据卷已挂载：/siyuan/workspace
2. 语言通过 --lang=zh_CN 设置为中文
3. 自动重启已启用
4. 如有问题可查看详细日志

更新完成！ 🎉
EOF
)

# 10. 保存消息到文件
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_DIR="/root/bash/siyuan_backup"
mkdir -p "$BACKUP_DIR"
MESSAGE_FILE="${BACKUP_DIR}/siyuan_update_qq_${TIMESTAMP}.txt"
echo "$QQ_MESSAGE" > "$MESSAGE_FILE"
echo "QQ消息已保存到备份目录: $MESSAGE_FILE"

# 11. 清理旧备份文件（保留最近10个）
echo "清理旧备份文件..."
cd "$BACKUP_DIR"
BACKUP_COUNT=$(ls -1 siyuan_update_qq_*.txt 2>/dev/null | wc -l)
if [ "$BACKUP_COUNT" -gt 10 ]; then
    FILES_TO_DELETE=$((BACKUP_COUNT - 10))
    echo "发现 $BACKUP_COUNT 个备份文件，删除最旧的 $FILES_TO_DELETE 个..."
    ls -1t siyuan_update_qq_*.txt | tail -n "$FILES_TO_DELETE" | xargs rm -f 2>/dev/null || true
    echo "备份文件清理完成"
else
    echo "当前有 $BACKUP_COUNT 个备份文件，无需清理"
fi

# 12. 显示更新完成信息
echo ""
echo "=== 更新完成 ==="
echo ""
echo "📋 更新摘要:"
echo "├─ 容器名称: $CONTAINER_NAME"
echo "├─ 镜像名称: $IMAGE_NAME"
if [[ -n "$OLD_IMAGE_ID" ]]; then
    echo "├─ 旧镜像ID: ${OLD_IMAGE_ID:0:12}..."
fi
echo "├─ 新镜像ID: ${NEW_IMAGE_ID:0:12}..."
echo "├─ 容器状态: $CONTAINER_STATUS"
echo "└─ QQ消息备份: $MESSAGE_FILE"
echo ""

# 13. 显示QQ消息内容
echo "📱 QQ消息内容:"
echo "════════════════════════════════════════"
echo "$QQ_MESSAGE"
echo "════════════════════════════════════════"
echo ""

# 14. 发送提示
echo "🚀 发送到QQ的方法:"
echo "1. 复制上面的消息内容到QQ"
echo "2. 或查看备份文件: cat $MESSAGE_FILE"
echo ""
echo "🔧 后续检查:"
echo "1. 访问 http://www.yessirpopesama.cc:6806/"
echo "2. 查看日志: docker logs $CONTAINER_NAME"
