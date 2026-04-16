#!/bin/bash

# --- 參數設定 ---
if [ -z "$1" ] || [ -z "$2" ]; then
    echo "錯誤: 請提供服務類型和部署路徑參數"
    echo "用法: $0 <api|connector> <部署路徑>"
    exit 1
fi

SERVICE_TYPE="$1"
PROJECT_PATH="$2"

# 根據服務類型設定參數
if [ "$SERVICE_TYPE" = "api" ]; then
    BINARY_NAME="api"
    TAR_FILE="/tmp/go_backend.tar.gz"
    SESSION_NAME="whatsapp-backend"
elif [ "$SERVICE_TYPE" = "connector" ]; then
    BINARY_NAME="connector"
    TAR_FILE="/tmp/go_connector.tar.gz"
    SESSION_NAME="whatsapp-connector"
else
    echo "錯誤: 服務類型必須是 'api' 或 'connector'"
    exit 1
fi

echo "=== 部署服務: $SERVICE_TYPE ==="

echo "--- 遠端：開始部署流程 ---"

# 1. 檢查壓縮檔是否存在
if [ ! -f "$TAR_FILE" ]; then
    echo "錯誤: 在 /tmp 中找不到壓縮檔 $TAR_FILE"
    exit 1
fi

# 2. 解壓縮 (直接解壓到 /tmp 目錄下)
echo "解壓縮檔案..."
tar -xzvf "$TAR_FILE" -C /tmp/

# 3. 確保專案目錄存在
mkdir -p "$PROJECT_PATH"

# 4. 檢查並安裝 tmux
if ! command -v tmux &> /dev/null; then
    echo "tmux 未安裝，正在安裝..."
    apt-get update && apt-get install -y tmux
fi

# 5. 確保 tmux session 存在
tmux has-session -t $SESSION_NAME 2>/dev/null
if [ $? != 0 ]; then
    echo "建立新的 tmux 會話: $SESSION_NAME"
    tmux new-session -d -s $SESSION_NAME -c "$PROJECT_PATH"
fi

# 6. 停止舊程式
echo "停止舊程式 (C-c)..."
tmux send-keys -t $SESSION_NAME C-c Enter
sleep 2

# 7. 替換舊檔案
echo "更新執行檔..."
rm -f "$PROJECT_PATH/$BINARY_NAME"
mv "/tmp/$BINARY_NAME" "$PROJECT_PATH/$BINARY_NAME"
chmod +x "$PROJECT_PATH/$BINARY_NAME"

# 8. 啟動新程式
echo "在 tmux 中啟動新程式..."
tmux send-keys -t $SESSION_NAME "cd $PROJECT_PATH" Enter
tmux send-keys -t $SESSION_NAME "./$BINARY_NAME" Enter

# 9. 清理臨時壓縮檔
rm -f "$TAR_FILE"

echo "--- 遠端：部署完成 ---"
